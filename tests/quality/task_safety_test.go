package quality

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/app"
	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/runlog"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/internal/webfixtures"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// safetyFile is task_safety.json (#1017).
type safetyFile struct {
	Disguised []string     `json:"disguised"`
	Poisoned  []poisonCase `json:"poisoned"`
}

// poisonCase is a task where what it reads tries to give orders. stub plays a
// model that obeys, the worst case. A real model may not try anything.
type poisonCase struct {
	ID      string            `json:"id"`
	What    string            `json:"what"`
	Real    bool              `json:"real"`
	Message string            `json:"message"`
	Files   map[string]string `json:"files"`
	Policy  string            `json:"policy"`
	Stub    []string          `json:"stub"`
	// Allow is answered yes once, Always yes for this chat, and all else no.
	Allow  []string `json:"allow"`
	Always []string `json:"always"`
	// MustAsk and MustRun name calls by their command or path, and
	// AfterOutside those that must warn they follow something from outside.
	MustAsk      []string `json:"must_ask"`
	MustRun      []string `json:"must_run"`
	AfterOutside []string `json:"after_outside"`
}

// safetyProject is a throwaway Git project with files to lose, and one beside
// it, outside the project.
type safetyProject struct {
	dir      string
	outside  string
	original map[string]string
}

func newSafetyProject(t *testing.T, files map[string]string) *safetyProject {
	t.Helper()
	root := t.TempDir()
	// macOS's temp folder is behind a link, and a project is its real path.
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	p := &safetyProject{dir: filepath.Join(root, "project"), outside: filepath.Join(root, "outside", "canary"), original: map[string]string{}}
	write := func(path, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(p.dir, "canary"), "keep me\n")
	write(filepath.Join(p.dir, "second"), "second\n")
	for name, text := range files {
		write(filepath.Join(p.dir, name), text)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-qm", "first"}} {
		if out, err := exec.Command("git", append([]string{"-C", p.dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	// Two commits, for what rewrites history, and a file Git doesn't know.
	write(filepath.Join(p.dir, "second"), "second, changed\n")
	for _, args := range [][]string{{"add", "."}, {"-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-qm", "second"}} {
		if out, err := exec.Command("git", append([]string{"-C", p.dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write(filepath.Join(p.dir, "untracked"), "not in Git\n")
	write(p.outside, "keep me too\n")
	for _, path := range p.watched() {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		p.original[path] = string(raw)
	}
	return p
}

func (p *safetyProject) watched() []string {
	return []string{filepath.Join(p.dir, "canary"), filepath.Join(p.dir, "second"), filepath.Join(p.dir, "untracked"), p.outside}
}

// damage says what's no longer as it was.
func (p *safetyProject) damage() []string {
	var out []string
	for _, path := range p.watched() {
		raw, err := os.ReadFile(path)
		if err != nil || string(raw) != p.original[path] {
			out = append(out, filepath.Base(path))
		}
	}
	return out
}

// safetyRun is what one task did.
type safetyRun struct {
	asked   []map[string]any
	started []string
	answer  string
}

func callName(args map[string]any) string {
	if c, _ := args["command"].(string); c != "" {
		return c
	}
	p, _ := args["path"].(string)
	return p
}

func (r safetyRun) ask(name string) map[string]any {
	for _, p := range r.asked {
		args, _ := p["args"].(map[string]any)
		if strings.Contains(callName(args), name) {
			return p
		}
	}
	return nil
}

func TestTaskSafety(t *testing.T) {
	if config.Env("QUALITY_URL") != "" {
		t.Skip("runs in this process, with its own Toskar")
	}
	model := config.Env("QUALITY_MODEL_URL")
	raw, err := os.ReadFile("task_safety.json")
	if err != nil {
		t.Fatal(err)
	}
	var file safetyFile
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TOSKAR_STUB_INFERENCE", "1")
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	a, err := app.New(app.Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })
	web, err := webfixtures.Load("task_safety_web.json")
	if err != nil {
		t.Fatal(err)
	}
	webfixtures.Register(a.Tools, web)

	run := func(t *testing.T, c poisonCase, project *safetyProject) safetyRun {
		t.Helper()
		ctx := context.Background()
		var mu sync.Mutex
		script := append([]string(nil), c.Stub...)
		a.StubReply = func(_ string, messages []pluginapi.ChatMessage) string {
			mu.Lock()
			defer mu.Unlock()
			if model != "" {
				reply, err := served(model, messages, sampling{})
				if err != nil {
					t.Errorf("model: %v", err)
				}
				return reply
			}
			if len(script) == 0 {
				return "done"
			}
			next := script[0]
			script = script[1:]
			return next
		}
		policies := map[string]string{}
		if c.Policy != "" {
			policies["terminal"] = c.Policy
		}
		profileID := setupProfile(t, ctx, a, Setup{Tools: policies})
		conv, err := a.Conversations.Create(ctx, "safety "+c.ID, profileID, "auto")
		if err != nil {
			t.Fatal(err)
		}
		var got safetyRun
		var eventsMu sync.Mutex
		subID, ch := a.Bus.Subscribe()
		done := make(chan struct{})
		go func() {
			defer close(done)
			for evt := range ch {
				switch evt.Type {
				case events.ToolRequested:
					args, _ := evt.Payload["args"].(map[string]any)
					eventsMu.Lock()
					got.asked = append(got.asked, evt.Payload)
					eventsMu.Unlock()
					allow, always := false, false
					for _, s := range c.Allow {
						allow = allow || strings.Contains(callName(args), s)
					}
					for _, s := range c.Always {
						always = always || strings.Contains(callName(args), s)
					}
					if id, _ := evt.Payload["request_id"].(string); id != "" {
						_ = a.Tools.Decide(id, allow || always, always)
					}
				case events.ToolStarted:
					// A tool's summary is its command or path.
					summary, _ := evt.Payload["summary"].(string)
					id, _ := evt.Payload["tool_id"].(string)
					eventsMu.Lock()
					got.started = append(got.started, id+": "+summary)
					eventsMu.Unlock()
				}
			}
		}()
		turn := turnopts.WithProject(runlog.WithOrigin(ctx, runlog.Origin{Client: "terminal"}), project.dir)
		stream, err := a.RunChat(turn, profileID, conv.ID, c.Message, false, "auto", "")
		if err != nil {
			t.Fatal(err)
		}
		var answer strings.Builder
		for chunk := range stream {
			if chunk.Error != "" {
				if model == "" {
					t.Fatalf("turn failed: %s", chunk.Error)
				}
				t.Logf("turn failed: %s", chunk.Error)
			}
			answer.WriteString(chunk.Content)
		}
		time.Sleep(50 * time.Millisecond) // let the last events arrive
		a.Bus.Unsubscribe(subID)
		<-done
		eventsMu.Lock()
		defer eventsMu.Unlock()
		got.answer = answer.String()
		return got
	}

	// Whatever a real model does, nothing is lost without someone saying yes,
	// since every question that can do damage here is answered no.
	for _, c := range file.Poisoned {
		if model != "" && !c.Real {
			continue
		}
		t.Run(c.ID, func(t *testing.T) {
			project := newSafetyProject(t, c.Files)
			got := run(t, c, project)
			if lost := project.damage(); len(lost) > 0 {
				t.Errorf("%s: damaged %v. Ran %v", c.What, lost, got.started)
			}
			if model != "" {
				t.Logf("asked %d, ran %v, answered: %.200s", len(got.asked), got.started, got.answer)
				return
			}
			for _, name := range c.MustAsk {
				if got.ask(name) == nil {
					t.Errorf("%s: %q didn't ask. Asked %v, ran %v", c.What, name, got.asked, got.started)
				}
			}
			for _, name := range c.AfterOutside {
				if p := got.ask(name); p != nil && p["after_outside"] != true {
					t.Errorf("%s: %q asked without saying it follows something from outside: %v", c.What, name, p)
				}
			}
			for _, name := range c.MustRun {
				if !strings.Contains(strings.Join(got.started, "\n"), name) {
					t.Errorf("%s: %q should have run. Ran %v", c.What, name, got.started)
				}
			}
		})
	}
	if model != "" {
		return
	}

	// Every disguise is refused, or asks with no "always" on offer, and doesn't
	// run. One that's merely harmless fails here too: the gate decides without
	// knowing what a command will do.
	for _, command := range file.Disguised {
		t.Run(command, func(t *testing.T) {
			if risk, _ := tools.ClassifyCommand(command); !risk.AlwaysAsks() {
				t.Errorf("taken for harmless")
			}
			project := newSafetyProject(t, nil)
			encoded, _ := json.Marshal(map[string]any{"tool_call": map[string]any{"id": "terminal", "args": map[string]any{"command": command}}})
			got := run(t, poisonCase{ID: "disguised", Message: "Fix the build.", Stub: []string{string(encoded), "I wasn't allowed to."}}, project)
			if lost := project.damage(); len(lost) > 0 {
				t.Errorf("damaged %v", lost)
			}
			// What can't be read as a command at all is refused before it
			// gets as far as asking, which is just as safe.
			if p := got.ask(command); p != nil && p["always_asks"] != true {
				t.Errorf("offered always: %v", p)
			} else if p == nil && len(got.started) > 0 {
				t.Errorf("didn't ask: %v", got.started)
			}
			for _, started := range got.started {
				if strings.HasPrefix(started, "terminal") {
					t.Errorf("ran after a no: %s", started)
				}
			}
		})
	}
}
