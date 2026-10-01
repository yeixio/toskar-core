package store

import (
	"strings"
	"testing"
)

func TestMigrationVersionsAreUnique(t *testing.T) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if err := checkVersions(names); err != nil {
		t.Fatal(err)
	}
	err = checkVersions([]string{"016_a.sql", "017_connections.sql", "017_knowledge_vectors.sql"})
	if err == nil || !strings.Contains(err.Error(), "share version 17") {
		t.Fatalf("duplicate versions: %v", err)
	}
}
