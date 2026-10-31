package llamacpp

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"time"
)

// helpCache remembers each llama-server binary's help, which says what the
// build can do (#835, #955): an older build refuses to start with a flag
// it doesn't know.
var helpCache struct {
	sync.Mutex
	known map[string]helpProbe
}

type helpProbe struct {
	modTime time.Time
	text    string
}

// runHelp prints a llama-server's help; a variable so tests can stand in.
var runHelp = func(ctx context.Context, path string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, path, "--help").CombinedOutput()
}

// serverHelp is the help of the llama-server at path, or empty when it's
// missing. It asks once per binary, and again when an update replaces it.
func serverHelp(ctx context.Context, path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return ""
	}
	helpCache.Lock()
	defer helpCache.Unlock()
	if p, ok := helpCache.known[path]; ok && p.modTime.Equal(st.ModTime()) {
		return p.text
	}
	out, _ := runHelp(ctx, path)
	if helpCache.known == nil {
		helpCache.known = map[string]helpProbe{}
	}
	helpCache.known[path] = helpProbe{modTime: st.ModTime(), text: string(out)}
	return string(out)
}
