package terminal

import (
	"reflect"
	"strings"
	"testing"
)

func TestReaches(t *testing.T) {
	for _, c := range []struct {
		command string
		want    []string
	}{
		{"curl -s https://api.github.com/repos/o/r | jq .stars", []string{"api.github.com"}},
		{"cd app && npm install", []string{"npm"}},
		{"git clone git@github.com:o/r.git && cd r", []string{"github.com"}},
		{"sudo apt-get install -y ffmpeg", []string{"apt-get"}},
		{"python3 -m pip install requests", []string{"pip"}},
		{"HTTPS_PROXY=x wget -q https://example.com/a.tgz https://cdn.example.org/b", []string{"cdn.example.org", "example.com"}},
		{"echo $(curl -s https://ifconfig.me)", []string{"ifconfig.me"}},
		{"npx create-vite app", []string{"npx"}},
	} {
		got, ok := Reaches(c.command)
		if !ok || !reflect.DeepEqual(got.Destinations, c.want) {
			t.Errorf("%q: %v %v, want %v", c.command, got.Destinations, ok, c.want)
		}
	}
	for _, local := range []string{"ls -la", "git status && git commit -m 'curl https://x.com'", "npm test", "go build ./...", "cat notes.txt | grep curl", "echo https://example.com"} {
		if got, ok := Reaches(local); ok {
			t.Errorf("%q reaches %v", local, got.Destinations)
		}
	}
}

func TestReachesScrubsCredentials(t *testing.T) {
	got, ok := Reaches(`curl -H "Authorization: Bearer sk-abc123" https://me:hunter2@api.example.com/x?y=1 -u admin:pw`)
	if !ok {
		t.Fatal("not a network command")
	}
	for _, secret := range []string{"sk-abc123", "hunter2", "admin:pw"} {
		if strings.Contains(got.Command, secret) {
			t.Fatalf("%q kept %q", got.Command, secret)
		}
	}
	if !strings.Contains(got.Command, "api.example.com/x") {
		t.Fatalf("lost the address: %q", got.Command)
	}
}
