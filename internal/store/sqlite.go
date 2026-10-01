package store

import (
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// DB wraps SQLite access and migrations.
type DB struct {
	SQL *sql.DB
}

// Open opens (or creates) the SQLite database and runs migrations.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)

	db := &DB{SQL: sqlDB}
	if err := db.Migrate(); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}

// Close closes the database.
func (db *DB) Close() error {
	return db.SQL.Close()
}

// Migrate applies embedded SQL migrations in order.
func (db *DB) Migrate() error {
	if _, err := db.SQL.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`); err != nil {
		return fmt.Errorf("ensure migrations table: %w", err)
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if err := checkVersions(names); err != nil {
		return err
	}

	for _, name := range names {
		var version int
		if _, err := fmt.Sscanf(name, "%d_", &version); err != nil {
			return fmt.Errorf("parse migration version from %s: %w", name, err)
		}
		var exists int
		err := db.SQL.QueryRow(`SELECT COUNT(1) FROM schema_migrations WHERE version = ?`, version).Scan(&exists)
		if err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		tx, err := db.SQL.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// checkVersions refuses two migrations with the same number. Only the
// number is recorded as applied, so the second would be skipped silently.
func checkVersions(names []string) error {
	seen := map[int]string{}
	for _, name := range names {
		var version int
		if _, err := fmt.Sscanf(name, "%d_", &version); err != nil {
			return fmt.Errorf("parse migration version from %s: %w", name, err)
		}
		if other, ok := seen[version]; ok {
			return fmt.Errorf("migrations %s and %s share version %d; renumber one", other, name, version)
		}
		seen[version] = name
	}
	return nil
}
