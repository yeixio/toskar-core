package qa

// Copy checks (layer 8 of the test plan): static checks over the text
// people read, from the server's Go code and the English UI strings. They
// run in well under a second.

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// repoText is every Go source (not tests) under internal/, and every
// English UI string file, by path from the repository root.
func repoText(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join("..", "..")
	out := map[string]string{}
	for _, dir := range []string{"internal", filepath.Join("i18n", "locales", "en")} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			name := d.Name()
			if (strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")) || strings.HasSuffix(name, ".json") {
				b, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				rel, _ := filepath.Rel(root, path)
				out[filepath.ToSlash(rel)] = string(b)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// find lists the files and lines where re matches.
func find(files map[string]string, re *regexp.Regexp) []string {
	var hits []string
	for path, text := range files {
		for i, line := range strings.Split(text, "\n") {
			if re.MatchString(line) {
				hits = append(hits, path+":"+itoa(i+1)+": "+strings.TrimSpace(line))
			}
		}
	}
	return hits
}

func itoa(n int) string {
	var b []byte
	for n > 0 || len(b) == 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// Messages point people to where a setting is now: Connected services is on
// Tools, and Find other computers is under Computers → Network & access.
// None of them names a config key.
func TestQAMessagesNameSettingsThatExist(t *testing.T) {
	// Regression: toskar-core#1250
	stale := regexp.MustCompile(`Settings\s*[›→>]\s*(Connected services|Find other computers)|\(discovery_enabled\)`)
	hits := find(repoText(t), stale)
	for _, h := range hits {
		t.Errorf("points to a setting that moved: %s", h)
	}
}

// Labels that tell people what something is use plain names; the Norse
// names stay decorative (the lore button, the mascot).
func TestQAPlainNamesInFunctionalLabels(t *testing.T) {
	// Regression: toskar-core#1251
	norse := regexp.MustCompile(`"(label|hint|nornHint|mimirHint|bifrostHint)"\s*:\s*"[^"]*\b(Bifrost|Mimir|Muninn|Norn|Valgrind|Thrymgjoll|Ymir|Heimdall)\b`)
	files := repoText(t)
	for path := range files {
		if strings.HasSuffix(path, "/lore.json") {
			delete(files, path)
		}
	}
	hits := find(files, norse)
	for _, h := range hits {
		t.Errorf("a Norse name in a functional label: %s", h)
	}
}
