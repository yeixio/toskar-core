package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/store"
)

// What each role and person may use (#589), through the API: GET /me says,
// routes and uploads refuse with CAPABILITY_OFF, and only the Owner
// changes the Admins'.
func TestCapabilitiesAPI(t *testing.T) {
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
	srv := NewServer(Dependencies{Config: mgr, People: people, Sessions: auth.NewSessions(db.SQL), Invites: auth.NewInvites(db.SQL),
		VerifyAPIKey: keys.Verify, PeopleActivity: auth.NewActivity(db.SQL)})
	srv.BindArtifacts(artifacts.NewStore(db.SQL, filepath.Join(dir, "files")))

	call := func(cookie *http.Cookie, method, path, body string) (int, map[string]any, *http.Cookie) {
		// Without a cookie, the Owner at this computer; with one, a browser
		// on another device.
		r := localRequest(method, path, strings.NewReader(body))
		if cookie != nil {
			r = httptest.NewRequest(method, path, strings.NewReader(body))
			r.Host, r.RemoteAddr = "toskar.local:7331", "192.168.1.30:51000"
			r.Header.Set("Origin", "http://toskar.local:7331")
			r.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, r)
		out := map[string]any{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		var c *http.Cookie
		for _, k := range rec.Result().Cookies() {
			if k.Name == auth.SessionCookie {
				c = k
			}
		}
		return rec.Code, out, c
	}
	code := func(body map[string]any) string {
		e, _ := body["error"].(map[string]any)
		s, _ := e["code"].(string)
		return s
	}
	signIn := func(name, role string) *http.Cookie {
		user := strings.ToLower(name) + "-user"
		if c, b, _ := call(nil, http.MethodPost, "/api/v1/people", `{"name":"`+name+`","role":"`+role+`","username":"`+user+`","password":"a long password"}`); c != http.StatusCreated {
			t.Fatalf("add %s: %d %v", name, c, b)
		}
		r := httptest.NewRequest(http.MethodPost, "/api/v1/session", strings.NewReader(`{"username":"`+user+`","password":"a long password"}`))
		r.Host, r.RemoteAddr = "toskar.local:7331", "192.168.1.30:51000"
		r.Header.Set("Origin", "http://toskar.local:7331")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, r)
		for _, k := range rec.Result().Cookies() {
			if k.Name == auth.SessionCookie {
				return k
			}
		}
		t.Fatalf("sign in %s: %d %s", name, rec.Code, rec.Body)
		return nil
	}

	vic := signIn("Vic", "visitor")
	mo := signIn("Mo", "member")
	ada := signIn("Ada", "admin")

	// GET /me says what each may use.
	_, me, _ := call(vic, http.MethodGet, "/api/v1/me", "")
	caps, _ := me["capabilities"].(map[string]any)
	if caps["files"] != false || caps["web"] != true || caps["make_pictures"] != false {
		t.Fatalf("visitor's capabilities %v", me["capabilities"])
	}

	// A Visitor can't upload a document, though a picture gets past the check.
	if c, b, _ := call(vic, http.MethodPost, "/api/v1/artifacts", `{"name":"notes.txt","text":"hello"}`); c != http.StatusForbidden || code(b) != "CAPABILITY_OFF" {
		t.Fatalf("visitor's document: %d %v", c, b)
	}
	if _, b, _ := call(vic, http.MethodPost, "/api/v1/artifacts", `{"name":"cat.png","content_base64":""}`); code(b) == "CAPABILITY_OFF" {
		t.Fatalf("visitor's picture refused: %v", b)
	}

	// Members lose automations; Mo is refused, with the Owner named.
	if c, b, _ := call(nil, http.MethodPut, "/api/v1/roles/member/capabilities", `{"automations":false}`); c != http.StatusOK || b["automations"] != false {
		t.Fatalf("set member: %d %v", c, b)
	}
	c, b, _ := call(mo, http.MethodPost, "/api/v1/automations/parse", `{"text":"every day at 8"}`)
	if details, _ := b["error"].(map[string]any)["details"].(map[string]any); c != http.StatusForbidden || details["capability"] != "automations" || details["admin"] != "Owner" {
		t.Fatalf("member's automation: %d %v", c, b)
	}

	// Mo gets it back alone.
	var moID string
	all, _ := people.List(t.Context())
	for _, p := range all {
		if p.Name == "Mo" {
			moID = p.ID
		}
	}
	if c, b, _ := call(nil, http.MethodPut, "/api/v1/people/"+moID+"/capabilities", `{"automations":true}`); c != http.StatusOK || b["effective"].(map[string]any)["automations"] != true {
		t.Fatalf("allow Mo: %d %v", c, b)
	}
	if _, b, _ := call(mo, http.MethodPost, "/api/v1/automations/parse", `{"text":"every day at 8"}`); code(b) == "CAPABILITY_OFF" {
		t.Fatalf("Mo still refused: %v", b)
	}

	// An Admin can't change what Admins may use; the Owner can't lose anything.
	if c, b, _ := call(ada, http.MethodPut, "/api/v1/roles/admin/capabilities", `{"web":false}`); c != http.StatusForbidden || code(b) != "ROLE_REQUIRED" {
		t.Fatalf("admin on admins: %d %v", c, b)
	}
	if c, b, _ := call(nil, http.MethodPut, "/api/v1/roles/owner/capabilities", `{"web":false}`); code(b) != "OWNER_FIXED" {
		t.Fatalf("owner's: %d %v", c, b)
	}
	if c, b, _ := call(nil, http.MethodGet, "/api/v1/roles/capabilities", ""); c != http.StatusOK || len(b["capabilities"].([]any)) != len(auth.AllCapabilities) {
		t.Fatalf("roles: %d %v", c, b)
	}
}
