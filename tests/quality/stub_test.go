package quality

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/app"
	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/imagegen"
	"github.com/yeixio/toskar-core/internal/mimir"
	"github.com/yeixio/toskar-core/internal/models"
	"github.com/yeixio/toskar-core/internal/webfixtures"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
	"os"
)

// stubDriver runs a case in-process with the web replaced by the pages in
// web.json. Without a model URL the stub model's replies are scripted, so
// it needs no model and no network. With one, every model call goes to that
// OpenAI-compatible server instead, so a real model answers through the
// same routing, look-ups, tools, and checks.
type stubDriver struct {
	// model is an OpenAI-compatible server, such as llama-server, or "".
	model string
}

func (d stubDriver) Name() string {
	if d.model != "" {
		return "served"
	}
	return "stub"
}

// served sends one model call to an OpenAI-compatible server, at
// temperature 0 with a fixed seed so a run repeats as closely as the
// server allows.
func served(base string, messages []pluginapi.ChatMessage) (string, error) {
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	var out []msg
	for _, m := range messages {
		role := m.Role
		if role != "system" && role != "user" && role != "assistant" {
			// Tool results go back as the person's turn; llama-server's
			// chat templates take only these three roles.
			role = "user"
		}
		if n := len(out); n > 0 && out[n-1].Role == role {
			out[n-1].Content += "\n\n" + m.Content
			continue
		}
		out = append(out, msg{Role: role, Content: m.Content})
	}
	body, _ := json.Marshal(map[string]any{"messages": out, "temperature": 0, "seed": 1, "max_tokens": 768})
	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Post(base+"/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: %s", resp.Status, raw)
	}
	var reply struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil || len(reply.Choices) == 0 {
		return "", fmt.Errorf("unreadable reply: %.200s", raw)
	}
	return reply.Choices[0].Message.Content, nil
}

func (d stubDriver) Run(t *testing.T, c Case) Result {
	t.Helper()
	t.Setenv("TOSKAR_STUB_INFERENCE", "1")
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	a, err := app.New(app.Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })
	web, err := webfixtures.Load("web.json")
	if err != nil {
		t.Fatal(err)
	}
	webfixtures.Register(a.Tools, web)
	webfixtures.RegisterMedia(a.Tools, a.Artifacts)
	ctx := context.Background()
	// A picture attached needs a model that sees: the stub, with a
	// projector, so the picture is sent with the message (#510).
	for _, f := range c.Attach {
		if imagegen.IsEditable(f.Name) && !c.Setup.NoVision {
			installSeeingStub(t, ctx, a)
			break
		}
	}

	// The script: each model call gets the next reply; the rest say "done".
	var mu sync.Mutex
	var prompts [][]pluginapi.ChatMessage
	script := append([]string(nil), c.Stub...)
	a.StubReply = func(_ string, messages []pluginapi.ChatMessage) string {
		mu.Lock()
		defer mu.Unlock()
		prompts = append(prompts, append([]pluginapi.ChatMessage(nil), messages...))
		if d.model != "" {
			reply, err := served(d.model, messages)
			if err != nil {
				t.Errorf("model: %v", err)
			}
			return reply
		}
		// The search a follow-up asks for is written by the model, outside
		// the case's script of answers.
		if len(messages) > 0 && strings.HasPrefix(messages[0].Content, "You write web search queries") {
			if c.StubQuery != "" {
				return c.StubQuery
			}
			return c.Message
		}
		if len(script) == 0 {
			return "done"
		}
		next := script[0]
		script = script[1:]
		return next
	}

	profileID := setupProfile(t, ctx, a, c.Setup)
	conv, err := a.Conversations.Create(ctx, "quality "+c.ID, profileID, "auto")
	if err != nil {
		t.Fatal(err)
	}
	for i, m := range c.History {
		if _, err := a.Conversations.AddMessage(ctx, conv.ID, m.Role, padded(c, i, m.Content)); err != nil {
			t.Fatal(err)
		}
	}

	// Every approval is answered no, so nothing risky can run.
	var evMu sync.Mutex
	var got []Event
	subID, ch := a.Bus.Subscribe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range ch {
			evMu.Lock()
			got = append(got, Event{Type: evt.Type, Payload: evt.Payload})
			evMu.Unlock()
			if evt.Type == events.ToolRequested {
				if id, _ := evt.Payload["request_id"].(string); id != "" {
					_ = a.Tools.Decide(id, false, false)
				}
			}
		}
	}()

	// Attachments are saved as an upload is, after the same check, and
	// sent with the message (#510).
	var attached []string
	for _, f := range c.Attach {
		data, err := f.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		if !artifacts.IsAudio(f.Name) && !artifacts.IsVideo(f.Name) && !imagegen.IsEditable(f.Name) {
			if _, err := mimir.FilePassages(f.Name, data); err != nil {
				t.Fatalf("the upload of %s would be refused: %v", f.Name, err)
			}
		}
		saved, err := a.Artifacts.Save(ctx, artifacts.Input{ConversationID: conv.ID, Name: f.Name, Producer: artifacts.ProducerUser, Data: data})
		if err != nil {
			t.Fatal(err)
		}
		attached = append(attached, saved.ID)
	}
	stream, err := a.RunChat(artifacts.WithAttachments(ctx, attached), profileID, conv.ID, c.Message, false, "auto", "")
	if err != nil {
		t.Fatal(err)
	}
	var answer strings.Builder
	for chunk := range stream {
		if chunk.Error != "" {
			t.Fatalf("turn failed: %s", chunk.Error)
		}
		answer.WriteString(chunk.Content)
	}
	// A follow-up in the same chat, with nothing attached: what the case
	// checks is its answer and the prompts the model got for it.
	followFrom := 0
	if c.FollowUp != "" {
		mu.Lock()
		followFrom = len(prompts)
		mu.Unlock()
		stream, err := a.RunChat(ctx, profileID, conv.ID, c.FollowUp, false, "auto", "")
		if err != nil {
			t.Fatal(err)
		}
		answer.Reset()
		for chunk := range stream {
			if chunk.Error != "" {
				t.Fatalf("follow-up failed: %s", chunk.Error)
			}
			answer.WriteString(chunk.Content)
		}
	}
	time.Sleep(50 * time.Millisecond) // let the last events arrive
	if d.model != "" {
		t.Logf("answer: %.300s", answer.String())
	}
	a.Bus.Unsubscribe(subID)
	<-done

	mu.Lock()
	r := Result{Answer: answer.String(), Prompts: prompts[followFrom:]}
	mu.Unlock()
	evMu.Lock()
	r.Events = got
	evMu.Unlock()
	if msgs, err := a.Conversations.ListMessages(ctx, conv.ID); err == nil {
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Role == "assistant" {
				r.Meta = msgs[i].Meta
				if r.Answer == "" {
					r.Answer = msgs[i].Content
				}
				break
			}
		}
	}
	if r.Meta != nil {
		r.Files = map[string][]byte{}
		for _, f := range r.Meta.Files {
			if f.Producer != artifacts.ProducerAssistant {
				continue
			}
			if _, data, err := a.Artifacts.Read(ctx, f.ID); err == nil {
				r.Files[f.Name] = data
			}
		}
	}
	if r.Meta != nil && r.Meta.RunID != "" {
		if run, err := a.RunLog.Get(ctx, r.Meta.RunID); err == nil {
			r.Run = &run
		}
	}
	return r
}

// setupProfile copies the general assistant with the case's knowledge and
// tool policies.
// installSeeingStub installs a stand-in model that sees pictures, with a
// projector file, as the app's vision tests do.
func installSeeingStub(t *testing.T, ctx context.Context, a *app.App) {
	t.Helper()
	e := models.CatalogEntry{ID: "stub-vision", DisplayName: "Stub Vision", MemoryNeededBytes: 1,
		Capabilities: contracts.ModelCapabilities{ToolCalling: true, ToolCallSupport: "compatible", Vision: true},
		Tags:         []string{"vision", "general"}, Purpose: []string{"general"}, Runtime: []string{"llamacpp"},
		Projector: &models.ModelFile{URL: "http://unused/mmproj.gguf"}, Dynamic: true}
	a.Models.Catalog().Upsert(e)
	st := a.Models.Storage()
	if err := st.UpsertCatalogEntry(ctx, e); err != nil {
		t.Fatal(err)
	}
	_ = st.EnsureDirs()
	for path, data := range map[string]string{st.ModelPath(e.ID): "gguf", st.ProjectorPath(e.ID): "mmproj"} {
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.MarkInstalled(ctx, e.ID, st.ModelPath(e.ID), "", 4); err != nil {
		t.Fatal(err)
	}
}

func setupProfile(t *testing.T, ctx context.Context, a *app.App, s Setup) string {
	t.Helper()
	p, err := a.Profiles.Get(ctx, "general-assistant")
	if err != nil {
		t.Fatal(err)
	}
	p.ID, p.Name = "", "Quality"
	for _, k := range s.Knowledge {
		src, err := a.Mimir.Create(ctx, mimir.CreateInput{Kind: mimir.KindText, Filename: k.Filename, Text: k.Text})
		if err != nil {
			t.Fatal(err)
		}
		p.KnowledgeSources = append(p.KnowledgeSources, src.ID)
	}
	for id, policy := range s.Tools {
		found := false
		for i := range p.Tools {
			if p.Tools[i].ToolID == id {
				p.Tools[i].Policy, found = policy, true
			}
		}
		if !found {
			p.Tools = append(p.Tools, contracts.ToolPolicy{ToolID: id, Policy: policy})
		}
	}
	p.Topics = s.Topics
	created, err := a.Profiles.Create(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	return created.ID
}
