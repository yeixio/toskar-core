package nodes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/yggdrasil-core/internal/auth"
	"github.com/yeixio/yggdrasil-core/internal/store"
)

// A paired computer that never answers must not make every chat wait.
func TestOfflinePeerDoesNotDelayEveryChat(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	hang := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-hang }))
	defer srv.Close()
	defer close(hang)

	addr := strings.TrimPrefix(srv.URL, "http://")
	if _, err := db.SQL.Exec(`INSERT INTO nodes (id, name, status, is_local, address) VALUES ('peer', 'Studio', 'online', 0, ?)`, addr); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL.Exec(`INSERT INTO node_trust (node_id, fingerprint) VALUES ('peer', 'fp')`); err != nil {
		t.Fatal(err)
	}
	local, err := auth.LoadOrCreateIdentity(auth.NewSecretStore(dir), "local")
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(db.SQL, nil, auth.NewPairingManager(db.SQL, local), "local", "Local", nil)
	ctx := context.Background()

	start := time.Now()
	m.RefreshPairedLivenessIfStale(ctx)
	if d := time.Since(start); d > livenessProbeCap+500*time.Millisecond {
		t.Fatalf("first placement waited %v for an unresponsive peer", d)
	}
	start = time.Now()
	m.RefreshPairedLivenessIfStale(ctx)
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Fatalf("a fresh result should be reused, waited %v", d)
	}
	var status string
	_ = db.SQL.QueryRow(`SELECT status FROM nodes WHERE id = 'peer'`).Scan(&status)
	if status != "offline" {
		t.Fatalf("status = %s", status)
	}
}
