package config

import (
	"os"
	"path/filepath"
	"testing"
)

// A stray config.json in the new Toskar folder doesn't switch a
// Yggdrasil-era install to an empty folder: the folder with the database
// wins.
func TestQAStrayConfigKeepsTheOldData(t *testing.T) {
	// Regression: toskar-core#1265
	base := t.TempDir()
	newDir, oldDir := filepath.Join(base, "Toskar"), filepath.Join(base, "Yggdrasil")
	for _, dir := range []string{newDir, oldDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Something wrote a config.json into the new folder; the data is in the old one.
	if err := os.WriteFile(filepath.Join(newDir, "config.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "yggdrasil.db"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := chooseDataDir(newDir, oldDir)
	if got != oldDir {
		t.Errorf("chose %s, an empty folder, over %s, which has the database", got, oldDir)
	}
	// Once the new folder has its own database, it's the one.
	if err := os.WriteFile(filepath.Join(newDir, "toskar.db"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := chooseDataDir(newDir, oldDir); got != newDir {
		t.Errorf("chose %s over the new folder with its own database", got)
	}
}
