package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/portals"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A visitor enters a portal with its passcode and chats there, with the
// portal's settings, and reaches nothing else; the main app in the same
// browser is untouched, and turning the portal off stops them (#205).
func TestPortalGuests(t *testing.T) {
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
	store := portals.NewStore(db.SQL)
	code, name := "tide-pool", "Help desk"
	slug, tools, lang := "support", portals.ToolsReadOnly, "es"
	portal, err := store.Create(context.Background(), portals.Input{Slug: &slug, Name: &name, Passcode: &code, Tools: &tools, Language: &lang})
	if err != nil {
		t.Fatal(err)
	}
	type chatCall struct {
		person, profile, model string
		opts                   *turnopts.Options
	}
	var calls []chatCall
	srv := NewServer(Dependencies{
		Config: mgr, People: auth.NewPeople(db.SQL), Sessions: auth.NewSessions(db.SQL), Invites: auth.NewInvites(db.SQL), Portals: store,
		Chat: func(w http.ResponseWriter, r *http.Request, conversationID, profileID, modelID, message string, stream bool, execution string) error {
			calls = append(calls, chatCall{auth.PersonID(r.Context()), profileID, modelID, turnopts.From(r.Context())})
			writeJSON(w, http.StatusOK, contracts.ChatAnswer{Content: "yes"})
			return nil
		},
	})
	do := func(method, path, body string, headers map[string]string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.RemoteAddr = "192.168.1.40:5000"
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		for _, c := range cookies {
			r.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, r)
		return rec
	}
	portalHeaders := map[string]string{"X-Toskar-Portal": "support", "Origin": "http://example.com"}

	page := do(http.MethodGet, "/api/v1/portals/support/page", "", nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `"name":"Help desk"`) || strings.Contains(page.Body.String(), "argon2") {
		t.Fatalf("page: %d %s", page.Code, page.Body)
	}
	if rec := do(http.MethodPost, "/api/v1/portals/support/enter", `{"passcode":"wrong"}`, portalHeaders); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong passcode: %d", rec.Code)
	}
	enter := do(http.MethodPost, "/api/v1/portals/support/enter", `{"passcode":"tide-pool"}`, portalHeaders)
	if enter.Code != http.StatusOK {
		t.Fatalf("enter: %d %s", enter.Code, enter.Body)
	}
	var guestCookie *http.Cookie
	for _, c := range enter.Result().Cookies() {
		if c.Name == "toskar_portal_support" {
			guestCookie = c
		}
	}
	if guestCookie == nil || !guestCookie.HttpOnly {
		t.Fatalf("guest cookie %+v", guestCookie)
	}
	var guest auth.Principal
	_ = json.NewDecoder(enter.Body).Decode(&guest)
	if guest.Person.PortalID != portal.ID {
		t.Fatalf("guest %+v", guest)
	}

	// Chatting takes the portal's profile, tools, and language, whatever
	// the request asks for.
	if rec := do(http.MethodPost, "/api/v1/chat", `{"message":"hola","profile_id":"programming","model_id":"big-model"}`, portalHeaders, guestCookie); rec.Code != http.StatusOK {
		t.Fatalf("chat: %d %s", rec.Code, rec.Body)
	}
	if len(calls) != 1 || calls[0].person != guest.Person.ID || calls[0].profile != "" || calls[0].model != "" || calls[0].opts == nil ||
		!calls[0].opts.FixedProfile || !calls[0].opts.ReadOnlyTools || calls[0].opts.Memory || calls[0].opts.Language != "es" {
		t.Fatalf("chat call %+v %+v", calls, calls[0].opts)
	}
	if rec := do(http.MethodGet, "/api/v1/conversations", "", portalHeaders, guestCookie); rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
		t.Fatalf("conversations: %d", rec.Code)
	}
	for _, path := range []string{"/api/v1/settings", "/api/v1/models", "/api/v1/memory", "/api/v1/people"} {
		if rec := do(http.MethodGet, path, "", portalHeaders, guestCookie); rec.Code != http.StatusForbidden {
			t.Errorf("guest reached %s: %d", path, rec.Code)
		}
	}
	// The same session as an ordinary sign-in is still the portal's guest.
	stolen := &http.Cookie{Name: auth.SessionCookie, Value: guestCookie.Value}
	if rec := do(http.MethodGet, "/api/v1/settings", "", nil, stolen); rec.Code != http.StatusForbidden {
		t.Fatalf("the guest's session as a sign-in: %d", rec.Code)
	}
	// Without the portal's header, the guest cookie is nobody.
	if rec := do(http.MethodGet, "/api/v1/conversations", "", nil, guestCookie); rec.Code != http.StatusUnauthorized {
		t.Fatalf("guest cookie without the header: %d", rec.Code)
	}
	// Entering again is the same guest.
	again := do(http.MethodPost, "/api/v1/portals/support/enter", `{}`, portalHeaders, guestCookie)
	var same auth.Principal
	_ = json.NewDecoder(again.Body).Decode(&same)
	if same.Person.ID != guest.Person.ID {
		t.Fatalf("entered again as %+v", same)
	}

	off := false
	if _, err := store.Update(context.Background(), portal.ID, portals.Input{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	if rec := do(http.MethodPost, "/api/v1/chat", `{"message":"still there?"}`, portalHeaders, guestCookie); rec.Code != http.StatusForbidden || len(calls) != 1 {
		t.Fatalf("chat after turning it off: %d, %d calls", rec.Code, len(calls))
	}
	if rec := do(http.MethodGet, "/api/v1/portals/support/page", "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("page after turning it off: %d", rec.Code)
	}
}

func TestPortalLimiter(t *testing.T) {
	var l portalLimiter
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		if !l.allow("g", 3) {
			t.Fatalf("message %d refused", i)
		}
	}
	if l.allow("g", 3) || !l.allow("other", 3) || !l.allow("g", 0) {
		t.Fatal("hourly limit")
	}
	now = now.Add(61 * time.Minute)
	if !l.allow("g", 3) {
		t.Fatal("an hour later")
	}
	done1, ok1 := l.enter("p", 2)
	_, ok2 := l.enter("p", 2)
	if _, ok3 := l.enter("p", 2); !ok1 || !ok2 || ok3 {
		t.Fatalf("concurrency %v %v %v", ok1, ok2, ok3)
	}
	done1()
	if _, ok := l.enter("p", 2); !ok {
		t.Fatal("a chat that ended still counted")
	}
}

// A portal's guest is held to its limits (#205).
func TestPortalLimits(t *testing.T) {
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
	ps := portals.NewStore(db.SQL)
	slug, name, access, hourly, longest, most := "help", "Help", portals.AccessOpen, 2, 100, 1
	if _, err := ps.Create(context.Background(), portals.Input{Slug: &slug, Name: &name, Access: &access, HourlyLimit: &hourly, MaxMessage: &longest, Concurrency: &most}); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	started := make(chan struct{}, 4)
	srv := NewServer(Dependencies{
		Config: mgr, People: auth.NewPeople(db.SQL), Sessions: auth.NewSessions(db.SQL), Invites: auth.NewInvites(db.SQL), Portals: ps,
		Chat: func(w http.ResponseWriter, r *http.Request, _, _, _, message string, _ bool, _ string) error {
			if message == "slow" {
				started <- struct{}{}
				<-release
			}
			writeJSON(w, http.StatusOK, contracts.ChatAnswer{Content: "yes"})
			return nil
		},
	})
	call := func(method, path, body string, c *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.RemoteAddr = "192.168.1.40:5000"
		r.Header.Set("X-Toskar-Portal", "help")
		r.Header.Set("Origin", "http://example.com")
		if c != nil {
			r.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, r)
		return rec
	}
	enter := call(http.MethodPost, "/api/v1/portals/help/enter", `{}`, nil)
	cookie := enter.Result().Cookies()[0]
	code := func(rec *httptest.ResponseRecorder) string {
		var b struct {
			Error struct{ Code string } `json:"error"`
		}
		_ = json.NewDecoder(rec.Body).Decode(&b)
		return b.Error.Code
	}

	if rec := call(http.MethodPost, "/api/v1/chat", `{"message":"`+strings.Repeat("x", 101)+`"}`, cookie); rec.Code != http.StatusBadRequest || code(rec) != "PORTAL_TOO_LONG" {
		t.Fatalf("too long: %d", rec.Code)
	}
	// One chat at a time: a second waits its turn with PORTAL_BUSY.
	go call(http.MethodPost, "/api/v1/chat", `{"message":"slow"}`, cookie)
	<-started
	if rec := call(http.MethodPost, "/api/v1/chat", `{"message":"hi"}`, cookie); rec.Code != http.StatusTooManyRequests || code(rec) != "PORTAL_BUSY" {
		t.Fatalf("busy: %d", rec.Code)
	}
	close(release)
	time.Sleep(50 * time.Millisecond)
	// Two an hour: "slow" was one, this is two, the next is refused.
	if rec := call(http.MethodPost, "/api/v1/chat", `{"message":"hi"}`, cookie); rec.Code != http.StatusOK {
		t.Fatalf("second message: %d %s", rec.Code, rec.Body)
	}
	if rec := call(http.MethodPost, "/api/v1/chat", `{"message":"hi"}`, cookie); rec.Code != http.StatusTooManyRequests || code(rec) != "PORTAL_RATE" {
		t.Fatalf("third message: %d", rec.Code)
	}
}

// A Members only portal answers its signed-in Members as themselves, and
// an invited one the visitors an Admin invited, each by a one-time link
// that can't set a password (#205, #206 item 6).
func TestPortalMembersAndInvited(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.SQL.Exec(`INSERT INTO people (id, name, role) VALUES ('sam', 'Sam', 'member'), ('vee', 'Vee', 'visitor')`); err != nil {
		t.Fatal(err)
	}
	mgr, err := config.NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Update(func(c *config.Config) { c.APIHost = "0.0.0.0" }); err != nil {
		t.Fatal(err)
	}
	ps := portals.NewStore(db.SQL)
	ctx := context.Background()
	mk := func(slug, access string) portals.Portal {
		name := slug
		p, err := ps.Create(ctx, portals.Input{Slug: &slug, Name: &name, Access: &access})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	team, club := mk("team", portals.AccessMembers), mk("club", portals.AccessInvited)
	var chats []string
	sessions := auth.NewSessions(db.SQL)
	srv := NewServer(Dependencies{
		Config: mgr, People: auth.NewPeople(db.SQL), Sessions: sessions, Invites: auth.NewInvites(db.SQL), Portals: ps,
		Chat: func(w http.ResponseWriter, r *http.Request, _, _, _, _ string, _ bool, _ string) error {
			if o := turnopts.From(r.Context()); o != nil {
				chats = append(chats, auth.PersonID(r.Context())+"@"+o.Portal)
			}
			writeJSON(w, http.StatusOK, contracts.ChatAnswer{Content: "yes"})
			return nil
		},
	})
	signedIn := func(id string) *http.Cookie {
		token, _, err := sessions.Create(ctx, id, "test", "192.168.1.40")
		if err != nil {
			t.Fatal(err)
		}
		return &http.Cookie{Name: auth.SessionCookie, Value: token}
	}
	call := func(method, path, slug, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.RemoteAddr = "192.168.1.40:5000"
		r.Header.Set("Origin", "http://example.com")
		if slug != "" {
			r.Header.Set("X-Toskar-Portal", slug)
		}
		for _, c := range cookies {
			r.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, r)
		return rec
	}

	// Members only: Sam chats as Sam, with the portal's settings; Vee and
	// someone signed out don't.
	sam, vee := signedIn("sam"), signedIn("vee")
	if rec := call(http.MethodGet, "/api/v1/portals/team/page", "team", "", sam); !strings.Contains(rec.Body.String(), `"entered":true`) {
		t.Fatalf("sam's page: %s", rec.Body)
	}
	if rec := call(http.MethodPost, "/api/v1/chat", "team", `{"message":"hi"}`, sam); rec.Code != http.StatusOK || len(chats) != 1 || chats[0] != "sam@"+team.ID {
		t.Fatalf("sam's chat: %d %v", rec.Code, chats)
	}
	if rec := call(http.MethodPost, "/api/v1/chat", "team", `{"message":"hi"}`, vee); rec.Code != http.StatusForbidden {
		t.Fatalf("vee's chat: %d", rec.Code)
	}
	if rec := call(http.MethodPost, "/api/v1/portals/team/enter", "team", `{}`); rec.Code != http.StatusForbidden && rec.Code != http.StatusUnauthorized {
		t.Fatalf("signed out: %d", rec.Code)
	}

	// Invited: the Admin (this computer's Owner here) invites Robin.
	owner := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.RemoteAddr = "127.0.0.1:5000"
		r.Host = "127.0.0.1:7331"
		r.Header.Set("Origin", "http://127.0.0.1:7331")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, r)
		return rec
	}
	invited := owner(http.MethodPost, "/api/v1/portals/"+club.ID+"/visitors", `{"name":"Robin"}`)
	var made struct {
		Person auth.Person `json:"person"`
		Link   struct {
			Path string `json:"path"`
		} `json:"link"`
	}
	_ = json.NewDecoder(invited.Body).Decode(&made)
	if invited.Code != http.StatusCreated || !strings.HasPrefix(made.Link.Path, "/p/club?invite=") || made.Person.Name != "Robin" {
		t.Fatalf("invite: %d %+v", invited.Code, made)
	}
	token := strings.TrimPrefix(made.Link.Path, "/p/club?invite=")

	// The link can't be spent to choose a username and password.
	if rec := call(http.MethodGet, "/api/v1/invites/"+token, "", ""); rec.Code == http.StatusOK {
		t.Fatalf("a portal invitation read as a sign-in link: %s", rec.Body)
	}
	if rec := call(http.MethodPost, "/api/v1/portals/club/enter", "club", `{}`); rec.Code != http.StatusForbidden {
		t.Fatalf("without the invitation: %d", rec.Code)
	}
	enter := call(http.MethodPost, "/api/v1/portals/club/enter", "club", `{"invite":"`+token+`"}`)
	if enter.Code != http.StatusOK {
		t.Fatalf("with the invitation: %d %s", enter.Code, enter.Body)
	}
	robin := enter.Result().Cookies()[0]
	if rec := call(http.MethodPost, "/api/v1/chat", "club", `{"message":"hi"}`, robin); rec.Code != http.StatusOK || chats[len(chats)-1] != made.Person.ID+"@"+club.ID {
		t.Fatalf("robin's chat: %d %v", rec.Code, chats)
	}
	if rec := call(http.MethodPost, "/api/v1/portals/club/enter", "club", `{"invite":"`+token+`"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("the invitation twice: %d", rec.Code)
	}
	if list := owner(http.MethodGet, "/api/v1/portals/"+club.ID+"/visitors", ""); !strings.Contains(list.Body.String(), "Robin") {
		t.Fatalf("visitors: %s", list.Body)
	}
	if rec := owner(http.MethodDelete, "/api/v1/portals/"+club.ID+"/visitors/"+made.Person.ID, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("remove: %d", rec.Code)
	}
	if rec := call(http.MethodPost, "/api/v1/chat", "club", `{"message":"still here?"}`, robin); rec.Code == http.StatusOK {
		t.Fatalf("a removed visitor chatted: %d", rec.Code)
	}
}

// Only Toskar frames its own pages, except a portal's in the websites its
// Admin lists; a portal in such a frame keeps its session in a header,
// since browsers hold back its cookie there (#205).
func TestPortalEmbedding(t *testing.T) {
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
	ps := portals.NewStore(db.SQL)
	slug, name, access := "shop", "Shop", portals.AccessOpen
	origins := []string{"https://Shop.Example.com/", "https://shop.example.com"}
	p, err := ps.Create(context.Background(), portals.Input{Slug: &slug, Name: &name, Access: &access, EmbedOrigins: &origins})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.EmbedOrigins) != 1 || p.EmbedOrigins[0] != "https://shop.example.com" {
		t.Fatalf("origins %v", p.EmbedOrigins)
	}
	bad := []string{"https://shop.example.com/page"}
	if _, err := ps.Update(context.Background(), p.ID, portals.Input{EmbedOrigins: &bad}); !errors.Is(err, portals.ErrBadOrigins) {
		t.Fatalf("an origin with a path: %v", err)
	}
	web := fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>Toskar</title>")}}
	srv := NewServer(Dependencies{Config: mgr, People: auth.NewPeople(db.SQL), Sessions: auth.NewSessions(db.SQL), Invites: auth.NewInvites(db.SQL), Portals: ps, WebRoot: web})
	get := func(path string, headers map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.RemoteAddr = "192.168.1.40:5000"
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, r)
		return rec
	}
	if csp := get("/chat", nil).Header().Get("Content-Security-Policy"); csp != "frame-ancestors 'self'" {
		t.Fatalf("the app's frame policy: %q", csp)
	}
	if csp := get("/p/shop", nil).Header().Get("Content-Security-Policy"); csp != "frame-ancestors 'self' https://shop.example.com" {
		t.Fatalf("the portal's frame policy: %q", csp)
	}
	if csp := get("/p/other", nil).Header().Get("Content-Security-Policy"); csp != "frame-ancestors 'self'" {
		t.Fatalf("an unknown portal's frame policy: %q", csp)
	}
	if script := get("/embed.js", nil); !strings.Contains(script.Body.String(), "data-portal") || !strings.HasPrefix(script.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("embed.js: %s", script.Header().Get("Content-Type"))
	}

	r := httptest.NewRequest(http.MethodPost, "/api/v1/portals/shop/enter", strings.NewReader(`{"embed":true}`))
	r.RemoteAddr = "192.168.1.40:5000"
	r.Header.Set("X-Toskar-Portal", "shop")
	r.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, r)
	var entered struct {
		Person  auth.Person `json:"person"`
		Session string      `json:"session"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&entered)
	if rec.Code != http.StatusOK || entered.Session == "" || entered.Person.PortalID != p.ID {
		t.Fatalf("enter for a frame: %d %+v", rec.Code, entered)
	}
	me := get("/api/v1/me", map[string]string{"X-Toskar-Portal": "shop", "X-Toskar-Portal-Session": entered.Session})
	if !strings.Contains(me.Body.String(), `"portal_id":"`+p.ID+`"`) {
		t.Fatalf("the session by header: %d %s", me.Code, me.Body)
	}
}
