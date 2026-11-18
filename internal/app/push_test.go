package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/yeixio/toskar-core/internal/gjallarhorn"
	"github.com/yeixio/toskar-core/internal/push"
	"github.com/yeixio/toskar-core/internal/store"
)

// With an iPhone and an iPad, one that failed for a passing reason gets the
// notification on the retry, and the other doesn't get it twice (#1267).
func TestPushRetriesOnlyTheDevicesThatMissedIt(t *testing.T) {
	// Regression: toskar-core#1267
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, id := range []string{"iphone", "ipad"} {
		if _, err := db.SQL.Exec(`INSERT INTO api_keys (id, name, key_prefix, key_hash) VALUES (?, ?, 'p', 'h')`, id, id); err != nil {
			t.Fatal(err)
		}
	}
	regs := push.NewStore(db.SQL)
	for _, r := range []push.Registration{
		{KeyID: "iphone", PersonID: "sam", Handle: "h-iphone", Platform: push.PlatformIOS},
		{KeyID: "ipad", PersonID: "sam", Handle: "h-ipad", Platform: push.PlatformIOS},
	} {
		if err := regs.Set(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	got := map[string]int{}
	ipadDown := true
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var m push.Message
		_ = json.Unmarshal(body, &m)
		mu.Lock()
		defer mu.Unlock()
		if m.Handle == "h-ipad" && ipadDown {
			http.Error(w, `{"error":{"code":"APNS_UNAVAILABLE","message":"try later"}}`, http.StatusServiceUnavailable)
			return
		}
		got[m.Handle]++
	}))
	defer relay.Close()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	p := &pushSender{store: regs, client: &push.Client{Relay: relay.URL, Key: priv}}
	m := gjallarhorn.PushMessage{Title: "Back in stock", NotificationID: "n1"}

	if _, err := p.SendToPerson(ctx, "sam", m); err == nil {
		t.Fatal("the iPad missed it, and that wasn't an error, so it would never be retried")
	}
	mu.Lock()
	ipadDown = false
	mu.Unlock()
	sent, err := p.SendToPerson(ctx, "sam", m)
	if err != nil || sent != 2 {
		t.Fatalf("retry: %d sent, %v", sent, err)
	}
	if got["h-iphone"] != 1 || got["h-ipad"] != 1 {
		t.Fatalf("deliveries %v: the retry should reach the iPad once and skip the iPhone", got)
	}
}
