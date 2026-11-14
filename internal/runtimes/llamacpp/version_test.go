package llamacpp

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The version is llama.cpp's release (#1066), as the Runtimes page and the
// quality results name it: what Toskar wrote when it installed it, else the
// binary's date, as before.
func TestReadVersion(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "llama-server")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if err := os.WriteFile(bin, []byte("not a program"), 0o755); err != nil {
		t.Fatal(err)
	}
	// No release file, as for one installed before: its date, as before.
	if got := readVersion(bin); len(got) != len("2006-01-02") {
		t.Errorf("no release: %q, want a date", got)
	}
	if err := os.WriteFile(filepath.Join(dir, versionFile), []byte("b12361\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readVersion(bin); got != "b12361" {
		t.Errorf("with the release Toskar wrote: %q", got)
	}
}

// The release picked is the one its build came from.
func TestPickAssetKeepsItsRelease(t *testing.T) {
	rels := []ghRelease{
		tagged("b12361", "llama-b12361-bin-macos-arm64.tar.gz"),
		tagged("b12360", "llama-b12360-bin-ubuntu-vulkan-x64.tar.gz"),
	}
	a, err := pickAsset(rels, []string{"ubuntu-vulkan-x64"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Tag != "b12360" || a.Build != "ubuntu-vulkan-x64" {
		t.Errorf("picked %+v", a)
	}
}
