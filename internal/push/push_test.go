package push

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/store"
)

// Only a device whose key is still good is pushed to, and a person's
// devices are theirs alone.
func TestRegistrationsFollowTheirKeys(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, k := range []struct{ id, revoked string }{{"phone", ""}, {"old-phone", "2026-10-01"}, {"tablet", ""}} {
		if _, err := db.SQL.Exec(`INSERT INTO api_keys (id, name, key_prefix, key_hash, revoked_at) VALUES (?, ?, 'p', 'h', NULLIF(?, ''))`, k.id, k.id, k.revoked); err != nil {
			t.Fatal(err)
		}
	}
	s := NewStore(db.SQL)
	for _, r := range []Registration{
		{KeyID: "phone", PersonID: "sam", Handle: "h1", Platform: PlatformIOS},
		{KeyID: "old-phone", PersonID: "sam", Handle: "h2", Platform: PlatformIOS},
		{KeyID: "tablet", PersonID: "ana", Handle: "h3", Platform: PlatformAndroid},
	} {
		if err := s.Set(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ForPerson(ctx, "sam")
	if err != nil || len(got) != 1 || got[0].Handle != "h1" {
		t.Fatalf("sam's devices = %+v, %v", got, err)
	}
	if err := s.Set(ctx, Registration{KeyID: "phone", PersonID: "sam", Handle: "h1b", Platform: PlatformIOS}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ForPerson(ctx, "sam"); len(got) != 1 || got[0].Handle != "h1b" {
		t.Fatalf("a new handle didn't replace the old: %+v", got)
	}
	if err := s.Set(ctx, Registration{KeyID: "phone", Handle: "h", Platform: "palm"}); err == nil {
		t.Fatal("an unknown platform was taken")
	}
	_ = s.Remove(ctx, "phone")
	if got, _ := s.ForPerson(ctx, "sam"); len(got) != 0 {
		t.Fatalf("removed device still pushed to: %+v", got)
	}
}

// Each push is signed with the computer's key over the time and the body,
// as the relay checks it; a handle the relay calls gone is ErrGone, and a
// refusal keeps the relay's code.
func TestSendSignsAndReadsTheRelay(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	status, code := http.StatusOK, ""
	var got Message
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		key, _ := base64.StdEncoding.DecodeString(r.Header.Get("Toskar-Key"))
		sig, _ := base64.StdEncoding.DecodeString(r.Header.Get("Toskar-Signature"))
		at, _ := strconv.ParseInt(r.Header.Get("Toskar-Time"), 10, 64)
		if r.URL.Path != "/v1/push" || !ed25519.PublicKey(key).Equal(pub) || !ed25519.Verify(key, Signed(at, body), sig) {
			http.Error(w, "bad signature", http.StatusUnauthorized)
			return
		}
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(status)
		if code != "" {
			_, _ = w.Write([]byte(`{"error":{"code":"` + code + `","message":"no"}}`))
		}
	}))
	defer relay.Close()
	c := &Client{Relay: relay.URL, Key: priv, Now: func() time.Time { return time.Unix(1_760_000_000, 0) }}
	ctx := context.Background()
	if err := c.Send(ctx, Message{Handle: "h1", Title: "Back in stock: Trail runner, size 10", Urgent: true}); err != nil {
		t.Fatal(err)
	}
	if got.Handle != "h1" || got.Title != "Back in stock: Trail runner, size 10" || !got.Urgent {
		t.Fatalf("relay got %+v", got)
	}
	status, code = http.StatusGone, "HANDLE_GONE"
	if err := c.Send(ctx, Message{Handle: "h1", Title: "x"}); !errors.Is(err, ErrGone) {
		t.Fatalf("gone: %v", err)
	}
	status, code = http.StatusTooManyRequests, "RATE_LIMITED"
	var re *RelayError
	if err := c.Send(ctx, Message{Handle: "h1", Title: "x"}); !errors.As(err, &re) || re.Code != "RATE_LIMITED" || re.Permanent() {
		t.Fatalf("rate limited: %v", err)
	}
	status, code = http.StatusBadRequest, "BAD_HANDLE"
	if err := c.Send(ctx, Message{Handle: "h1", Title: "x"}); !errors.As(err, &re) || !re.Permanent() {
		t.Fatalf("bad handle: %v", err)
	}
}
