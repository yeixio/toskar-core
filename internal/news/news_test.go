package news

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const feedJSON = `{"version":1,"updated":"2026-10-22T13:20:00Z","items":[
 {"id":"model:a/b","date":"2026-10-22","kind":"model","title":"B from A","summary":"A model.","source":{"name":"Hugging Face","url":"https://huggingface.co/a/b"},"model_id":"b-q4"},
 {"id":"announcement:x/y","date":"2026-10-21","kind":"announcement","title":"Plain link","summary":"Left out.","source":{"name":"X","url":"http://x.example/y"}},
 {"id":"announcement:x/z","date":"2026-10-21","kind":"announcement","title":"","summary":"No title.","source":{"name":"X","url":"https://x.example/z"}}]}`

// The news is read once and kept: a second look within six hours reads the
// copy, a later one reads toskar.ai again, and the request is recorded.
func TestGetKeepsAndRefreshes(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(feedJSON))
	}))
	defer srv.Close()
	now := time.Date(2026, 10, 22, 14, 0, 0, 0, time.UTC)
	var sent []string
	r := &Reader{URL: srv.URL, Dir: t.TempDir(), Now: func() time.Time { return now },
		Sent: func(_ context.Context, host, detail string) { sent = append(sent, host+" "+detail) }}
	ctx := context.Background()

	v, err := r.Get(ctx)
	if err != nil || !v.Enabled || v.Stale || len(v.Items) != 1 || v.Items[0].ModelID != "b-q4" || v.Updated != "2026-10-22T13:20:00Z" {
		t.Fatalf("first: %+v, %v", v, err)
	}
	now = now.Add(5 * time.Hour)
	if _, err := r.Get(ctx); err != nil || hits.Load() != 1 {
		t.Fatalf("within six hours: %d reads, %v", hits.Load(), err)
	}
	now = now.Add(2 * time.Hour)
	if _, err := r.Get(ctx); err != nil || hits.Load() != 2 {
		t.Fatalf("after six hours: %d reads, %v", hits.Load(), err)
	}
	if len(sent) != 2 || sent[0] == "" {
		t.Errorf("recorded %q", sent)
	}
}

// When toskar.ai can't be reached, the kept copy shows as stale; with no
// copy, the error comes back.
func TestGetOffline(t *testing.T) {
	up := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !up {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(feedJSON))
	}))
	defer srv.Close()
	now := time.Now()
	dir := t.TempDir()
	r := &Reader{URL: srv.URL, Dir: dir, Now: func() time.Time { return now }}
	ctx := context.Background()
	if _, err := r.Get(ctx); err != nil {
		t.Fatal(err)
	}
	up = false
	now = now.Add(7 * time.Hour)
	// A new Reader, as after a restart: the copy is on disk.
	r2 := &Reader{URL: srv.URL, Dir: dir, Now: func() time.Time { return now }}
	v, err := r2.Get(ctx)
	if err != nil || !v.Stale || len(v.Items) != 1 {
		t.Fatalf("offline with a copy: %+v, %v", v, err)
	}
	if _, err := (&Reader{URL: srv.URL, Dir: t.TempDir()}).Get(ctx); err == nil {
		t.Error("offline with no copy: no error")
	}
}

// Off, nothing is read; a feed of another version is refused.
func TestGetOffAndVersion(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{"version":2,"items":[]}`))
	}))
	defer srv.Close()
	ctx := context.Background()
	off := &Reader{URL: srv.URL, Dir: t.TempDir(), On: func(context.Context) bool { return false }}
	if v, err := off.Get(ctx); err != nil || v.Enabled || v.Items == nil || hits.Load() != 0 {
		t.Fatalf("off: %+v, %v, %d reads", v, err, hits.Load())
	}
	if _, err := (&Reader{URL: srv.URL, Dir: t.TempDir()}).Get(ctx); err == nil {
		t.Error("version 2 was read")
	}
}
