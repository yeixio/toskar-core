package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestMCPBridge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" || r.Header.Get("Authorization") != "Bearer k1" {
			t.Errorf("path %s auth %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		if _, ok := m["id"]; !ok {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": m["id"], "result": map[string]any{"method": m["method"]}})
	}))
	defer srv.Close()
	out := &syncBuffer{}
	b := &bridge{endpoint: srv.URL + "/mcp", key: "k1", out: out, client: srv.Client()}
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if err := b.run(in); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.Contains(out.String(), `"method":"initialize"`) || !strings.Contains(out.String(), `"method":"tools/list"`) {
		t.Fatalf("output = %q", out.String())
	}
}

func TestMCPBridgeExplainsWhenYggdrasilIsDown(t *testing.T) {
	out := &syncBuffer{}
	b := &bridge{endpoint: "http://127.0.0.1:1/mcp", out: out, client: http.DefaultClient}
	_ = b.run(strings.NewReader(`{"jsonrpc":"2.0","id":"a","method":"initialize"}` + "\n"))
	var resp struct {
		ID    string `json:"id"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &resp); err != nil {
		t.Fatalf("output = %q", out.String())
	}
	if resp.ID != "a" || !strings.Contains(resp.Error.Message, "not running") {
		t.Fatalf("resp = %+v", resp)
	}
}
