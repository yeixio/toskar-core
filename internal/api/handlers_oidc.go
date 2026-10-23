package api

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/auth"
)

// Sign-in with an OpenID Connect provider (#206). Start sends the browser
// to the provider, with the sign-in's state in a short-lived cookie as
// well, so a callback link made for someone else's browser signs nobody
// in. The callback signs the browser in with an ordinary session.

// oidcCookie holds the state of the sign-in this browser started.
const oidcCookie = "toskar_oidc"

// oidcHolder keeps one provider client while its settings stay the same,
// since it holds the sign-ins under way.
type oidcHolder struct {
	mu  sync.Mutex
	key string
	o   *auth.OIDC
}

func (s *Server) oidcRoutes(api *mux.Router) {
	api.HandleFunc("/oidc", s.handleOIDCInfo).Methods(http.MethodGet)
	api.HandleFunc("/oidc/start", s.handleOIDCStart).Methods(http.MethodGet)
	api.HandleFunc("/oidc/callback", s.handleOIDCCallback).Methods(http.MethodGet)
}

// oidc is the provider's client, or nil when none is set.
func (s *Server) oidc() (*auth.OIDC, error) {
	if s.deps.Config == nil {
		return nil, nil
	}
	settings, err := s.deps.Config.Get().OIDCSettings()
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("%#v", settings)
	s.oidcState.mu.Lock()
	defer s.oidcState.mu.Unlock()
	if s.oidcState.key == key && s.oidcState.o != nil {
		return s.oidcState.o, nil
	}
	o, err := auth.NewOIDC(settings)
	if err != nil {
		return nil, err
	}
	s.oidcState.key, s.oidcState.o = key, o
	return o, nil
}

// handleOIDCInfo says whether the sign-in screen offers the provider, and
// its name.
func (s *Server) handleOIDCInfo(w http.ResponseWriter, _ *http.Request) {
	o, err := s.oidc()
	if err != nil || o == nil {
		writeJSON(w, http.StatusOK, oidcInfo{})
		return
	}
	writeJSON(w, http.StatusOK, oidcInfo{Enabled: true, Label: o.Label()})
}

// oidcInfo says whether the sign-in screen offers an OpenID Connect
// provider, and the name to show on its button.
type oidcInfo struct {
	Enabled bool   `json:"enabled"`
	Label   string `json:"label,omitempty"`
}

// localPath is return when it's a path in Toskar, else "/".
func localPath(ret string) string {
	if !strings.HasPrefix(ret, "/") || strings.HasPrefix(ret, "//") || strings.HasPrefix(ret, "/\\") || strings.ContainsAny(ret, "\r\n") {
		return "/"
	}
	return ret
}

// publicBase is the address the browser reached Toskar at, such as
// https://toskar.example.com, as a trusted proxy says when there is one.
func (s *Server) publicBase(r *http.Request) string {
	scheme, host := "http", r.Host
	if r.TLS != nil {
		scheme = "https"
	}
	if s.fromTrustedProxy(r) {
		if p := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); p == "https" || p == "http" {
			scheme = p
		}
		if h := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0]); h != "" {
			host = h
		}
	}
	return scheme + "://" + host
}

// oidcFailed sends the browser back to the sign-in screen, which says why.
func (s *Server) oidcFailed(w http.ResponseWriter, r *http.Request, reason string, err error) {
	if err != nil {
		s.deps.Logger.Warn("sign-in with the provider failed", "reason", reason, "error", err)
	}
	http.Redirect(w, r, "/?oidc_error="+url.QueryEscape(reason), http.StatusSeeOther)
}

func (s *Server) handleOIDCStart(w http.ResponseWriter, r *http.Request) {
	o, err := s.oidc()
	if err != nil || o == nil {
		s.oidcFailed(w, r, "unavailable", err)
		return
	}
	to, err := o.Start(r.Context(), o.RedirectURL(s.publicBase(r)), localPath(r.URL.Query().Get("return")))
	if err != nil {
		s.oidcFailed(w, r, "failed", err)
		return
	}
	state, _ := url.Parse(to)
	http.SetCookie(w, &http.Cookie{
		Name: oidcCookie, Value: state.Query().Get("state"), Path: "/api/v1/oidc", MaxAge: 600,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil,
	})
	http.Redirect(w, r, to, http.StatusFound)
}

func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: oidcCookie, Value: "", Path: "/api/v1/oidc", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil})
	o, err := s.oidc()
	if err != nil || o == nil || !s.signInReady(w) {
		s.oidcFailed(w, r, "unavailable", err)
		return
	}
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		s.oidcFailed(w, r, "failed", fmt.Errorf("the provider said %s: %s", e, q.Get("error_description")))
		return
	}
	state := q.Get("state")
	cookie, err := r.Cookie(oidcCookie)
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) != 1 {
		s.oidcFailed(w, r, "failed", errors.New("the sign-in was started in another browser"))
		return
	}
	id, returnTo, err := o.Finish(r.Context(), state, q.Get("code"))
	if err != nil {
		s.oidcFailed(w, r, "failed", err)
		return
	}
	person, err := s.deps.People.SignedInWith(r.Context(), o, id)
	switch {
	case errors.Is(err, auth.ErrOIDCRefused):
		s.oidcFailed(w, r, "refused", nil)
		return
	case errors.Is(err, auth.ErrNoPerson):
		s.oidcFailed(w, r, "disabled", nil)
		return
	case err != nil:
		s.oidcFailed(w, r, "failed", err)
		return
	}
	if err := s.setSession(w, r, person); err != nil {
		s.oidcFailed(w, r, "failed", err)
		return
	}
	http.Redirect(w, r, localPath(returnTo), http.StatusSeeOther)
}
