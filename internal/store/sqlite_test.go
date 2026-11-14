package store_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/yeixio/toskar-core/internal/store"
)

func TestOpenMigrates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	var n int
	if err := db.SQL.QueryRow(`SELECT COUNT(1) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatalf("query migrations: %v", err)
	}
	if n < 1 {
		t.Fatalf("expected migrations applied, got %d", n)
	}

	// Re-open should be idempotent.
	db2, err := store.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db2.Close()
}

// A database is opened in its own folder whatever the folder is called: a
// # or ? ended the path of the URI SQLite opened, so case#1, case#2, and
// every later run shared one database outside them.
func TestOpenInAFolderWithURICharacters(t *testing.T) {
	names := []string{"case#1", "case#2", "50%", "a%23b"}
	if runtime.GOOS != "windows" {
		names = append(names, "why?")
	}
	root := t.TempDir()
	for i, name := range names {
		path := filepath.Join(root, name, "toskar.db")
		db, err := store.Open(path)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := db.SQL.Exec(`CREATE TABLE IF NOT EXISTS probe (n INTEGER)`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.SQL.Exec(`INSERT INTO probe (n) VALUES (?)`, i); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := db.SQL.QueryRow(`SELECT count(*) FROM probe`).Scan(&n); err != nil || n != 1 {
			t.Errorf("%s: %d rows, %v: it shares a database with another folder", name, n, err)
		}
		_ = db.Close()
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s: no database in the folder: %v", name, err)
		}
	}
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if !e.IsDir() {
			t.Errorf("%s was opened beside the folders", e.Name())
		}
	}
}
