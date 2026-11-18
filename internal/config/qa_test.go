package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yeixio/toskar-core/internal/qa"
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
	qa.KnownBug(t, "toskar-core#1265", func(t testing.TB) {
		if got != oldDir {
			t.Errorf("chose %s, an empty folder, over %s, which has the database", got, oldDir)
		}
	})
}
