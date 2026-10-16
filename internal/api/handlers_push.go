package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/gjallarhorn"
	"github.com/yeixio/toskar-core/internal/push"
)

// Push to the Toskar apps (#460, docs/push.md). A paired phone or tablet
// registers the handle the push relay gave it, for this computer's key,
// and an app destination then reaches it.

// BindPush serves the push routes with the registrations and this
// computer's push client.
func (s *Server) BindPush(store *push.Store, client *push.Client) {
	s.push, s.pushClient = store, client
}

func (s *Server) pushRoutes(api *mux.Router) {
	api.HandleFunc("/me/push", s.handlePushStatus).Methods(http.MethodGet)
	api.HandleFunc("/me/push", s.handleRegisterPush).Methods(http.MethodPut)
	api.HandleFunc("/me/push", s.handleUnregisterPush).Methods(http.MethodDelete)
}

// PushStatus is what a device needs to register with the relay, and
// whether it has.
type PushStatus struct {
	// Relay is the push relay to register with.
	Relay string `json:"relay"`
	// ComputerKey is this computer's key (Ed25519, base64), which the relay
	// seals into the handle so only this computer can push to it.
	ComputerKey string `json:"computer_key"`
	Registered  bool   `json:"registered"`
	Platform    string `json:"platform,omitempty"`
}

// pushDevice is the request's paired device's key, or "" for a request that
// isn't from one: only a paired phone or tablet takes pushes.
func (s *Server) pushDevice(r *http.Request) string {
	p := auth.PrincipalFrom(r.Context())
	if p.Via != auth.ViaAPIKey || p.KeyID == "" || s.deps.ListAPIKeys == nil {
		return ""
	}
	keys, err := s.deps.ListAPIKeys(r.Context())
	if err != nil {
		return ""
	}
	for _, k := range keys {
		if k.ID == p.KeyID && k.Kind == auth.KindDevice && !k.Revoked {
			return k.ID
		}
	}
	return ""
}

func (s *Server) pushReady(w http.ResponseWriter) bool {
	if s.push == nil || s.pushClient == nil {
		writeErr(w, http.StatusNotImplemented, "PUSH_UNAVAILABLE", "push to the Toskar apps isn't available on this computer", nil)
		return false
	}
	return true
}

func (s *Server) handlePushStatus(w http.ResponseWriter, r *http.Request) {
	if !s.pushReady(w) {
		return
	}
	out := PushStatus{Relay: s.pushClient.RelayURL(), ComputerKey: s.pushClient.PublicKey()}
	if key := s.pushDevice(r); key != "" {
		if reg, ok, err := s.push.Get(r.Context(), key); err == nil && ok {
			out.Registered, out.Platform = true, reg.Platform
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleRegisterPush(w http.ResponseWriter, r *http.Request) {
	if !s.pushReady(w) {
		return
	}
	key := s.pushDevice(r)
	if key == "" {
		writeErr(w, http.StatusForbidden, "PUSH_DEVICE_ONLY", "only a paired phone or tablet can turn on notifications", nil)
		return
	}
	var body struct {
		Handle   string `json:"handle"`
		Platform string `json:"platform"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid JSON body", nil)
		return
	}
	err := s.push.Set(r.Context(), push.Registration{KeyID: key, PersonID: auth.PersonID(r.Context()), Handle: body.Handle, Platform: body.Platform})
	if err != nil {
		writeErrFrom(w, http.StatusBadRequest, "PUSH_INVALID", err)
		return
	}
	s.ensureAppDestination(r)
	s.handlePushStatus(w, r)
}

// ensureAppDestination adds the Toskar app as a notification destination
// when a device turns notifications on and there's none yet, so turning
// them on in the app is all it takes. Its settings, title only by default,
// are changed in Settings › Notifications.
func (s *Server) ensureAppDestination(r *http.Request) {
	if s.notifications == nil {
		return
	}
	list, err := s.notifications.Destinations(r.Context())
	if err != nil {
		return
	}
	for _, d := range list {
		if d.Kind == gjallarhorn.KindApp {
			return
		}
	}
	name := "Toskar app"
	_, _, _ = s.notifications.CreateDestination(r.Context(), gjallarhorn.DestinationInput{Kind: gjallarhorn.KindApp, Name: &name})
}

func (s *Server) handleUnregisterPush(w http.ResponseWriter, r *http.Request) {
	if !s.pushReady(w) {
		return
	}
	if key := s.pushDevice(r); key != "" {
		if err := s.push.Remove(r.Context(), key); err != nil {
			writeErrFrom(w, http.StatusInternalServerError, "PUSH_FAILED", err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
