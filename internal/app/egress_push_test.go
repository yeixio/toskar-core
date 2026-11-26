package app

import "testing"

// A git push is recorded without the user name or token in its remote (#1415).
func TestGitRemoteDropsCredentials(t *testing.T) {
	for _, c := range []struct{ raw, host, where string }{
		{"https://mike:ghp_secret@github.com/yeixio/repo.git", "github.com", "https://github.com/yeixio/repo.git"},
		{"ssh://git@gitlab.com/o/r.git", "gitlab.com", "ssh://gitlab.com/o/r.git"},
		{"git@github.com:o/r.git", "github.com", "github.com:o/r.git"},
		{"origin", "origin", "origin"},
	} {
		host, where := gitRemote(c.raw)
		if host != c.host || where != c.where {
			t.Errorf("%s: %q %q, want %q %q", c.raw, host, where, c.host, c.where)
		}
	}
}
