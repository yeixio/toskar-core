// Package dlmirror sends large downloads through a caching server on the
// local network first, when TOSKAR_DOWNLOAD_MIRROR names one. It is for
// computers that download the same models again and again, such as a set
// of CI runners, so each file crosses the internet connection once.
//
// The mirror is asked first and the original address second, so a mirror
// that is down, or doesn't have a file, only costs one failed request.
package dlmirror

import (
	"os"
	"strings"
)

// Env names the mirror, such as http://192.168.1.20:8380. Unset, every
// download goes straight to its source.
const Env = "TOSKAR_DOWNLOAD_MIRROR"

// prefixes maps each source the mirror caches to its path on the mirror.
// The mirror's nginx configuration (ops/lan-cache) uses the same paths.
var prefixes = []struct{ source, path string }{
	{"https://huggingface.co/", "/hf/"},
}

// URLs returns the addresses to try for raw, in order: the mirror's copy
// when a mirror is set and caches raw's source, then raw itself.
func URLs(raw string) []string {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv(Env)), "/")
	if base == "" || !(strings.HasPrefix(base, "http://") || strings.HasPrefix(base, "https://")) {
		return []string{raw}
	}
	for _, p := range prefixes {
		if rest, ok := strings.CutPrefix(raw, p.source); ok && rest != "" {
			return []string{base + p.path + rest, raw}
		}
	}
	return []string{raw}
}
