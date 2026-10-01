package mimir

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type memSecrets struct {
	mu sync.Mutex
	m  map[string]string
}

func (s *memSecrets) Write(name, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]string{}
	}
	s.m[name] = value
	return nil
}

func (s *memSecrets) Read(name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[name]
	if !ok {
		return "", errors.New("not found")
	}
	return v, nil
}

func (s *memSecrets) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, name)
	return nil
}

func (s *memSecrets) get(name string) string {
	v, _ := s.Read(name)
	return v
}

// shopDB makes a SQLite database with a products table.
func shopDB(t *testing.T) (string, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shop.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE products (sku TEXT, name TEXT, price REAL, in_stock INTEGER, discontinued INTEGER);
		INSERT INTO products VALUES ('MP-22545', 'Michelin Pilot Sport 4', 189.99, 12, 0), ('BW-20555', 'Bridgestone Blizzak', 142.5, 0, 0),
			('OLD-1', 'Retired tire', 10, 0, 1)`); err != nil {
		t.Fatal(err)
	}
	return path, db
}

func TestSQLiteQueryBecomesSearchableRows(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	path, db := shopDB(t)
	src, err := s.Create(ctx, CreateInput{Kind: KindDatabase, Remote: &RemoteInput{
		Driver: DriverSQLite, Database: path, Query: "SELECT sku, name, price, in_stock FROM products WHERE discontinued = 0",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if src.Status != StatusReady || src.ChunkCount != 2 || src.Name != "shop.db query" || src.Remote.RefreshMinutes != 60 {
		t.Fatalf("source = %+v (%s)", src, src.Error)
	}
	hits, err := s.Search(ctx, SearchInput{Query: "price of MP-22545", SourceIDs: []string{src.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || !strings.Contains(hits[0].Body, "price: 189.99") || !strings.Contains(hits[0].Body, "in stock: 12") {
		t.Fatalf("hits = %+v", hits)
	}

	// The price changes in the database. Within the refresh interval the
	// indexed data is used; after it, a search fetches again in the background.
	if _, err := db.Exec(`UPDATE products SET price = 174 WHERE sku = 'MP-22545'`); err != nil {
		t.Fatal(err)
	}
	if hits, _ := s.Search(ctx, SearchInput{Query: "MP-22545", SourceIDs: []string{src.ID}}); !strings.Contains(hits[0].Body, "189.99") {
		t.Fatalf("refreshed too early: %+v", hits)
	}
	s.now = func() time.Time { return time.Now().Add(61 * time.Minute) }
	_, _ = s.Search(ctx, SearchInput{Query: "MP-22545", SourceIDs: []string{src.ID}})
	deadline := time.Now().Add(5 * time.Second)
	for {
		hits, _ := s.Search(ctx, SearchInput{Query: "MP-22545", SourceIDs: []string{src.ID}})
		if len(hits) > 0 && strings.Contains(hits[0].Body, "price: 174") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the new price never arrived: %+v", hits)
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Let the background fetch finish before the database is closed.
	for deadline := time.Now().Add(5 * time.Second); fetchesRunning(s) && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
}

func fetchesRunning(s *Store) bool {
	running := false
	s.fetching.Range(func(any, any) bool { running = true; return false })
	return running
}

func TestDatabaseSourcesCannotWrite(t *testing.T) {
	path, db := shopDB(t)
	for _, q := range []string{
		"DELETE FROM products",
		"UPDATE products SET price = 0",
		"SELECT 1; DROP TABLE products",
		"-- comment\nINSERT INTO products VALUES ('x','y',1,1,0)",
		"",
	} {
		if err := checkReadOnlyQuery(q); err == nil {
			t.Errorf("%q was accepted", q)
		}
	}
	for _, q := range []string{"SELECT * FROM products", "  with x as (select 1) select * from x;", "/* stock */ SELECT sku FROM products"} {
		if err := checkReadOnlyQuery(q); err != nil {
			t.Errorf("%q was refused: %v", q, err)
		}
	}
	// Even past the check, the file is opened read-only.
	_, err := queryDatabase(context.Background(), "shop", Remote{Driver: DriverSQLite, Database: path, Query: "DELETE FROM products"}, remoteSecret{})
	if err == nil {
		t.Fatal("a write ran against the database")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM products`).Scan(&n); err != nil || n != 3 {
		t.Fatalf("rows = %d, %v", n, err)
	}
}

func TestAPISourceKeepsItsTokenSecret(t *testing.T) {
	ctx := context.Background()
	const token = "Bearer s3cret-token-123"
	var mu sync.Mutex
	price := "189.99"
	fail := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if fail || r.Header.Get("Authorization") != token {
			// A careless API echoes the credential back.
			http.Error(w, "invalid credential "+r.Header.Get("Authorization"), http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": {"products": [{"sku": "MP-22545", "price": ` + price + `}, {"sku": "BW-20555", "price": 142.5}]}, "page": 1}`))
	}))
	defer srv.Close()

	s := newTestStore(t)
	sec := &memSecrets{}
	s.SetSecrets(sec)
	src, err := s.Create(ctx, CreateInput{Kind: KindAPI, Name: "Stock API", Remote: &RemoteInput{
		URL: srv.URL + "/v1/stock", Items: "data.products", Headers: map[string]string{"authorization": token}, RefreshMinutes: 5,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if src.Status != StatusReady || src.ChunkCount != 2 {
		t.Fatalf("source = %+v (%s)", src, src.Error)
	}
	if len(src.Remote.HeaderNames) != 1 || src.Remote.HeaderNames[0] != "Authorization" {
		t.Fatalf("header names = %v", src.Remote.HeaderNames)
	}
	var stored string
	if err := s.db.QueryRow(`SELECT remote_json FROM knowledge_sources WHERE id = ?`, src.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, "s3cret") || !strings.Contains(sec.get(secretName(src.ID)), "s3cret") {
		t.Fatalf("the token is in the database (%s) or not in secrets", stored)
	}

	// Changing settings with a blank header value keeps the token.
	src, err = s.Update(ctx, src.ID, UpdateInput{Remote: &RemoteInput{URL: srv.URL + "/v1/stock", Items: "data.products",
		Headers: map[string]string{"Authorization": ""}, RefreshMinutes: 10}})
	if err != nil || src.Status != StatusReady || src.Remote.RefreshMinutes != 10 {
		t.Fatalf("update = %+v, %v (%s)", src, err, src.Error)
	}

	// The API starts failing. The error does not repeat the token, and the
	// last data stays searchable.
	mu.Lock()
	fail, price = true, "1"
	mu.Unlock()
	if err := s.Refresh(ctx, src.ID); err == nil {
		t.Fatal("refresh should fail")
	}
	src, _ = s.Get(ctx, src.ID)
	if src.Status != StatusFailed || strings.Contains(src.Error, "s3cret") || !strings.Contains(src.Error, "401") ||
		!strings.Contains(src.Error, "last successful fetch") {
		t.Fatalf("error = %q", src.Error)
	}
	hits, _ := s.Search(ctx, SearchInput{Query: "MP-22545 price", SourceIDs: []string{src.ID}})
	if len(hits) == 0 || !strings.Contains(hits[0].Body, "189.99") {
		t.Fatalf("hits = %+v", hits)
	}

	if err := s.Delete(ctx, src.ID); err != nil {
		t.Fatal(err)
	}
	if sec.get(secretName(src.ID)) != "" {
		t.Fatal("the token outlived its source")
	}
}

func TestAPIResponsesBecomeTablesOrText(t *testing.T) {
	cases := []struct {
		name, items, body string
		rows              int
		text              string
	}{
		{"top-level list", "", `[{"a": 1}, {"a": 2}]`, 2, ""},
		{"one list inside", "", `{"results": [{"a": 1}], "count": 1}`, 1, ""},
		{"named path", "x.y", `{"x": {"y": [{"a": 1}, {"a": 2}, {"a": 3}]}}`, 3, ""},
		{"two lists stays text", "", `{"a": [{"b": 1}], "c": [{"d": 2}]}`, 0, `"b": 1`},
		{"object stays text", "", `{"open": "9-5"}`, 0, `"open": "9-5"`},
	}
	for _, c := range cases {
		docs, err := apiJSON("api", c.items, []byte(c.body))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if len(docs[0].Rows) != c.rows || !strings.Contains(docs[0].Text, c.text) {
			t.Errorf("%s: %+v", c.name, docs[0])
		}
	}
	if _, err := apiJSON("api", "missing.path", []byte(`{"x": 1}`)); err == nil || !strings.Contains(err.Error(), "missing.path") {
		t.Fatalf("got %v", err)
	}
}

func TestRemoteSettingsAreChecked(t *testing.T) {
	bad := []struct {
		kind Kind
		in   RemoteInput
		want string
	}{
		{KindDatabase, RemoteInput{Driver: DriverPostgres, Query: "SELECT 1"}, "connection string"},
		{KindDatabase, RemoteInput{Driver: "oracle", Query: "SELECT 1", ConnectionString: "x"}, "driver"},
		{KindDatabase, RemoteInput{Driver: DriverSQLite, Query: "SELECT 1"}, "SQLite database file"},
		{KindAPI, RemoteInput{URL: "ftp://example.com/x"}, "http"},
		{KindAPI, RemoteInput{URL: "https://user:pw@example.com/x"}, "header"},
		{KindAPI, RemoteInput{URL: "https://example.com/x", RefreshMinutes: -5}, "refresh"},
	}
	for _, c := range bad {
		if _, _, err := buildRemote(c.kind, c.in, remoteSecret{}); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: got %v, want %q", c.in, err, c.want)
		}
	}
	// A stored connection string is kept when the update leaves it blank.
	_, sec, err := buildRemote(KindDatabase, RemoteInput{Driver: DriverMySQL, Query: "SELECT 1"}, remoteSecret{ConnectionString: "reader:pw@tcp(db:3306)/shop"})
	if err != nil || sec.ConnectionString != "reader:pw@tcp(db:3306)/shop" {
		t.Fatalf("got %+v, %v", sec, err)
	}
	// Credentials need somewhere safe to go.
	s := newTestStore(t)
	if _, err := s.Create(context.Background(), CreateInput{Kind: KindAPI, Remote: &RemoteInput{URL: "https://example.com", Headers: map[string]string{"X-Key": "k"}}}); err == nil {
		t.Fatal("a header was accepted with nowhere to store it")
	}
}

func TestConnectionErrorsHidePasswords(t *testing.T) {
	for _, dsn := range []string{"postgres://reader:hunter2pw@db.local:5432/shop", "reader:hunter2pw@tcp(db.local:3306)/shop"} {
		err := redact(errors.New("failed to connect to "+dsn+": password hunter2pw rejected"), remoteSecret{ConnectionString: dsn})
		if strings.Contains(err.Error(), "hunter2pw") {
			t.Errorf("leaked: %v", err)
		}
	}
}
