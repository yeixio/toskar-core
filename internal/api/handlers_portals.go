package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/portals"
)

// Chat portals (#205). Admins manage portals; a portal's page shows its
// branding to anyone, and a visitor enters (with the passcode, when it has
// one) to become a guest: a person who belongs to the portal, signed in by
// a cookie of the portal's own. A request carries its portal in the
// X-Toskar-Portal header, so the guest cookie never reaches the main app,
// and a guest reaches only the portal's chat.

// portalHeader names the portal a portal page's request is for.
const portalHeader = "X-Toskar-Portal"

// portalSessionHeader carries a portal guest's session when the portal is
// shown in another website's frame, where browsers hold back cookies.
const portalSessionHeader = "X-Toskar-Portal-Session"

// portalCookie holds a portal guest's session.
func portalCookie(slug string) string { return "toskar_portal_" + slug }

// Wrong passcodes: ten per address in ten minutes, then it waits.
var portalByAddress = auth.NewLimiter(10, 10*time.Minute)

// guestRoutes are all a portal's guest reaches: its chats and their files,
// the live events about them, a tool's question, and leaving.
var guestRoutes = map[string]bool{
	"GET /api/v1/health":                                  true,
	"GET /api/v1/me":                                      true,
	"DELETE /api/v1/session":                              true,
	"GET /api/v1/events":                                  true,
	"POST /api/v1/chat":                                   true,
	"POST /api/v1/chat/stop":                              true,
	"POST /api/v1/tools/decide":                           true,
	"GET /api/v1/conversations":                           true,
	"POST /api/v1/conversations":                          true,
	"PATCH /api/v1/conversations/{id}":                    true,
	"DELETE /api/v1/conversations/{id}":                   true,
	"POST /api/v1/conversations/delete":                   true,
	"GET /api/v1/conversations/{id}/messages":             true,
	"PUT /api/v1/conversations/{id}/messages/{mid}/shown": true,
	"GET /api/v1/conversations/{id}/artifacts":            true,
	"GET /api/v1/artifacts/{id}":                          true,
	"GET /api/v1/artifacts/{id}/content":                  true,
	"GET /api/v1/portals/{slug}/page":                     true,
	"POST /api/v1/portals/{slug}/enter":                   true,
}

func (s *Server) portalRoutes(api *mux.Router) {
	api.HandleFunc("/portals", s.portalsReady(s.handleListPortals)).Methods(http.MethodGet)
	api.HandleFunc("/portals", s.portalsReady(s.handleCreatePortal)).Methods(http.MethodPost)
	api.HandleFunc("/portals/{id}", s.portalsReady(s.handleGetPortal)).Methods(http.MethodGet)
	api.HandleFunc("/portals/{id}", s.portalsReady(s.handleUpdatePortal)).Methods(http.MethodPatch)
	api.HandleFunc("/portals/{id}", s.portalsReady(s.handleDeletePortal)).Methods(http.MethodDelete)
	api.HandleFunc("/portals/{id}/conversations", s.portalsReady(s.handlePortalConversations)).Methods(http.MethodGet)
	api.HandleFunc("/portals/{id}/conversations/{cid}/messages", s.portalsReady(s.handlePortalMessages)).Methods(http.MethodGet)
	api.HandleFunc("/portals/{id}/usage", s.portalsReady(s.handlePortalUsage)).Methods(http.MethodGet)
	api.HandleFunc("/portals/{id}/visitors", s.portalsReady(s.handleListVisitors)).Methods(http.MethodGet)
	api.HandleFunc("/portals/{id}/visitors", s.portalsReady(s.handleInviteVisitor)).Methods(http.MethodPost)
	api.HandleFunc("/portals/{id}/visitors/{vid}/link", s.portalsReady(s.handleVisitorLink)).Methods(http.MethodPost)
	api.HandleFunc("/portals/{id}/visitors/{vid}", s.portalsReady(s.handleRemoveVisitor)).Methods(http.MethodDelete)
	api.HandleFunc("/portals/{slug}/page", s.portalsReady(s.handlePortalPage)).Methods(http.MethodGet)
	api.HandleFunc("/portals/{slug}/enter", s.portalsReady(s.handleEnterPortal)).Methods(http.MethodPost)
}

func (s *Server) portalsReady(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.deps.Portals == nil {
			writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Portals aren't available.", nil)
			return
		}
		h(w, r)
	}
}

func portalError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, portals.ErrNotFound):
		writeErr(w, http.StatusNotFound, "PORTAL_NOT_FOUND", err.Error(), nil)
	case errors.Is(err, portals.ErrSlugTaken):
		writeErr(w, http.StatusConflict, "PORTAL_SLUG_TAKEN", err.Error(), nil)
	case errors.Is(err, portals.ErrBadSlug), errors.Is(err, portals.ErrBadName), errors.Is(err, portals.ErrBadTools),
		errors.Is(err, portals.ErrBadAccess), errors.Is(err, portals.ErrNoPasscode), errors.Is(err, portals.ErrBadBranding),
		errors.Is(err, portals.ErrBadLanguage), errors.Is(err, portals.ErrBadLimits), errors.Is(err, portals.ErrBadOrigins), errors.Is(err, portals.ErrBadRetention):
		writeErr(w, http.StatusBadRequest, "PORTAL_INVALID", err.Error(), nil)
	default:
		writeErr(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
	}
}

func (s *Server) handleListPortals(w http.ResponseWriter, r *http.Request) {
	list, err := s.deps.Portals.List(r.Context())
	if err != nil {
		portalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func decodePortal(w http.ResponseWriter, r *http.Request) (portals.Input, bool) {
	var in portals.Input
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return in, false
	}
	return in, true
}

func (s *Server) handleCreatePortal(w http.ResponseWriter, r *http.Request) {
	in, ok := decodePortal(w, r)
	if !ok {
		return
	}
	p, err := s.deps.Portals.Create(r.Context(), in)
	if err != nil {
		portalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleGetPortal(w http.ResponseWriter, r *http.Request) {
	p, err := s.deps.Portals.Get(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		portalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleUpdatePortal(w http.ResponseWriter, r *http.Request) {
	in, ok := decodePortal(w, r)
	if !ok {
		return
	}
	p, err := s.deps.Portals.Update(r.Context(), mux.Vars(r)["id"], in)
	if err != nil {
		portalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeletePortal(w http.ResponseWriter, r *http.Request) {
	if err := s.deps.Portals.Delete(r.Context(), mux.Vars(r)["id"]); err != nil {
		portalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// portalPage is what a portal's page shows before anyone enters.
type portalPage struct {
	Slug     string          `json:"slug"`
	Name     string          `json:"name"`
	Access   string          `json:"access"`
	Language string          `json:"language"`
	Branding json.RawMessage `json:"branding"`
	// MaxMessage is the longest message the portal takes.
	MaxMessage int `json:"max_message"`
	// Entered is true when this browser is already the portal's guest.
	Entered bool `json:"entered"`
}

// livePortal is the enabled portal at slug.
func (s *Server) livePortal(r *http.Request, slug string) (portals.Portal, bool) {
	p, err := s.deps.Portals.BySlug(r.Context(), slug)
	if err != nil || !p.Enabled {
		return portals.Portal{}, false
	}
	return p, true
}

func (s *Server) handlePortalPage(w http.ResponseWriter, r *http.Request) {
	p, ok := s.livePortal(r, mux.Vars(r)["slug"])
	if !ok {
		writeErr(w, http.StatusNotFound, "PORTAL_NOT_FOUND", "There's no portal at this address, or it's turned off.", nil)
		return
	}
	_, entered := s.guestOf(r, p)
	if p.Access == portals.AccessMembers && member(auth.PrincipalFrom(r.Context())) {
		entered = true
	}
	writeJSON(w, http.StatusOK, portalPage{Slug: p.Slug, Name: p.Name, Access: p.Access, Language: p.Language, Branding: p.Branding, MaxMessage: p.MaxMessage, Entered: entered})
}

// handleEnterPortal makes this browser the portal's guest: again the same
// guest when it already is one, else a new one, with the passcode when the
// portal has one.
func (s *Server) handleEnterPortal(w http.ResponseWriter, r *http.Request) {
	if !s.signInReady(w) {
		return
	}
	p, ok := s.livePortal(r, mux.Vars(r)["slug"])
	if !ok {
		writeErr(w, http.StatusNotFound, "PORTAL_NOT_FOUND", "There's no portal at this address, or it's turned off.", nil)
		return
	}
	if guest, ok := s.guestOf(r, p); ok {
		writeJSON(w, http.StatusOK, auth.Principal{Person: guest, Via: auth.ViaSession})
		return
	}
	var body struct {
		Passcode string `json:"passcode"`
		Invite   string `json:"invite"`
		// Embed asks for the session in the answer, for a page in another
		// website's frame to send in a header.
		Embed bool `json:"embed"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body)
	switch p.Access {
	case portals.AccessMembers:
		// Members and up chat as themselves, signed in (#206).
		if principal := auth.PrincipalFrom(r.Context()); member(principal) {
			writeJSON(w, http.StatusOK, principal)
			return
		}
		writeErr(w, http.StatusForbidden, "PORTAL_MEMBERS", "this chat is for the people who sign in to this Toskar", nil)
		return
	case portals.AccessInvited:
		s.enterInvited(w, r, p, strings.TrimSpace(body.Invite), body.Embed)
		return
	case portals.AccessPasscode:
		address := remoteIP(r)
		if portalByAddress.Blocked(address) {
			writeErr(w, http.StatusTooManyRequests, "SIGN_IN_THROTTLED", "too many tries; wait 10 minutes and try again", nil)
			return
		}
		if !p.CheckPasscode(strings.TrimSpace(body.Passcode)) {
			portalByAddress.Fail(address)
			writeErr(w, http.StatusUnauthorized, "PORTAL_PASSCODE", portals.ErrWrongPasscode.Error(), nil)
			return
		}
	}
	guest, err := s.deps.People.CreateGuest(r.Context(), p.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	s.signInGuest(w, r, p, guest, body.Embed)
}

// member reports whether principal may use a Members only portal: a person
// signed in to this Toskar as a Member or up, not another portal's guest.
func member(principal auth.Principal) bool {
	return principal.Via != auth.ViaNone && principal.Person.PortalID == "" && principal.Person.Role.AtLeast(auth.RoleMember)
}

// enterInvited makes this browser the invited visitor whose one-time link
// it brings.
func (s *Server) enterInvited(w http.ResponseWriter, r *http.Request, p portals.Portal, invite string, embed bool) {
	if invite == "" || s.deps.Invites == nil {
		writeErr(w, http.StatusForbidden, "PORTAL_INVITE", "this chat is by invitation; open the link you were sent", nil)
		return
	}
	id, err := s.deps.Invites.UsePortal(r.Context(), invite)
	if err != nil {
		writeErr(w, http.StatusForbidden, "PORTAL_INVITE", "this invitation doesn't work anymore; ask for a new one", nil)
		return
	}
	guest, err := s.deps.People.Active(r.Context(), id)
	if err != nil || guest.PortalID != p.ID {
		writeErr(w, http.StatusForbidden, "PORTAL_INVITE", "this invitation doesn't work anymore; ask for a new one", nil)
		return
	}
	s.signInGuest(w, r, p, guest, embed)
}

// enteredGuest is a portal's guest, with their session when the page asked
// for it to send in a header.
type enteredGuest struct {
	auth.Principal
	Session string `json:"session,omitempty"`
}

// signInGuest signs this browser in as guest, by the portal's own cookie,
// and in the answer too when embed asks.
func (s *Server) signInGuest(w http.ResponseWriter, r *http.Request, p portals.Portal, guest auth.Person, embed bool) {
	token, expires, err := s.deps.Sessions.Create(r.Context(), guest.ID, r.UserAgent(), remoteIP(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: portalCookie(p.Slug), Value: token, Path: "/", Expires: expires,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil,
	})
	answer := enteredGuest{Principal: auth.Principal{Person: guest, Via: auth.ViaSession}}
	if embed {
		answer.Session = token
	}
	writeJSON(w, http.StatusOK, answer)
}

// guestOf is this browser's guest of portal p, while the guest and their
// session are good.
func (s *Server) guestOf(r *http.Request, p portals.Portal) (auth.Person, bool) {
	if s.deps.Sessions == nil || s.deps.People == nil {
		return auth.Person{}, false
	}
	// A portal shown in another website's frame sends its session in a
	// header, since browsers hold back cookies there.
	token := strings.TrimSpace(r.Header.Get(portalSessionHeader))
	if token == "" {
		cookie, err := r.Cookie(portalCookie(p.Slug))
		if err != nil {
			return auth.Person{}, false
		}
		token = cookie.Value
	}
	id, err := s.deps.Sessions.Person(r.Context(), token)
	if err != nil {
		return auth.Person{}, false
	}
	person, err := s.deps.People.Active(r.Context(), id)
	if err != nil || person.PortalID != p.ID {
		return auth.Person{}, false
	}
	return person, true
}

// guestPrincipal is the guest a portal page's request comes from, by its
// X-Toskar-Portal header and that portal's cookie. off is true when the
// portal is unknown or turned off.
func (s *Server) guestPrincipal(r *http.Request) (principal auth.Principal, ok, off bool) {
	slug := strings.ToLower(strings.TrimSpace(r.Header.Get(portalHeader)))
	if slug == "" || s.deps.Portals == nil {
		return auth.Principal{}, false, false
	}
	p, live := s.livePortal(r, slug)
	if !live {
		return auth.Principal{}, false, true
	}
	guest, ok := s.guestOf(r, p)
	if !ok {
		return auth.Principal{}, false, false
	}
	return auth.Principal{Person: guest, Via: auth.ViaSession}, true, false
}

// portalOf is the portal a request chats in: a guest's own, or a Members
// only portal its page names for a signed-in Member (#205). refused is
// true when the page names a Members only portal the person may not use.
func (s *Server) portalOf(r *http.Request) (p portals.Portal, ok, refused bool) {
	if s.deps.Portals == nil {
		return portals.Portal{}, false, false
	}
	principal := auth.PrincipalFrom(r.Context())
	if id := principal.Person.PortalID; id != "" {
		p, err := s.deps.Portals.Get(r.Context(), id)
		if err != nil || !p.Enabled {
			return portals.Portal{}, false, false
		}
		return p, true, false
	}
	slug := strings.ToLower(strings.TrimSpace(r.Header.Get(portalHeader)))
	if slug == "" {
		return portals.Portal{}, false, false
	}
	p, live := s.livePortal(r, slug)
	if !live || p.Access != portals.AccessMembers {
		return portals.Portal{}, false, false
	}
	if !member(principal) {
		return portals.Portal{}, false, true
	}
	return p, true, false
}

// portalLimiter keeps a portal's limits (#205): how many messages each
// visitor sent in the last hour, and how many of each portal's chats are
// running.
type portalLimiter struct {
	mu      sync.Mutex
	sent    map[string][]time.Time
	running map[string]int
	now     func() time.Time
}

// enter admits one of a portal's chats while fewer than most run, and
// returns what ends it.
func (l *portalLimiter) enter(portal string, most int) (func(), bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.running == nil {
		l.running = map[string]int{}
	}
	if l.running[portal] >= most {
		return nil, false
	}
	l.running[portal]++
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.running[portal]--; l.running[portal] <= 0 {
			delete(l.running, portal)
		}
	}, true
}

// allow counts a visitor's message when they've sent fewer than perHour in
// the last hour; 0 is no limit.
func (l *portalLimiter) allow(guest string, perHour int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if l.now != nil {
		now = l.now()
	}
	if l.sent == nil {
		l.sent = map[string][]time.Time{}
	}
	recent := l.sent[guest][:0]
	for _, t := range l.sent[guest] {
		if now.Sub(t) < time.Hour {
			recent = append(recent, t)
		}
	}
	if perHour > 0 && len(recent) >= perHour {
		l.sent[guest] = recent
		return false
	}
	l.sent[guest] = append(recent, now)
	return true
}

// visitorLink is an invited visitor's one-time link: the portal's page
// with the invitation, good for a week.
type visitorLink struct {
	Path      string    `json:"path"`
	ExpiresAt time.Time `json:"expires_at"`
}

// portalVisitor is the portal, and one of its invited visitors when vid
// names one.
func (s *Server) portalVisitor(w http.ResponseWriter, r *http.Request) (portals.Portal, auth.Person, bool) {
	p, err := s.deps.Portals.Get(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		portalError(w, err)
		return portals.Portal{}, auth.Person{}, false
	}
	vid := mux.Vars(r)["vid"]
	if vid == "" {
		return p, auth.Person{}, true
	}
	guest, err := s.deps.People.Get(r.Context(), vid)
	if err != nil || guest.PortalID != p.ID {
		writeErr(w, http.StatusNotFound, "PERSON_NOT_FOUND", "no such visitor", nil)
		return portals.Portal{}, auth.Person{}, false
	}
	return p, guest, true
}

// handleListVisitors lists a portal's invited visitors (#205).
func (s *Server) handleListVisitors(w http.ResponseWriter, r *http.Request) {
	p, _, ok := s.portalVisitor(w, r)
	if !ok {
		return
	}
	list, err := s.deps.People.Guests(r.Context(), p.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// inviteLink makes a new one-time link for guest.
func (s *Server) inviteLink(r *http.Request, p portals.Portal, guest auth.Person) (visitorLink, error) {
	token, expires, err := s.deps.Invites.Create(r.Context(), guest.ID, auth.InvitePortal, auth.PersonID(r.Context()))
	if err != nil {
		return visitorLink{}, err
	}
	return visitorLink{Path: "/p/" + p.Slug + "?invite=" + token, ExpiresAt: expires}, nil
}

// handleInviteVisitor invites someone by name and answers with their link.
func (s *Server) handleInviteVisitor(w http.ResponseWriter, r *http.Request) {
	p, _, ok := s.portalVisitor(w, r)
	if !ok || !s.signInReady(w) {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return
	}
	guest, err := s.deps.People.CreateInvitedGuest(r.Context(), p.ID, body.Name)
	if err != nil {
		if errors.Is(err, auth.ErrBadName) {
			writeErr(w, http.StatusBadRequest, "NAME_INVALID", err.Error(), nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	link, err := s.inviteLink(r, p, guest)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusCreated, invitedVisitor{Person: guest, Link: link})
}

// handleVisitorLink makes an invited visitor a new link, which replaces
// any they haven't used.
func (s *Server) handleVisitorLink(w http.ResponseWriter, r *http.Request) {
	p, guest, ok := s.portalVisitor(w, r)
	if !ok || !s.signInReady(w) {
		return
	}
	link, err := s.inviteLink(r, p, guest)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, link)
}

// handleRemoveVisitor stops an invited visitor at once: they're disabled
// and signed out.
func (s *Server) handleRemoveVisitor(w http.ResponseWriter, r *http.Request) {
	_, guest, ok := s.portalVisitor(w, r)
	if !ok || !s.signInReady(w) {
		return
	}
	disabled := true
	if _, err := s.deps.People.Update(r.Context(), guest.ID, auth.Change{Disabled: &disabled}); err != nil {
		writeErr(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	_ = s.deps.Sessions.DeleteFor(r.Context(), guest.ID)
	w.WriteHeader(http.StatusNoContent)
}

// framing lets only Toskar's own pages show its pages in a frame, so no
// other website can dress them up to be clicked, except a portal's page in
// the websites its Admin lists (#205).
func (s *Server) framing(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ancestors := "'self'"
		if slug, ok := strings.CutPrefix(r.URL.Path, "/p/"); ok && s.deps.Portals != nil {
			if p, live := s.livePortal(r, strings.Trim(slug, "/")); live {
				for _, o := range p.EmbedOrigins {
					ancestors += " " + o
				}
			}
		}
		w.Header().Set("Content-Security-Policy", "frame-ancestors "+ancestors)
		next.ServeHTTP(w, r)
	})
}

// handleEmbedScript is a script a website adds to show a portal's chat
// behind a button: <script src="…/embed.js" data-portal="slug"></script>.
func (s *Server) handleEmbedScript(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "max-age=300")
	_, _ = io.WriteString(w, embedScript)
}

// embedScript adds a chat button to the page that loads it, opening the
// portal named by its data-portal attribute in a frame from Toskar's own
// address, which only the websites the portal lists may show.
const embedScript = `(function () {
  var me = document.currentScript;
  if (!me) return;
  var slug = (me.getAttribute('data-portal') || '').toLowerCase();
  if (!/^[a-z0-9-]{2,40}$/.test(slug)) return;
  var base = new URL(me.src).origin;
  var label = me.getAttribute('data-label') || 'Chat';
  var color = /^#[0-9a-f]{3,6}$/i.test(me.getAttribute('data-color') || '') ? me.getAttribute('data-color') : '#0f766e';
  var button = document.createElement('button');
  button.type = 'button';
  button.textContent = label;
  button.setAttribute('aria-expanded', 'false');
  button.style.cssText = 'position:fixed;right:20px;bottom:20px;z-index:2147483646;border:0;border-radius:999px;padding:12px 20px;font:600 15px system-ui,sans-serif;color:#fff;background:' + color + ';box-shadow:0 4px 16px rgba(0,0,0,.25);cursor:pointer';
  var frame = null;
  button.addEventListener('click', function () {
    if (!frame) {
      frame = document.createElement('iframe');
      frame.src = base + '/p/' + slug;
      frame.title = label;
      frame.style.cssText = 'position:fixed;right:20px;bottom:80px;z-index:2147483646;width:min(380px,calc(100vw - 40px));height:min(600px,calc(100vh - 120px));border:0;border-radius:12px;box-shadow:0 8px 32px rgba(0,0,0,.3);background:#fff';
      document.body.appendChild(frame);
    } else {
      frame.style.display = frame.style.display === 'none' ? '' : 'none';
    }
    button.setAttribute('aria-expanded', frame.style.display === 'none' ? 'false' : 'true');
  });
  document.body.appendChild(button);
})();
`

// handlePortalConversations lists a portal's visitors' conversations for
// Admins (#205); visitors are told the people who run it can read them.
func (s *Server) handlePortalConversations(w http.ResponseWriter, r *http.Request) {
	list, err := s.deps.Portals.Conversations(r.Context(), mux.Vars(r)["id"], 200)
	if err != nil {
		portalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handlePortalMessages is one visitor's conversation, to read.
func (s *Server) handlePortalMessages(w http.ResponseWriter, r *http.Request) {
	list, err := s.deps.Portals.Messages(r.Context(), mux.Vars(r)["id"], mux.Vars(r)["cid"])
	if err != nil {
		portalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handlePortalUsage counts a portal's use over the last 7 and 30 days.
func (s *Server) handlePortalUsage(w http.ResponseWriter, r *http.Request) {
	if _, err := s.deps.Portals.Get(r.Context(), mux.Vars(r)["id"]); err != nil {
		portalError(w, err)
		return
	}
	u, err := s.deps.Portals.Usage(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		portalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// invitedVisitor is a portal's new visitor and their link.
type invitedVisitor struct {
	Person auth.Person `json:"person"`
	Link   visitorLink `json:"link"`
}
