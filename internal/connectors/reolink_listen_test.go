package connectors

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/connectors/reolinktest"
)

// Toskar hears a Reolink doorbell through reolink-cli's live preview: the
// preview goes through a named pipe, its audio to ffmpeg, and ffmpeg's
// samples come out; a reolink-cli that fails says why (#667).
func TestReolinkListensThroughReolinkCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("named pipes")
	}
	dir := t.TempDir()
	// What the camera sends: a header, a picture, and 40 ms of "AAC" that
	// the fake ffmpeg passes through as samples.
	adts := append([]byte{0xff, 0xf1}, bytes.Repeat([]byte{0x10, 0x00}, 2*listenChunk-1)...)
	info := append([]byte("1001\x20\x00\x00\x00"), make([]byte, 24)...)
	stream := append(info, bcVideoFrame("00dc", []byte{0, 0, 0, 1, 0x65}, 4)...)
	stream = append(stream, bcAudio(adts)...)
	if err := os.WriteFile(filepath.Join(dir, "stream"), stream, 0o600); err != nil {
		t.Fatal(err)
	}
	cli := filepath.Join(dir, "reolink-cli")
	// Its gateway commands do nothing.
	script := "#!/bin/sh\ncase \"$1\" in gateway) exit 0;; esac\nprintf '%s\\n' \"$@\" > \"" + dir + "/args\"\nfor a; do last=\"$a\"; done\nif [ \"$FAIL\" = 1 ]; then echo '{\"ok\":false,\"error\":{\"message\":\"camera busy\"}}'; exit 1; fi\ncat \"" + dir + "/stream\" > \"$last\"\nsleep 0.2\n"
	if err := os.WriteFile(cli, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	ff := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(ff, []byte("#!/bin/sh\nexec cat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldCLI, oldFF, oldUp := reolinkCLI, ffmpegBin, gatewayUp
	reolinkCLI = func() (string, error) { return cli, nil }
	ffmpegBin = func() (string, error) { return ff, nil }
	gatewayUp = func(string) bool { return true }
	t.Cleanup(func() { reolinkCLI, ffmpegBin, gatewayUp = oldCLI, oldFF, oldUp })

	door := reolinktest.New(false)
	defer door.Close()
	m := reolinkManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := m.Connect(ctx, "reolink", map[string]string{"url": door.URL, "username": "admin", "password": door.Password, "talk_camera": "hub-ch0", "talk_gateway": "127.0.0.1:9000"}); err != nil {
		t.Fatal(err)
	}
	h, err := m.Listen(ctx, "reolink")
	if err != nil {
		t.Fatal(err)
	}
	var heard []int16
	for chunk := range h.Audio {
		heard = append(heard, chunk...)
	}
	if len(heard) != 2*listenChunk || heard[0] != -3585 || heard[1] != 0x10 {
		t.Fatalf("heard %d samples, starting %v", len(heard), heard[:min(len(heard), 2)])
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args"))
	if want := "--gateway-addr\n127.0.0.1:9000\n--camera\nhub-ch0\npreview\ncapture\n--stream\nsub\n"; !strings.HasPrefix(string(args), want) {
		t.Fatalf("reolink-cli %q", args)
	}

	t.Setenv("FAIL", "1")
	h, err = m.Listen(ctx, "reolink")
	if err != nil {
		t.Fatal(err)
	}
	for range h.Audio {
	}
	if err := h.Err(); err == nil || !strings.Contains(err.Error(), "camera busy") {
		t.Fatalf("a failing reolink-cli: %v", err)
	}
}
