package diagnostics

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/config"
)

func TestWriteBundleExcludesSecrets(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.DataDir = dir
	cfg.LogsDir = filepath.Join(dir, "logs")
	cfg.ModelsDir = filepath.Join(dir, "models")
	cfg.RuntimesDir = filepath.Join(dir, "runtimes")
	cfg.DBPath = filepath.Join(dir, "yggdrasil.db")
	cfg.NodeID = "test-node"
	cfg.NodeName = "Test"
	_ = os.MkdirAll(cfg.LogsDir, 0o755)
	_ = os.WriteFile(filepath.Join(cfg.LogsDir, "daemon.log"), []byte("hello"), 0o644)
	_ = os.WriteFile(filepath.Join(cfg.LogsDir, "secret.key"), []byte("nope"), 0o600)

	out := filepath.Join(dir, "bundle.zip")
	if err := WriteBundle(out, Options{Config: cfg, HardwareJSON: []byte(`{"os":"test"}`)}); err != nil {
		t.Fatalf("write: %v", err)
	}
	st, err := os.Stat(out)
	if err != nil || st.Size() == 0 {
		t.Fatalf("expected non-empty bundle")
	}
}

// A bundle is saved in the logs folder it packs, so it must leave itself
// and earlier bundles out: reading the zip it was writing grew one to
// 749 GB on a server (#1505).
func TestBundleLeavesItselfOut(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.DataDir = dir
	cfg.LogsDir = filepath.Join(dir, "logs")
	cfg.DBPath = filepath.Join(dir, "yggdrasil.db")
	_ = os.MkdirAll(cfg.LogsDir, 0o755)
	_ = os.WriteFile(filepath.Join(cfg.LogsDir, "daemon.log"), []byte("hello"), 0o644)
	_ = os.WriteFile(filepath.Join(cfg.LogsDir, "toskar-diagnostics-20260101-000000.zip"), []byte("old bundle"), 0o644)

	out := DefaultBundlePath(cfg)
	// The bundle file exists, and is listed, before its logs are added.
	done := make(chan error, 1)
	go func() { done <- WriteBundle(out, Options{Config: cfg}) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the bundle kept growing")
	}
	st, err := os.Stat(out)
	if err != nil || st.Size() > 10<<20 {
		t.Fatalf("bundle size: %v %v", st, err)
	}
	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var logs []string
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "logs/") {
			logs = append(logs, f.Name)
		}
	}
	if len(logs) != 1 || logs[0] != "logs/daemon.log" {
		t.Fatalf("logs in the bundle: %v", logs)
	}
}
