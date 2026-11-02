package hfclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/models/hfclient"
)

// The store shows what it can install, with what Hugging Face says about it.
func TestListingForTheStore(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/tree/") {
			_ = json.NewEncoder(w).Encode([]map[string]any{{"type": "file", "path": "Good-Q4_K_M.gguf", "size": 2_000_000_000}})
			return
		}
		calls++
		if got := r.URL.Query().Get("sort"); got != "trendingScore" {
			t.Errorf("sort %q", got)
		}
		repo := func(id, file string, more map[string]any) map[string]any {
			m := map[string]any{"id": id, "likes": 50, "createdAt": "2026-10-24T00:00:00.000Z", "tags": []string{"gguf", "license:apache-2.0"},
				"siblings": []map[string]any{{"rfilename": file}}}
			for k, v := range more {
				m[k] = v
			}
			return m
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			repo("org/Good-GGUF", "Good-Q4_K_M.gguf", map[string]any{"gated": false}),
			repo("org/Good-Uncensored-GGUF", "Good-Q4_K_M.gguf", nil),
			repo("org/Locked-GGUF", "Locked-Q4_K_M.gguf", map[string]any{"gated": "manual"}),
			repo("org/Odd-GGUF", "Odd-GSQ-RCO-Q4_K_M.gguf", nil),
			repo("org/Raw-GGUF", "Raw-BF16.gguf", nil),
		})
	}))
	defer srv.Close()
	c := hfclient.New()
	c.BaseURL = srv.URL
	got, err := c.Listing(context.Background(), "trendingScore", "", 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	m := got[0]
	if m.RepoID != "org/Good-GGUF" || m.Likes != 50 || m.License != "apache-2.0" || m.SizeBytes != 2_000_000_000 || m.DetailsURL != "https://huggingface.co/org/Good-GGUF" || m.CreatedAt.IsZero() {
		t.Fatalf("%+v", m)
	}
	if _, err := c.Listing(context.Background(), "trendingScore", "", 8); err != nil || calls != 1 {
		t.Fatalf("not kept: %d calls, %v", calls, err)
	}
}

func TestListingSaysWhenHuggingFaceFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer srv.Close()
	c := hfclient.New()
	c.BaseURL = srv.URL
	if _, err := c.Listing(context.Background(), "likes", "", 8); err == nil {
		t.Fatal("no error")
	}
}
