package connectors

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/connectors/reolinktest"
)

// fakeReolinkCLI stands in for reolink-cli: it writes its arguments and the
// audio it was given to dir, and prints answer.
func fakeReolinkCLI(t *testing.T, answer string, code int) (dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake reolink-cli is a shell script")
	}
	dir = t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + dir + "/args\"\nprintf '%s' \"$REOLINK_PASSWORD\" > \"" + dir + "/password\"\ncat > \"" + dir + "/audio\"\necho '" + answer + "'\nexit " + string(rune('0'+code)) + "\n"
	bin := filepath.Join(dir, "reolink-cli")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	old, oldUp := reolinkCLI, gatewayUp
	reolinkCLI = func() (string, error) { return bin, nil }
	gatewayUp = func(string) bool { return true }
	t.Cleanup(func() { reolinkCLI, gatewayUp = old, oldUp })
	return dir
}

// Toskar speaks through a Reolink doorbell with reolink-cli, and adds
// silence after the words, since the doorbell cuts off the last moment
// of what it's sent (#667).
func TestReolinkTalksThroughReolinkCLI(t *testing.T) {
	door := reolinktest.New(false)
	defer door.Close()
	m := reolinkManager(t)
	ctx := context.Background()
	if _, err := m.Connect(ctx, "reolink", map[string]string{"url": door.URL, "username": "admin", "password": door.Password}); err != nil {
		t.Fatal(err)
	}
	words := make([]int16, ReolinkTalkRate) // one second
	for i := range words {
		words[i] = 1000
	}
	// Nothing to set up (#667): it reaches the connection's own camera, at
	// its address and channel with its login, through the default gateway,
	// and the password isn't on the command line.
	if _, err := m.Connect(ctx, "reolink", map[string]string{"url": door.URL, "username": "admin", "password": door.Password, "channel": "0"}); err != nil {
		t.Fatal(err)
	}
	dir := fakeReolinkCLI(t, `{"ok":true,"command":"audio","data":{"durationMs":1800}}`, 0)
	if !m.CanTalk("reolink") {
		t.Fatal("a connection with reolink-cli can't talk")
	}
	if err := m.Talk(ctx, "reolink", words, ReolinkTalkRate); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args"))
	if got := strings.Join(strings.Fields(string(args)), " "); got != "--gateway-addr 127.0.0.1:9000 --host=127.0.0.1:9000 --channel=0 --user=admin audio talk --stdin --sample-rate 16000" {
		t.Fatalf("args = %v", got)
	}
	if strings.Contains(string(args), door.Password) {
		t.Fatal("the password is on the command line")
	}
	if pw, _ := os.ReadFile(filepath.Join(dir, "password")); string(pw) != door.Password {
		t.Fatalf("reolink-cli was given the password %q", pw)
	}

	// A camera named in reolink-cli is used instead.
	if _, err := m.Connect(ctx, "reolink", map[string]string{"url": door.URL, "username": "admin", "password": door.Password, "talk_camera": "hub-ch0", "talk_gateway": "127.0.0.1:9100"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Talk(ctx, "reolink", words, ReolinkTalkRate); err != nil {
		t.Fatal(err)
	}
	args, _ = os.ReadFile(filepath.Join(dir, "args"))
	if got := strings.Join(strings.Fields(string(args)), " "); got != "--gateway-addr 127.0.0.1:9100 --camera hub-ch0 audio talk --stdin --sample-rate 16000" {
		t.Fatalf("args = %v", got)
	}
	audio, _ := os.ReadFile(filepath.Join(dir, "audio"))
	tail := ReolinkTalkRate * 8 / 10
	if len(audio) != 2*(len(words)+tail) {
		t.Fatalf("sent %d bytes, want the words and 0.8 s of silence, %d", len(audio), 2*(len(words)+tail))
	}
	for i := 2 * len(words); i < len(audio); i++ {
		if audio[i] != 0 {
			t.Fatalf("byte %d after the words isn't silence", i)
		}
	}
	// The words are sent louder, for a doorbell's small speaker: 1000 is
	// raised by the most it's raised, four times.
	if audio[0] != 0xa0 || audio[1] != 0x0f { // 4000, little-endian
		t.Fatalf("words start % x", audio[:2])
	}

	fakeReolinkCLI(t, `{"ok":false,"error":{"code":"CAMERA_OFFLINE","message":"camera is offline"}}`, 1)
	if err := m.Talk(ctx, "reolink", words, ReolinkTalkRate); err == nil || !strings.Contains(err.Error(), "camera is offline") {
		t.Fatalf("a failed talk: %v", err)
	}
	if err := m.Talk(ctx, "homeassistant", words, ReolinkTalkRate); err == nil {
		t.Fatal("Home Assistant has no speaker here")
	}
}

// A speaker name or gateway that isn't one never reaches the command line.
func TestReolinkTalkRefusesOddNames(t *testing.T) {
	fakeReolinkCLI(t, `{"ok":true}`, 0)
	for _, cred := range []Credential{
		{"talk_camera": "--help"},
		{"talk_camera": "a b"},
		{"talk_camera": "door", "talk_gateway": "x; rm -rf"},
		{"url": "https://cam", "channel": "0; reboot"},
		{"url": ""},
	} {
		if err := reolinkTalk(context.Background(), cred, []int16{1}, ReolinkTalkRate); err == nil {
			t.Errorf("%v: no error", cred)
		}
	}
}

// Words are raised to 90% of full scale, by at most four times, and never
// lowered or clipped (#667).
func TestLouderForADoorbell(t *testing.T) {
	if got := louder([]int16{10000, -5000}); got[0] != 29490 || got[1] != -14745 {
		t.Errorf("a third of full scale: %v", got)
	}
	if got := louder([]int16{100, -50}); got[0] != 400 || got[1] != -200 {
		t.Errorf("quiet words: %v", got)
	}
	if got := louder([]int16{32000, -32767}); got[0] != 32000 || got[1] != -32767 {
		t.Errorf("already loud: %v", got)
	}
	if got := louder([]int16{0, 0}); got[0] != 0 {
		t.Errorf("silence: %v", got)
	}
}
