package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// toskarctl people (#592): flags anywhere, a password from standard input
// only, names or IDs, a confirmation to remove, and --output json.
func TestPeopleCommand(t *testing.T) {
	everyone := []map[string]any{
		{"id": "owner", "name": "Owner", "role": "owner"},
		{"id": "p-ana", "name": "Ana", "username": "ana", "role": "admin", "sign_in": true},
		{"id": "p-bob", "name": "Bob", "role": "member"},
		{"id": "p-bob2", "name": "bob", "role": "visitor"},
	}
	var got []string
	var lastBody map[string]any
	record := func(status int, v any) func(w http.ResponseWriter, r *http.Request) {
		return func(w http.ResponseWriter, r *http.Request) {
			lastBody = map[string]any{}
			_ = json.NewDecoder(r.Body).Decode(&lastBody)
			got = append(got, r.Method+" "+r.URL.RequestURI())
			reply(status, v)(w, r)
		}
	}
	c := fakeDaemon(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"GET /api/v1/people":            record(200, everyone),
		"POST /api/v1/people":           record(201, map[string]any{"person": map[string]any{"id": "p-cy", "name": "Cy", "role": "member", "username": "cy", "must_change": true}}),
		"PATCH /api/v1/people/p-ana":    record(200, map[string]any{"id": "p-ana", "name": "Ana", "role": "member", "sign_in": true}),
		"DELETE /api/v1/people/p-ana":   record(204, nil),
		"GET /api/v1/people/p-ana/keys": record(200, []map[string]any{{"id": "k1", "name": "scripts", "prefix": "ygg_ab"}}),
	})
	run := func(stdin string, args ...string) (string, error) {
		var out, prompt bytes.Buffer
		err := peopleCommand(args, c, peopleIO{in: strings.NewReader(stdin), out: &out, prompt: &prompt})
		return out.String() + prompt.String(), err
	}

	// Flags after the name; the password from standard input.
	out, err := run("a long password\n", "add", "Cy", "--role", "member", "--username", "cy", "--password-stdin", "--must-change")
	if err != nil || !strings.Contains(out, "Added Cy (member") || !strings.Contains(out, "choose a new password first") {
		t.Fatalf("add: %v %q", err, out)
	}
	if lastBody["password"] != "a long password" || lastBody["username"] != "cy" || lastBody["must_change"] != true || lastBody["role"] != "member" {
		t.Fatalf("add body %v", lastBody)
	}
	if _, err := run("", "add", "Cy", "--password-stdin"); err == nil || !strings.Contains(err.Error(), "--username") {
		t.Fatalf("password without username: %v", err)
	}
	if _, err := run("", "add", "Cy", "--username", "cy", "--password-stdin"); err == nil || !strings.Contains(err.Error(), "no password") {
		t.Fatalf("empty stdin: %v", err)
	}

	// By name, case and all; two called bob need an ID.
	if _, err := run("", "set", "ANA", "--role", "member"); err != nil || lastBody["role"] != "member" || len(lastBody) != 1 {
		t.Fatalf("set by name: %v %v", err, lastBody)
	}
	if _, err := run("", "show", "bob"); err == nil || !strings.Contains(err.Error(), "p-bob, p-bob2") {
		t.Fatalf("ambiguous: %v", err)
	}
	if _, err := run("", "show", "Zed"); err == nil || !strings.Contains(err.Error(), "nobody here") {
		t.Fatalf("unknown: %v", err)
	}

	// Removing asks for the name, unless --yes, and always sends it.
	got = nil
	if _, err := run("Anna\n", "rm", "ana"); err == nil || len(got) != 1 {
		t.Fatalf("wrong name: %v %v", err, got)
	}
	out, err = run("Ana\n", "rm", "ana", "--transfer-to", "Owner")
	if err != nil || !strings.Contains(out, "go to Owner") || got[len(got)-1] != "DELETE /api/v1/people/p-ana?transfer_to=owner" || lastBody["confirm"] != "Ana" {
		t.Fatalf("rm: %v %q %v %v", err, out, got, lastBody)
	}

	// JSON for scripts.
	out, err = run("", "keys", "p-ana", "--output", "json")
	var keys []map[string]any
	if err != nil || json.Unmarshal([]byte(out), &keys) != nil || keys[0]["id"] != "k1" {
		t.Fatalf("keys json: %v %q", err, out)
	}

	// Usage mistakes exit 2.
	var e *exitError
	if _, err := run("", "frobnicate"); !errors.As(err, &e) || e.code != 2 {
		t.Fatalf("unknown command: %v", err)
	}
}
