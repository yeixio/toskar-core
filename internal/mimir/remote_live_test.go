package mimir

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestServerDatabases runs database sources against real servers. Each
// subtest is skipped unless its variable holds a connection string for a
// user that may create tables, for example
// YGG_TEST_POSTGRES_DSN=postgres://postgres:pw@127.0.0.1:5432/postgres
// YGG_TEST_MYSQL_DSN=root:pw@tcp(127.0.0.1:3306)/test
func TestServerDatabases(t *testing.T) {
	for _, c := range []struct{ driver, sqlDriver, env string }{
		{DriverPostgres, "pgx", "YGG_TEST_POSTGRES_DSN"},
		{DriverMySQL, "mysql", "YGG_TEST_MYSQL_DSN"},
	} {
		t.Run(c.driver, func(t *testing.T) {
			dsn := os.Getenv(c.env)
			if dsn == "" {
				t.Skipf("set %s to run against %s", c.env, c.driver)
			}
			ctx := context.Background()
			admin, err := sql.Open(c.sqlDriver, dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer admin.Close()
			table := fmt.Sprintf("ygg_products_%d", time.Now().UnixNano())
			if _, err := admin.ExecContext(ctx, "CREATE TABLE "+table+" (sku VARCHAR(20), price DECIMAL(8,2), added DATE)"); err != nil {
				t.Fatal(err)
			}
			defer func() { _, _ = admin.ExecContext(ctx, "DROP TABLE "+table) }()
			if _, err := admin.ExecContext(ctx, "INSERT INTO "+table+" VALUES ('MP-22545', 189.99, '2026-09-01'), ('BW-20555', 142.50, '2026-09-02')"); err != nil {
				t.Fatal(err)
			}

			s := newTestStore(t)
			s.SetSecrets(&memSecrets{})
			src, err := s.Create(ctx, CreateInput{Kind: KindDatabase, Remote: &RemoteInput{
				Driver: c.driver, ConnectionString: dsn, Query: "SELECT sku, price, added FROM " + table + " ORDER BY sku",
			}})
			if err != nil {
				t.Fatal(err)
			}
			if src.Status != StatusReady || src.ChunkCount != 2 {
				t.Fatalf("source = %+v (%s)", src, src.Error)
			}
			hits, _ := s.Search(ctx, SearchInput{Query: "MP-22545", SourceIDs: []string{src.ID}})
			if len(hits) == 0 || !strings.Contains(hits[0].Body, "189.99") || !strings.Contains(hits[0].Body, "added: 2026-09-01") {
				t.Fatalf("hits = %+v", hits)
			}

			// A data-changing statement inside WITH passes the text check;
			// the read-only transaction must stop it.
			if c.driver == DriverPostgres {
				_, err = queryDatabase(ctx, "x", Remote{Driver: c.driver, Query: "WITH d AS (DELETE FROM " + table + " RETURNING *) SELECT * FROM d"},
					remoteSecret{ConnectionString: dsn})
				if err == nil || !strings.Contains(strings.ToLower(err.Error()), "read-only") {
					t.Fatalf("a write ran: %v", err)
				}
			}
			var n int
			if err := admin.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil || n != 2 {
				t.Fatalf("rows = %d, %v", n, err)
			}
		})
	}
}
