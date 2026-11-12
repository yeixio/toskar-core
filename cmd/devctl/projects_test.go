package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

var someProjects = []contracts.Project{
	{ID: "p-garden", Name: "Garden", Instructions: "Answer for zone 7.", Folder: "/srv/garden", ChatCount: 2},
	{ID: "p-class", Name: "Class", ChatCount: 0},
	{ID: "p-site", Name: "Site"},
	{ID: "p-site2", Name: "site"},
}

// A project is named by its id, or its name in any case; two of the same
// name need the id (#1136).
func TestFindProject(t *testing.T) {
	for ref, want := range map[string]string{"p-class": "p-class", "garden": "p-garden", "GARDEN": "p-garden", "p-site2": "p-site2"} {
		if p, err := findProject(someProjects, ref); err != nil || p.ID != want {
			t.Errorf("%s: %v %v, want %s", ref, p.ID, err, want)
		}
	}
	if _, err := findProject(someProjects, "Kitchen"); err == nil || !strings.Contains(err.Error(), `no project called "Kitchen"`) || !strings.Contains(err.Error(), "toskarctl projects") {
		t.Errorf("unknown: %v", err)
	}
	if _, err := findProject(someProjects, "Site"); err == nil || !strings.Contains(err.Error(), "use its id: p-site, p-site2") {
		t.Errorf("ambiguous: %v", err)
	}
}

// chat --project starts the chat in the project; a name it doesn't know
// stops before anything starts (#1136).
func TestChatProject(t *testing.T) {
	f := newFakeChat()
	f.projs = someProjects
	if _, _, err := runChat(t, f, []string{"--project", "garden", "hello"}, "/new\n/exit\n", true); err != nil {
		t.Fatal(err)
	}
	if len(f.created) != 2 || f.created[0] != "||p-garden" || f.created[1] != "||p-garden" {
		t.Errorf("created %v: a new chat, and /new, start in the project", f.created)
	}
	if len(f.sent) != 1 || f.sent[0].Message != "hello" {
		t.Errorf("sent %+v", f.sent)
	}

	f = newFakeChat()
	f.projs = someProjects
	if _, _, err := runChat(t, f, []string{"--project", "Kitchen", "hello"}, "", true); err == nil || !strings.Contains(err.Error(), `no project called "Kitchen"`) || len(f.created) != 0 {
		t.Errorf("unknown project: %v, created %v", err, f.created)
	}
	if _, _, err := runChat(t, f, []string{"--project", "site"}, "", true); err == nil || !strings.Contains(err.Error(), "more than one") {
		t.Errorf("ambiguous project: %v", err)
	}
}

// With --project, --resume picks from the project's chats, and a chat named
// by id moves into it. Continuing a chat in a project keeps /new there (#1136).
func TestChatProjectResume(t *testing.T) {
	now := time.Now()
	chats := func() []contracts.Conversation {
		return []contracts.Conversation{
			{ID: "11111111", Title: "Beds", ProjectID: "p-garden", UpdatedAt: now.Add(-time.Hour)},
			{ID: "22222222", Title: "Tires", UpdatedAt: now},
			{ID: "33333333", Title: "Essay", ProjectID: "p-class", UpdatedAt: now.Add(-time.Minute)},
		}
	}

	f := newFakeChat()
	f.projs, f.convs = someProjects, chats()
	out, _, err := runChat(t, f, []string{"--project", "Garden", "--resume"}, "1\nmore?\n/exit\n", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, " 1. Beds") || strings.Contains(out, "Tires") || strings.Contains(out, "Essay") {
		t.Errorf("the picker lists only the project's chats:\n%s", out)
	}
	if len(f.sent) != 1 || *f.sent[0].ConversationId != "11111111" || len(f.moved) != 0 {
		t.Errorf("sent %+v, moved %v", f.sent, f.moved)
	}

	f = newFakeChat()
	f.projs, f.convs = someProjects, chats()
	if _, _, err := runChat(t, f, []string{"--project", "Garden", "--resume", "22222222", "hi"}, "/exit\n", true); err != nil || len(f.moved) != 1 || f.moved[0] != "22222222>p-garden" {
		t.Errorf("--resume id moves the chat into the project: %v %v", err, f.moved)
	}

	f = newFakeChat()
	f.projs = someProjects
	if _, _, err := runChat(t, f, []string{"--project", "Class", "--resume"}, "", true); err == nil || !strings.Contains(err.Error(), "no chats in Class") {
		t.Errorf("a project with no chats: %v", err)
	}

	f = newFakeChat()
	f.projs, f.convs = someProjects, chats()
	if _, _, err := runChat(t, f, []string{"--resume", "33333333"}, "/new\n/exit\n", true); err != nil || len(f.created) != 1 || f.created[0] != "||p-class" || len(f.moved) != 0 {
		t.Errorf("/new after continuing a chat in a project: %v %v %v", err, f.created, f.moved)
	}
}

// /project says which project the chat is in, moves it into one by name,
// and none takes it out (#1136).
func TestChatProjectCommand(t *testing.T) {
	f := newFakeChat()
	f.projs = someProjects
	out, errOut, err := runChat(t, f, nil, "/project\n/project garden\n/project\n/project Kitchen\n/new\n/project none\n/project\n/exit\n", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"This chat isn't in a project. /project name moves it into one",
		"This chat is in Garden.",
		"Instructions: yes",
		"Folder: /srv/garden, though here the chat works in this terminal's folder",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in:\n%s", want, out)
		}
	}
	if !strings.Contains(errOut, `no project called "Kitchen"`) {
		t.Errorf("unknown project: %s", errOut)
	}
	// Moved in, a new chat in the same project, which none takes out.
	if strings.Join(f.moved, " ") != "0000000a>p-garden 0000000b>" || len(f.created) != 2 || f.created[1] != "||p-garden" {
		t.Errorf("moved %v, created %v", f.moved, f.created)
	}
	if strings.Count(out, "This chat isn't in a project.") != 2 {
		t.Errorf("after /project none:\n%s", out)
	}

	// On another computer, the project's folder is where the chat works.
	f = newFakeChat()
	f.projs, f.where = someProjects, "rig.local"
	out, _, _ = runChat(t, f, []string{"--project", "Garden"}, "/project\n/exit\n", true)
	if !strings.Contains(out, "Folder: /srv/garden\n") {
		t.Errorf("another computer:\n%s", out)
	}
}

// toskarctl projects lists yours, as a table or JSON (#1136).
func TestProjectsCommand(t *testing.T) {
	list := someProjects[:2]
	c := fakeDaemon(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"GET /api/v1/projects": func(w http.ResponseWriter, r *http.Request) { reply(200, list)(w, r) },
	})
	run := func(args ...string) (string, error) {
		var out bytes.Buffer
		err := projectsCommand(args, c, &out)
		return out.String(), err
	}
	out, err := run()
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || strings.Join(strings.Fields(lines[0]), " ") != "ID NAME CHATS" || strings.Join(strings.Fields(lines[1]), " ") != "p-garden Garden 2" || strings.Join(strings.Fields(lines[2]), " ") != "p-class Class 0" {
		t.Errorf("table:\n%s", out)
	}
	if again, err := run("list"); err != nil || again != out {
		t.Errorf("list is the same: %v\n%s", err, again)
	}
	out, err = run("--output", "json")
	var got []contracts.Project
	if err != nil || json.Unmarshal([]byte(out), &got) != nil || len(got) != 2 || got[0].Name != "Garden" {
		t.Errorf("json: %v %q", err, out)
	}

	list = []contracts.Project{}
	if out, err := run(); err != nil || !strings.Contains(out, "You have no projects yet") {
		t.Errorf("none: %v %q", err, out)
	}

	var e *exitError
	if _, err := run("show"); !errors.As(err, &e) || e.code != 2 {
		t.Errorf("unknown command: %v", err)
	}
	if _, err := run("--output", "yaml"); !errors.As(err, &e) || e.code != 2 {
		t.Errorf("bad output: %v", err)
	}
}

// Against a real Core: chat --project starts the chat in the project, and a
// task still runs in the folder the terminal is in, not the project's (#1136).
func TestChatProjectAgainstCore(t *testing.T) {
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
			return "Hello!"
		}
	})
	rest := newDaemon()
	projectDir := t.TempDir()
	var p contracts.Project
	if err := rest.request(http.MethodPost, "/projects", map[string]string{"name": "Kitchen remodel", "folder": projectDir}, &p); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	t.Chdir(dir)

	var out, errOut bytes.Buffer
	streams := chatIO{in: strings.NewReader("Make a build folder here: run mkdir build\ny\n/project\n/exit\n"), inTerminal: true, out: &out, errOut: &errOut}
	if err := chatCommand([]string{"--project", "kitchen REMODEL"}, streams, newDaemonChat); err != nil {
		t.Fatalf("%v\n%s\n%s", err, out.String(), errOut.String())
	}
	t.Logf("session:\n%s\nstderr:\n%s", out.String(), errOut.String())
	if !strings.Contains(out.String(), "This chat is in Kitchen remodel.") {
		t.Errorf("/project doesn't name the project")
	}
	if info, err := os.Stat(filepath.Join(dir, "build")); err != nil || !info.IsDir() {
		t.Errorf("the command didn't run in the terminal's folder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "build")); err == nil {
		t.Errorf("the command ran in the project's folder")
	}
	var chats []contracts.Conversation
	if err := rest.request(http.MethodGet, "/conversations?project_id="+p.ID, nil, &chats); err != nil || len(chats) != 1 {
		t.Errorf("the project's chats: %v %+v", err, chats)
	}
}
