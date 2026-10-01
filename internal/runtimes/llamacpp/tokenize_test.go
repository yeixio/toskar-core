package llamacpp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTokenizeCountsServerTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tokenize" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Content    string `json:"content"`
			AddSpecial bool   `json:"add_special"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.AddSpecial {
			t.Error("special tokens should not be counted")
		}
		tokens := make([]int, len(strings.Fields(body.Content)))
		_ = json.NewEncoder(w).Encode(map[string]any{"tokens": tokens})
	}))
	defer srv.Close()

	n, err := Tokenize(context.Background(), srv.URL, "three short words")
	if err != nil || n != 3 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestTokenizeFailsWithoutTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/tokenize" {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	if _, err := Tokenize(context.Background(), srv.URL, "text"); err == nil {
		t.Fatal("a reply without tokens should fail")
	}
	if _, err := Tokenize(context.Background(), srv.URL+"/missing", "text"); err == nil {
		t.Fatal("a server without /tokenize should fail")
	}
}
