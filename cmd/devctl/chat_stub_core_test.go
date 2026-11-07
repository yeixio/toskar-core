package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/app"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// stubCore starts a real Core in this process, answering with the stub model
// from reply, and points toskarctl at it with TOSKAR_URL.
func stubCore(t *testing.T, reply func(messages []pluginapi.ChatMessage) string) {
	t.Helper()
	port := func() string {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		return strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
	}
	api := port()
	t.Setenv("TOSKAR_API_PORT", api)
	t.Setenv("TOSKAR_INTERNAL_PORT", port())
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	t.Setenv("TOSKAR_STUB_INFERENCE", "1")
	t.Setenv("TOSKAR_URL", "http://127.0.0.1:"+api)
	t.Setenv("TOSKAR_API_KEY", "")
	a, err := app.New(app.Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	a.StubReply = func(_ string, messages []pluginapi.ChatMessage) string { return reply(messages) }
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = a.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = a.Shutdown(context.Background())
	})
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if resp, err := http.Get("http://127.0.0.1:" + api + "/api/v1/health"); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
	}
	t.Fatal("the Core never answered")
}

// A whole terminal session against a real Core and the stub model (#959):
// an answer, then work in this folder whose command asks first, runs there
// once you say y, and is answered.
func TestChatSessionAgainstCore(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a Core")
	}
	stubCore(t, func(messages []pluginapi.ChatMessage) string {
		last := messages[len(messages)-1].Content
		switch {
		case strings.Contains(last, "Tool result"):
			return "I made the build folder."
		case strings.Contains(last, "Make a build folder"):
			return `{"tool_call":{"id":"terminal","args":{"command":"mkdir build"}}}`
		default:
			return "Hello! What can I do for you?"
		}
	})
	dir := t.TempDir()
	t.Chdir(dir)

	var out, errOut bytes.Buffer
	streams := chatIO{in: strings.NewReader("hello\nMake a build folder here: run mkdir build\ny\n/exit\n"), inTerminal: true, out: &out, errOut: &errOut}
	if err := chatCommand(nil, streams, newDaemonChat); err != nil {
		t.Fatalf("%v\n%s\n%s", err, out.String(), errOut.String())
	}
	t.Logf("session:\n%s\nstderr:\n%s", out.String(), errOut.String())
	for _, want := range []string{"Hello! What can I do for you?", "mkdir build", "Allow? [y]es, [n]o", "I made the build folder."} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("no %q in the session", want)
		}
	}
	if info, err := os.Stat(filepath.Join(dir, "build")); err != nil || !info.IsDir() {
		t.Errorf("the command didn't run in this folder: %v", err)
	}
}
