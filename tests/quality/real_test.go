package quality

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/runlog"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// realDriver runs a case against a running daemon and its real models,
// over the API. It adds a profile, knowledge, and a chat for the case, and
// removes the profile and knowledge afterwards; the chat is kept so a
// failure can be looked at.
type realDriver struct {
	base string
	// stop is why the run can't go on, such as a daemon that started
	// asking for an API key the run doesn't have. Shared by every case.
	stop *string
	// model is the model every chat asks for, such as qwen2.5-32b-q4, or
	// "" for the daemon's own pick ("auto").
	model string
}

func newRealDriver(base string) realDriver {
	return realDriver{base: base, stop: new(string)}
}

func (realDriver) Name() string { return "real" }

// modelID is the model chats ask for.
func (d realDriver) modelID() string {
	if d.model == "" {
		return "auto"
	}
	return d.model
}

// Stopped says why the run should end early, or "".
func (d realDriver) Stopped() string { return *d.stop }

// The daemon can go away for a moment during a long run: a restart, a
// settings change that rebinds it, a network blip. A request that never
// reached it, or that it turned away as unavailable, is retried until
// recoverWithin; one that reached it is never repeated, so a chat is never
// sent twice.
const recoverWithin = 3 * time.Minute

func (d realDriver) do(t *testing.T, method, path string, body any, out any) int {
	t.Helper()
	code, err := d.request(method, path, body, out, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	return code
}

// request sends one API request, retrying while the daemon is away (see
// recoverWithin), and decodes the reply into out.
func (d realDriver) request(method, path string, body, out any, logf func(string, ...any)) (int, error) {
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	deadline := time.Now().Add(recoverWithin)
	for wait := 2 * time.Second; ; wait = min(wait*2, 30*time.Second) {
		req, err := http.NewRequest(method, d.base+path, bytes.NewReader(payload))
		if err != nil {
			return 0, err
		}
		req.Header.Set("Content-Type", "application/json")
		if key := config.Env("QUALITY_KEY"); key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		resp, err := (&http.Client{Timeout: requestTimeout}).Do(req)
		retry := (err != nil && unreached(err)) || (err == nil && resp.StatusCode == http.StatusServiceUnavailable)
		if retry && time.Now().Add(wait).Before(deadline) {
			if resp != nil {
				resp.Body.Close()
			}
			logf("%s %s: the daemon isn't answering; trying again in %s", method, path, wait)
			time.Sleep(wait)
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("%s %s: %w", method, path, err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized {
			// The daemon wants a key the run doesn't have, or refused the
			// one it has, such as a revoked key. Nothing later can pass, so
			// stop the whole run with one reason.
			*d.stop = "the daemon at " + d.base + " requires an API key: set TOSKAR_QUALITY_KEY to a key from its API Access page"
			if config.Env("QUALITY_KEY") != "" {
				*d.stop = "the daemon at " + d.base + " refused TOSKAR_QUALITY_KEY: make a new key on its API Access page"
			}
			return resp.StatusCode, fmt.Errorf("%s %s: 401: %s", method, path, *d.stop)
		}
		if resp.StatusCode >= 300 {
			// A daemon missing what every chat needs fails every case the
			// same way, so the run stops with that reason.
			var e struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal(raw, &e) == nil && e.Error.Code == "RUNTIME_NOT_INSTALLED" {
				*d.stop = "the daemon at " + d.base + " can't chat: " + e.Error.Message
			}
			return resp.StatusCode, fmt.Errorf("%s %s: %d %s", method, path, resp.StatusCode, raw)
		}
		if rb, ok := out.(*rawBody); ok {
			*rb = raw
			return resp.StatusCode, nil
		}
		if out != nil && len(raw) > 0 {
			if err := json.Unmarshal(raw, out); err != nil {
				return resp.StatusCode, fmt.Errorf("%s %s: %v in %s", method, path, err, raw)
			}
		}
		return resp.StatusCode, nil
	}
}

// rawBody takes a reply as it is, such as a file's content.
type rawBody []byte

// unreached reports a request that never got to the daemon: refused or
// reset before a response, so sending it again can't repeat anything.
func unreached(err error) bool {
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "dial" {
		return true
	}
	return errors.Is(err, syscall.ECONNREFUSED)
}

// watch collects events for one chat and answers every approval no.
func (d realDriver) watch(t *testing.T, ctx context.Context, conversationID string) func() []Event {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, d.base+"/api/v1/events", nil)
	if key := config.Env("QUALITY_KEY"); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	var mu sync.Mutex
	var got []Event
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var evt struct {
				Type    string         `json:"type"`
				Payload map[string]any `json:"payload"`
			}
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &evt) != nil {
				continue
			}
			if conv, _ := evt.Payload["conversation_id"].(string); conv != conversationID {
				continue
			}
			mu.Lock()
			got = append(got, Event{Type: evt.Type, Payload: evt.Payload})
			mu.Unlock()
			if evt.Type == "tool.requested" {
				if id, _ := evt.Payload["request_id"].(string); id != "" {
					d.do(t, http.MethodPost, "/api/v1/tools/decide", map[string]any{"request_id": id, "allow": false}, nil)
				}
			}
		}
	}()
	return func() []Event {
		time.Sleep(200 * time.Millisecond)
		mu.Lock()
		defer mu.Unlock()
		return append([]Event(nil), got...)
	}
}

func (d realDriver) Run(t *testing.T, c Case) Result {
	t.Helper()
	var profile map[string]any
	d.do(t, http.MethodGet, "/api/v1/profiles/general-assistant", nil, &profile)
	delete(profile, "id")
	profile["name"] = "Quality " + c.ID
	var sources []any
	for _, k := range c.Setup.Knowledge {
		var src struct {
			ID string `json:"id"`
		}
		d.do(t, http.MethodPost, "/api/v1/knowledge/sources", map[string]any{"kind": "text", "filename": k.Filename, "text": k.Text}, &src)
		sources = append(sources, src.ID)
		t.Cleanup(func() { d.do(t, http.MethodDelete, "/api/v1/knowledge/sources/"+src.ID, nil, nil) })
	}
	profile["knowledge_sources"] = sources
	if len(c.Setup.Tools) > 0 {
		tools, _ := profile["tools"].([]any)
		for id, policy := range c.Setup.Tools {
			found := false
			for _, raw := range tools {
				if m, ok := raw.(map[string]any); ok && m["tool_id"] == id {
					m["policy"], found = policy, true
				}
			}
			if !found {
				tools = append(tools, map[string]any{"tool_id": id, "policy": policy})
			}
		}
		profile["tools"] = tools
	}
	if c.Setup.Topics != nil {
		profile["topics"] = c.Setup.Topics
	}
	if c.Setup.Deliberate != "" {
		orch, _ := profile["orchestration"].(map[string]any)
		if orch == nil {
			orch = map[string]any{}
		}
		orch["deliberate"] = c.Setup.Deliberate
		profile["orchestration"] = orch
	}
	var created struct {
		ID string `json:"id"`
	}
	d.do(t, http.MethodPost, "/api/v1/profiles", profile, &created)
	t.Cleanup(func() { d.do(t, http.MethodDelete, "/api/v1/profiles/"+created.ID, nil, nil) })

	var conv struct {
		ID string `json:"id"`
	}
	d.do(t, http.MethodPost, "/api/v1/conversations", map[string]any{"title": "Quality " + c.ID, "profile_id": created.ID, "model_id": d.modelID()}, &conv)
	chat := func(message string, attachments ...string) {
		d.do(t, http.MethodPost, "/api/v1/chat", map[string]any{
			"conversation_id": conv.ID, "profile_id": created.ID, "model_id": d.modelID(), "message": message, "stream": false,
			"attachments": attachments,
		}, nil)
	}
	// Attachments are uploaded as the apps upload them (#510).
	var attached []string
	for _, f := range c.Attach {
		data, err := f.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		var saved struct {
			ID string `json:"id"`
		}
		d.do(t, http.MethodPost, "/api/v1/artifacts", map[string]any{
			"name": f.Name, "content_base64": base64.StdEncoding.EncodeToString(data), "conversation_id": conv.ID,
		}, &saved)
		attached = append(attached, saved.ID)
	}
	// The real model answers the earlier turns itself.
	for i, m := range c.History {
		if m.Role == "user" {
			chat(padded(c, i, m.Content))
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	events := d.watch(t, ctx, conv.ID)
	chat(c.Message, attached...)
	// A follow-up in the same chat, with nothing attached (#449).
	if c.FollowUp != "" {
		chat(c.FollowUp)
	}
	r := Result{Events: events()}
	cancel()

	var listed json.RawMessage
	d.do(t, http.MethodGet, "/api/v1/conversations/"+conv.ID+"/messages", nil, &listed)
	var msgs []contracts.Message
	if json.Unmarshal(listed, &msgs) != nil {
		var wrapped struct {
			Messages []contracts.Message `json:"messages"`
		}
		_ = json.Unmarshal(listed, &wrapped)
		msgs = wrapped.Messages
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			r.Answer, r.Meta = msgs[i].Content, msgs[i].Meta
			break
		}
	}
	if r.Meta != nil {
		r.Files = map[string][]byte{}
		for _, f := range r.Meta.Files {
			if f.Producer != "assistant" {
				continue
			}
			var data rawBody
			if _, err := d.request(http.MethodGet, "/api/v1/artifacts/"+f.ID+"/content", nil, &data, t.Logf); err == nil {
				r.Files[f.Name] = data
			}
		}
	}
	if r.Meta != nil && r.Meta.RunID != "" {
		var run runlog.Run
		d.do(t, http.MethodGet, "/api/v1/runs/"+r.Meta.RunID, nil, &run)
		r.Run = &run
	}
	t.Logf("answer: %.200s", r.Answer)
	return r
}

// requestTimeout is how long one request to the daemon may take. A chat
// answer comes in minutes; the media set raises it, since a clip from a
// large model takes longer (the daemon allows one 75 minutes).
var requestTimeout = 10 * time.Minute
