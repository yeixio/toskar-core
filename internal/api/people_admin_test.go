package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/store"
)

// Managing people without a browser (#592): adding someone with a
// password they must change, the change before anything else, filters,
// one person, the role rules, removing with a transfer and a confirmation,
// their keys, and a log that never holds a password.
func TestManagePeople(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mgr, err := config.NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Update(func(c *config.Config) { c.APIHost = "0.0.0.0" }); err != nil {
		t.Fatal(err)
	}
	people := auth.NewPeople(db.SQL)
	keys := auth.NewAPIKeyManager(db.SQL, auth.NewSecretStore(dir))
	var removed []string
	srv := NewServer(Dependencies{Config: mgr, People: people, Sessions: auth.NewSessions(db.SQL), Invites: auth.NewInvites(db.SQL),
		VerifyAPIKey: keys.Verify, ListAPIKeys: keys.List, RevokeAPIKey: keys.Revoke, PeopleActivity: auth.NewActivity(db.SQL),
		RemovePerson: func(ctx context.Context, id, transferTo string) error {
			removed = append(removed, id+"→"+transferTo)
			return people.Delete(ctx, id)
		}})

	type reply struct {
		code   int
		body   any
		cookie *http.Cookie
	}
	send := func(r *http.Request) reply {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, r)
		out := reply{code: rec.Code}
		_ = json.Unmarshal(rec.Body.Bytes(), &out.body)
		for _, c := range rec.Result().Cookies() {
			if c.Name == auth.SessionCookie {
				out.cookie = c
			}
		}
		return out
	}
	owner := func(method, path, body string) reply {
		return send(localRequest(method, path, strings.NewReader(body)))
	}
	as := func(cookie *http.Cookie, method, path, body string) reply {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Host = "toskar.local:7331"
		r.RemoteAddr = "192.168.1.30:51000"
		r.Header.Set("Origin", "http://toskar.local:7331")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		return send(r)
	}
	field := func(r reply, path ...string) any {
		v := r.body
		for _, k := range path {
			m, _ := v.(map[string]any)
			v = m[k]
		}
		return v
	}
	code := func(r reply) string { s, _ := field(r, "error", "code").(string); return s }

	// The Owner adds Ana, an Admin, with a password she must change.
	const given = "given password 123"
	added := owner(http.MethodPost, "/api/v1/people", `{"name":"Ana","role":"admin","email":"ana@example.com","username":"ana","password":"`+given+`","must_change":true}`)
	if added.code != http.StatusCreated || field(added, "link") != nil || field(added, "person", "must_change") != true || field(added, "person", "email") != "ana@example.com" {
		t.Fatalf("add Ana: %d %v", added.code, added.body)
	}
	anaID := field(added, "person", "id").(string)
	if r := owner(http.MethodPost, "/api/v1/people", `{"name":"Ana 2","email":"ANA@example.com"}`); code(r) != "EMAIL_TAKEN" {
		t.Fatalf("same email: %d %v", r.code, r.body)
	}

	// She signs in, and must change it before anything else.
	signed := as(nil, http.MethodPost, "/api/v1/session", `{"username":"ana","password":"`+given+`"}`)
	if signed.code != http.StatusOK || signed.cookie == nil {
		t.Fatalf("sign in: %d %v", signed.code, signed.body)
	}
	ana := signed.cookie
	if r := as(ana, http.MethodGet, "/api/v1/people", ""); code(r) != "PASSWORD_CHANGE_REQUIRED" {
		t.Fatalf("before changing: %d %v", r.code, r.body)
	}
	if r := as(ana, http.MethodGet, "/api/v1/me", ""); r.code != http.StatusOK || field(r, "person", "must_change") != true {
		t.Fatalf("me: %d %v", r.code, r.body)
	}
	if r := as(ana, http.MethodPost, "/api/v1/me/password", `{"current":"wrong password!!","new":"her own password"}`); code(r) != "PASSWORD_WRONG" {
		t.Fatalf("wrong current: %d %v", r.code, r.body)
	}
	changed := as(ana, http.MethodPost, "/api/v1/me/password", `{"current":"`+given+`","new":"her own password"}`)
	if changed.code != http.StatusOK || changed.cookie == nil || field(changed, "person", "must_change") != nil {
		t.Fatalf("change: %d %v", changed.code, changed.body)
	}
	ana = changed.cookie
	if r := as(ana, http.MethodGet, "/api/v1/people", ""); r.code != http.StatusOK {
		t.Fatalf("after changing: %d %v", r.code, r.body)
	}

	// Ana, an Admin, adds Bob; the Owner adds Cy, another Admin.
	bob := as(ana, http.MethodPost, "/api/v1/people", `{"name":"Bob"}`)
	bobID, _ := field(bob, "person", "id").(string)
	cy := owner(http.MethodPost, "/api/v1/people", `{"name":"Cy","role":"admin"}`)
	cyID, _ := field(cy, "person", "id").(string)
	if bobID == "" || cyID == "" {
		t.Fatalf("add Bob %v, Cy %v", bob.body, cy.body)
	}

	// Filters, and one person.
	admins := owner(http.MethodGet, "/api/v1/people?role=admin", "")
	if list, _ := admins.body.([]any); len(list) != 2 {
		t.Fatalf("admins: %v", admins.body)
	}
	if r := owner(http.MethodGet, "/api/v1/people/"+anaID, ""); field(r, "name") != "Ana" || field(r, "email") != "ana@example.com" {
		t.Fatalf("one person: %d %v", r.code, r.body)
	}

	// An Admin can't touch another Admin or the Owner, or remove herself.
	for _, c := range []struct{ method, path, body, want string }{
		{http.MethodPatch, "/api/v1/people/" + cyID, `{"password":"a new password!"}`, "ROLE_REQUIRED"},
		{http.MethodDelete, "/api/v1/people/" + cyID, `{"confirm":"Cy"}`, "ROLE_REQUIRED"},
		{http.MethodDelete, "/api/v1/people/owner", `{"confirm":"Owner"}`, "ROLE_REQUIRED"},
		{http.MethodDelete, "/api/v1/people/" + anaID, `{"confirm":"Ana"}`, "OWNER_FIXED"},
		{http.MethodGet, "/api/v1/people/" + cyID + "/keys", ``, "ROLE_REQUIRED"},
	} {
		if r := as(ana, c.method, c.path, c.body); code(r) != c.want {
			t.Errorf("%s %s: %d %v, want %s", c.method, c.path, r.code, r.body, c.want)
		}
	}

	// Bob's key, seen and revoked by an Admin.
	key, _, err := keys.Create(auth.AsPerson(context.Background(), auth.Person{ID: bobID, Role: auth.RoleMember}), "scripts")
	if err != nil {
		t.Fatal(err)
	}
	if r := as(ana, http.MethodGet, "/api/v1/people/"+bobID+"/keys", ""); r.code != http.StatusOK || len(r.body.([]any)) != 1 {
		t.Fatalf("Bob's keys: %d %v", r.code, r.body)
	}
	if r := as(ana, http.MethodDelete, "/api/v1/people/"+bobID+"/keys/"+key.ID, ""); r.code != http.StatusNoContent {
		t.Fatalf("revoke: %d %v", r.code, r.body)
	}

	// Removing Bob needs his name, and gives his data to the Owner.
	if r := as(ana, http.MethodDelete, "/api/v1/people/"+bobID, `{"confirm":"bob"}`); code(r) != "CONFIRM_NAME" {
		t.Fatalf("without confirming: %d %v", r.code, r.body)
	}
	if r := as(ana, http.MethodDelete, "/api/v1/people/"+bobID+"?transfer_to=nobody", `{"confirm":"Bob"}`); code(r) != "TRANSFER_INVALID" {
		t.Fatalf("transfer to nobody: %d %v", r.code, r.body)
	}
	if r := as(ana, http.MethodDelete, "/api/v1/people/"+bobID+"?transfer_to=owner", `{"confirm":"Bob"}`); r.code != http.StatusNoContent {
		t.Fatalf("remove: %d %v", r.code, r.body)
	}
	if len(removed) != 1 || removed[0] != bobID+"→owner" {
		t.Fatalf("removed %v", removed)
	}
	if r := owner(http.MethodGet, "/api/v1/people/"+bobID, ""); code(r) != "PERSON_NOT_FOUND" {
		t.Fatalf("Bob after: %d %v", r.code, r.body)
	}

	// The Owner sets Cy's password; the log says who did what, never the password.
	if r := owner(http.MethodPatch, "/api/v1/people/"+cyID, `{"name":"Cyrus","username":"cyrus","password":"cy's password!","must_change":true}`); r.code != http.StatusOK || field(r, "must_change") != true || field(r, "sign_in") != true {
		t.Fatalf("set Cy's password: %d %v", r.code, r.body)
	}
	log := owner(http.MethodGet, "/api/v1/people/activity", "")
	raw, _ := json.Marshal(log.body)
	for _, want := range []string{`"action":"added"`, `"action":"password_set"`, `"action":"password_changed"`, `"action":"key_revoked"`, `"action":"deleted"`, `their data given to Owner`, `name Cy → Cyrus`, `"actor_name":"Ana"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("activity lacks %s: %s", want, raw)
		}
	}
	for _, secret := range []string{given, "her own password", "cy's password!"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("activity holds a password: %s", raw)
		}
	}
}
