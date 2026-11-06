package llamacpp

import (
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"time"
)

// fakeLlamaEnv, when set, makes the test binary act as llama-server: the
// value is a file it adds the pid of each child it starts to, a line each,
// so a test can check that every one ends.
const fakeLlamaEnv = "YGG_FAKE_LLAMA"

// runFakeLlama serves /health on --port and starts a child process, as a
// llama-server with helper processes would, then waits to be stopped.
func runFakeLlama() {
	if os.Getenv("YGG_FAKE_CHILD") == "1" {
		time.Sleep(time.Hour)
		os.Exit(0)
	}
	// Core reads the build's help before starting it, to know which flags
	// it takes: a real one prints it and exits, and starts nothing.
	if slices.Contains(os.Args[1:], "--help") {
		_, _ = os.Stdout.WriteString("usage: llama-server [options]\n")
		os.Exit(0)
	}
	port := ""
	for i, a := range os.Args {
		if a == "--port" && i+1 < len(os.Args) {
			port = os.Args[i+1]
		}
	}
	child := exec.Command(os.Args[0])
	child.Env = append(os.Environ(), "YGG_FAKE_CHILD=1")
	if err := child.Start(); err != nil {
		os.Exit(2)
	}
	if f, err := os.OpenFile(os.Getenv(fakeLlamaEnv), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		_, _ = f.WriteString(strconv.Itoa(child.Process.Pid) + "\n")
		_ = f.Close()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"status":"ok"}`)) })
	_ = http.ListenAndServe("127.0.0.1:"+port, mux)
	os.Exit(1)
}
