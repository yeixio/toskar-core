package turnopts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProject(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for in, want := range map[string]string{dir: dir, file: "", "relative/dir": "", filepath.Join(dir, "missing"): "", "": ""} {
		if got := Project(in); got != want {
			t.Errorf("Project(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTrustsFilesIn(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	project := filepath.Join(home, "src", "app")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	for dir, want := range map[string]bool{project: true, home: false, filepath.Dir(home): false, "/": false, "": false} {
		if got := TrustsFilesIn(dir); got != want {
			t.Errorf("TrustsFilesIn(%q) = %v", dir, got)
		}
	}
}

func TestInProject(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(root, "app")
	outside := filepath.Join(root, "secret.txt")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(project, "link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]bool{filepath.Join(project, "a.go"): true, project: true, outside: false, link: false, filepath.Join(project, "..", "x"): false, filepath.Join(root, "app2", "x"): false} {
		if got := InProject(project, path); got != want {
			t.Errorf("InProject(%q) = %v", path, got)
		}
	}
}
