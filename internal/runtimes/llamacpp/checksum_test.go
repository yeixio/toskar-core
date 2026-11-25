package llamacpp

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// The llama.cpp archive is checked against GitHub's published digest
// before anything is replaced (#1209).
func TestInstallChecksTheArchive(t *testing.T) {
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	body := []byte("#!/bin/sh\necho llama-server\n")
	_ = tw.WriteHeader(&tar.Header{Name: "build/bin/llama-server", Mode: 0o755, Size: int64(len(body))})
	_, _ = tw.Write(body)
	_ = tw.Close()
	_ = gz.Close()
	sum := sha256.Sum256(archive.Bytes())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive.Bytes())
	}))
	defer srv.Close()

	install := func(digest string) (*Runtime, error) {
		r := New(t.TempDir(), t.TempDir())
		r.goos, r.goarch = "linux", "amd64"
		r.vulkan = func(context.Context) bool { return false }
		r.releases = func(context.Context) ([]ghRelease, error) {
			return []ghRelease{{Tag: "b7000", Assets: []ghAsset{{
				Name:               "llama-b7000-bin-ubuntu-x64.tar.gz",
				BrowserDownloadURL: srv.URL + "/llama-b7000-bin-ubuntu-x64.tar.gz",
				Digest:             digest,
			}}}}, nil
		}
		_, err := r.install(context.Background(), false)
		return r, err
	}

	r, err := install("sha256:" + strings.Repeat("0", 64))
	if err == nil || !strings.Contains(err.Error(), "published checksum") {
		t.Fatalf("a wrong checksum: %v", err)
	}
	if _, err := os.Stat(r.binaryPath()); err == nil {
		t.Fatal("llama-server was installed from an archive that failed its checksum")
	}

	r, err = install("sha256:" + hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatalf("the right checksum: %v", err)
	}
	if _, err := os.Stat(r.binaryPath()); err != nil {
		t.Fatalf("llama-server wasn't installed: %v", err)
	}

	// Older releases have no digest; they still install.
	if _, err := install(""); err != nil {
		t.Fatalf("no digest: %v", err)
	}
}
