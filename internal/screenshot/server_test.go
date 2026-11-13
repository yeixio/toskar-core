package screenshot

import (
	"encoding/json"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestHandlerDemoAPIAndUI(t *testing.T) {
	web := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<!doctype html><title>ui</title>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log(1)")},
	}
	server := httptest.NewServer(Handler(web))
	t.Cleanup(server.Close)

	health, err := http.Get(server.URL + "/api/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(health.Body)
	health.Body.Close()
	if health.StatusCode != http.StatusOK || !strings.Contains(string(body), `"status":"ok"`) {
		t.Fatalf("health: %d %s", health.StatusCode, body)
	}

	page, err := http.Get(server.URL + "/chat")
	if err != nil {
		t.Fatal(err)
	}
	pageBody, _ := io.ReadAll(page.Body)
	page.Body.Close()
	if !strings.Contains(string(pageBody), "<title>ui</title>") {
		t.Fatalf("spa fallback: %s", pageBody)
	}

	asset, err := http.Get(server.URL + "/assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	assetBody, _ := io.ReadAll(asset.Body)
	asset.Body.Close()
	if string(assetBody) != "console.log(1)" {
		t.Fatalf("asset: %s", assetBody)
	}
}

// The Diagnostics page reads the capability inventory; an empty list in its
// place blanked the page in the release screenshots.
func TestCapabilitiesFixtureHasTheInventoryShape(t *testing.T) {
	body, ok := screenshotGET("/api/v1/capabilities")
	if !ok {
		t.Fatal("no capabilities fixture")
	}
	var snap map[string]any
	if err := json.Unmarshal([]byte(body), &snap); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"models", "nodes", "tools", "connectors", "providers", "abilities"} {
		if _, isList := snap[key].([]any); !isList {
			t.Errorf("%s is not a list", key)
		}
	}
}

func TestMCPShareFixtureHasTheShareShape(t *testing.T) {
	body, ok := screenshotGET("/api/v1/mcp/share")
	if !ok {
		t.Fatal("no MCP share fixture")
	}
	var share struct {
		URL     string   `json:"url"`
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	if err := json.Unmarshal([]byte(body), &share); err != nil || share.URL == "" || share.Command == "" || len(share.Args) == 0 {
		t.Fatalf("share = %+v, %v", share, err)
	}
}

// The Performance page reads live figures and running models' acceleration
// in the contract's shapes, so the screenshots show the GPU card and pill.
func TestGPUFixturesHaveTheContractShape(t *testing.T) {
	body, ok := screenshotGET("/api/v1/performance/live")
	if !ok {
		t.Fatal("no live fixture")
	}
	var live []contracts.LiveFigures
	if err := json.Unmarshal([]byte(body), &live); err != nil {
		t.Fatal(err)
	}
	if len(live) != 1 || live[0].NodeID != "local" || len(live[0].Recent) != 60 || len(live[0].Current.GPUs) != 1 || live[0].Current.GPUs[0].BusyPercent == nil {
		t.Errorf("live: %+v", live)
	}
	body, _ = screenshotGET("/api/v1/models/running")
	var running []contracts.RunningModelView
	if err := json.Unmarshal([]byte(body), &running); err != nil {
		t.Fatal(err)
	}
	if len(running) == 0 || running[0].Acceleration == nil || running[0].Acceleration.State != contracts.AccelerationGPU {
		t.Errorf("running: %+v", running)
	}
}

// The Automations screenshot fills in the form from its sentence; the
// computer's reading must have the parsed request's fields, or the form fails.
func TestAutomationParseFixtureHasTheParsedShape(t *testing.T) {
	server := httptest.NewServer(Handler(fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("ui")}}))
	t.Cleanup(server.Close)
	resp, err := http.Post(server.URL+"/api/v1/automations/parse", "application/json", strings.NewReader(`{"text":"x","time_zone":"UTC","language":"en"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var parsed struct {
		Name         string                `json:"name"`
		Prompt       string                `json:"prompt"`
		Schedule     struct{ Kind string } `json:"schedule"`
		Notification struct {
			Mode      string `json:"mode"`
			Condition struct {
				Op    string  `json:"op"`
				Value float64 `json:"value"`
			} `json:"condition"`
		} `json:"notification"`
		Notes []string `json:"notes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Prompt == "" || parsed.Schedule.Kind != "daily" || parsed.Notification.Condition.Op != "below" || parsed.Notification.Condition.Value != 500 || parsed.Notes == nil {
		t.Fatalf("parse fixture: %+v", parsed)
	}
}

// Each demo chat's messages have the contract's shape, so a screen that
// opens one doesn't show "Toskar couldn't load this".
func TestChatFixturesHaveTheMessageShape(t *testing.T) {
	for _, path := range []string{"/api/v1/conversations/conv-local/messages", "/api/v1/conversations/conv-budget/messages", "/api/v1/conversations/conv-letter/messages", "/api/v1/conversations/conv-research/messages"} {
		body, ok := screenshotGET(path)
		if !ok {
			t.Fatalf("no fixture for %s", path)
		}
		var messages []contracts.Message
		if err := json.Unmarshal([]byte(body), &messages); err != nil || len(messages) == 0 {
			t.Errorf("%s: %d messages, %v", path, len(messages), err)
		}
	}
}

// The demo has projects (#1136), each with its page and its chats.
func TestProjectFixtures(t *testing.T) {
	body, ok := screenshotGET("/api/v1/projects")
	if !ok {
		t.Fatal("no projects fixture")
	}
	var list []contracts.Project
	if err := json.Unmarshal([]byte(body), &list); err != nil || len(list) == 0 {
		t.Fatalf("projects: %v %s", err, body)
	}
	for _, listed := range list {
		one, ok := screenshotGET("/api/v1/projects/" + listed.ID)
		if !ok {
			t.Fatalf("no page fixture for project %s", listed.ID)
		}
		var project contracts.Project
		if err := json.Unmarshal([]byte(one), &project); err != nil || project.Name == "" || project.ID != listed.ID {
			t.Fatalf("project: %v %s", err, one)
		}
		var chats []contracts.Conversation
		if err := json.Unmarshal([]byte(screenshotProjectConversations(project.ID)), &chats); err != nil || len(chats) != project.ChatCount {
			t.Fatalf("project %s chats: %v %d, want %d", project.ID, err, len(chats), project.ChatCount)
		}
		for _, c := range chats {
			if c.ProjectID != project.ID {
				t.Fatalf("chat %s is in %q", c.ID, c.ProjectID)
			}
		}
	}
}

// The demo has swarms (#1135): each record, read by id and in the list,
// with agents whose chats ?swarm_id= lists, and its coordinator chat in
// history. The task swarm has a folder, a branch, checks, and was applied.
func TestSwarmFixtures(t *testing.T) {
	listed, ok := screenshotGET("/api/v1/swarms")
	if !ok {
		t.Fatal("no swarms fixture")
	}
	var list []contracts.Swarm
	if err := json.Unmarshal([]byte(listed), &list); err != nil || len(list) != 2 {
		t.Fatalf("swarms: %v %s", err, listed)
	}
	var all []contracts.Conversation
	if err := json.Unmarshal([]byte(screenshotConversations()), &all); err != nil {
		t.Fatal(err)
	}
	task := false
	for _, item := range list {
		body, ok := screenshotGET("/api/v1/swarms/" + item.ID)
		if !ok {
			t.Fatalf("no fixture for swarm %s", item.ID)
		}
		var sw contracts.Swarm
		if err := json.Unmarshal([]byte(body), &sw); err != nil || len(sw.Agents) == 0 || sw.ID != item.ID {
			t.Fatalf("swarm: %v %s", err, body)
		}
		if sw.Folder != "" {
			task = sw.Branch != "" && sw.Checks == "passed" && sw.Applied
		}
		var chats []contracts.Conversation
		if err := json.Unmarshal([]byte(screenshotSwarmConversations(sw.ID)), &chats); err != nil {
			t.Fatal(err)
		}
		for _, ag := range sw.Agents {
			found := false
			for _, c := range chats {
				found = found || (c.ID == ag.ConversationID && c.SwarmRole == ag.Role && c.SwarmID == sw.ID)
			}
			if !found {
				t.Errorf("agent %d's chat %s isn't listed", ag.Agent, ag.ConversationID)
			}
		}
		coordinator := false
		for _, c := range all {
			coordinator = coordinator || (c.ID == sw.ConversationID && c.SwarmID == sw.ID && c.ProjectID == sw.ProjectID)
		}
		if !coordinator {
			t.Errorf("no coordinator chat %s with swarm_id %s", sw.ConversationID, sw.ID)
		}
	}
	if !task {
		t.Error("no task swarm with a branch, passing checks, and its change applied")
	}
}
