package llamacpp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// A response schema reaches llama-server as response_format, which it turns
// into a grammar (§27).
func TestChatSendsResponseSchema(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"ok\":true}"}}]}`))
	}))
	defer srv.Close()
	ch, err := NewClient().Chat(context.Background(), pluginapi.ChatRequest{
		ModelEndpoint: srv.URL, Messages: []pluginapi.ChatMessage{{Role: "user", Content: "ok?"}},
		ResponseSchema: json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
	rf, _ := got["response_format"].(map[string]any)
	schema, _ := rf["schema"].(map[string]any)
	if rf["type"] != "json_object" || schema["type"] != "object" {
		t.Fatalf("request = %+v", got)
	}

	got = nil
	ch, _ = NewClient().Chat(context.Background(), pluginapi.ChatRequest{ModelEndpoint: srv.URL, Messages: []pluginapi.ChatMessage{{Role: "user", Content: "hi"}}})
	for range ch {
	}
	if _, ok := got["response_format"]; ok {
		t.Fatal("response_format sent without a schema")
	}
}

// A temperature reaches llama-server, greedy as 0 (#70); none is sent
// when it isn't set, so the server's default stands.
func TestChatSendsTemperature(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = nil
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()
	for temp, want := range map[float64]any{0: nil, 0.7: 0.7, pluginapi.GreedyTemperature: 0.0} {
		ch, err := NewClient().Chat(context.Background(), pluginapi.ChatRequest{
			ModelEndpoint: srv.URL, Messages: []pluginapi.ChatMessage{{Role: "user", Content: "hi"}}, Temperature: temp, MaxTokens: 8,
		})
		if err != nil {
			t.Fatal(err)
		}
		for range ch {
		}
		if got["temperature"] != want || got["max_tokens"] != 8.0 {
			t.Errorf("temperature %v: sent %v, max_tokens %v", temp, got["temperature"], got["max_tokens"])
		}
	}
}

// The sampler and seed go to llama-server as it names them, and only when
// set (#839).
func TestChatSendsSampling(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = nil
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()
	send := func(s pluginapi.Sampling) {
		ch, err := NewClient().Chat(context.Background(), pluginapi.ChatRequest{
			ModelEndpoint: srv.URL, Messages: []pluginapi.ChatMessage{{Role: "user", Content: "hi"}}, Sampling: s,
		})
		if err != nil {
			t.Fatal(err)
		}
		for range ch {
		}
	}
	seed := int64(0)
	send(pluginapi.Sampling{TopP: 0.9, TopK: 40, MinP: 0.05, Seed: &seed})
	if got["top_p"] != 0.9 || got["top_k"] != 40.0 || got["min_p"] != 0.05 || got["seed"] != 0.0 {
		t.Fatalf("sent %v", got)
	}
	send(pluginapi.Sampling{})
	for _, k := range []string{"top_p", "top_k", "min_p", "seed"} {
		if _, ok := got[k]; ok {
			t.Errorf("unset %s was sent: %v", k, got[k])
		}
	}
}
