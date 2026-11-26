package training

import (
	"os"
	"path/filepath"
	"strings"
)

// BaseWeightsCached reports whether repo's weights are already in the
// Hugging Face cache at hfHome, as huggingface_hub lays it out
// (hub/models--org--name/snapshots/<revision>/*.safetensors), so a run
// reads them from disk and fetches nothing (#1426). The training scripts
// fetch only what is missing, so any weights there count.
func BaseWeightsCached(hfHome, repo string) bool {
	if hfHome == "" || repo == "" {
		return false
	}
	folder := filepath.Join(hfHome, "hub", "models--"+strings.ReplaceAll(repo, "/", "--"), "snapshots")
	revisions, err := os.ReadDir(folder)
	if err != nil {
		return false
	}
	for _, rev := range revisions {
		files, err := os.ReadDir(filepath.Join(folder, rev.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if strings.HasSuffix(f.Name(), ".safetensors") {
				return true
			}
		}
	}
	return false
}
