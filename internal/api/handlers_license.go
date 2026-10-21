package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/license"
)

// License is what the Toskar Pro desktop app says about its purchase
// (yeixio/toskar-apps#336).
type License interface {
	Get() license.State
	Set(license.State) (license.State, error)
}

// shellHeader carries the secret the desktop app started Core with.
const shellHeader = "X-Toskar-Shell"

// BindLicense attaches the license routes. shellSecret is what the desktop
// app passed in TOSKAR_SHELL_SECRET; without one, nothing can set it.
func (s *Server) BindLicense(l License, shellSecret string) {
	s.license = l
	s.shellSecret = shellSecret
}

func (s *Server) licenseRoutes(api *mux.Router) {
	api.HandleFunc("/license", s.handleGetLicense).Methods(http.MethodGet)
	api.HandleFunc("/license", s.handlePutLicense).Methods(http.MethodPut)
}

type licenseView struct {
	license.State
	// Limited: new setup waits for the Unlock.
	Limited bool `json:"limited"`
	// DaysLeft in a trial, rounded up.
	DaysLeft int `json:"days_left,omitempty"`
}

func viewLicense(st license.State) licenseView {
	v := licenseView{State: st, Limited: st.Limited()}
	if st.Kind == license.Trial {
		v.DaysLeft = int(math.Ceil(time.Until(st.TrialEnds).Hours() / 24))
	}
	return v
}

func (s *Server) licenseState() license.State {
	if s.license == nil {
		return license.State{}
	}
	return s.license.Get()
}

func (s *Server) handleGetLicense(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, viewLicense(s.licenseState()))
}

// handlePutLicense keeps what the desktop app says. Only it may: from this
// computer, with the secret it started Core with.
func (s *Server) handlePutLicense(w http.ResponseWriter, r *http.Request) {
	given := r.Header.Get(shellHeader)
	if s.license == nil || s.shellSecret == "" || !auth.FromThisComputer(r) ||
		subtle.ConstantTimeCompare([]byte(given), []byte(s.shellSecret)) != 1 {
		writeErr(w, http.StatusForbidden, "SHELL_ONLY", "only the Toskar Pro app on this computer sets its license", nil)
		return
	}
	var st license.State
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&st); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "the license isn't valid JSON", nil)
		return
	}
	kept, err := s.license.Set(st)
	switch {
	case errors.Is(err, license.ErrInvalid):
		writeErrFrom(w, http.StatusBadRequest, "BAD_REQUEST", err)
		return
	case err != nil:
		writeErrFrom(w, http.StatusInternalServerError, "INTERNAL_ERROR", err)
		return
	}
	writeJSON(w, http.StatusOK, viewLicense(kept))
}

// refuseLocked says UNLOCK_REQUIRED when Toskar Pro's trial has ended
// without the Unlock, for new setup: installing models, creating
// automations, adding people. What's set up keeps working.
func (s *Server) refuseLocked(w http.ResponseWriter) bool {
	st := s.licenseState()
	if !st.Limited() {
		return false
	}
	if st.Kind == license.Available {
		writeErr(w, http.StatusForbidden, "TRIAL_NOT_STARTED", "Toskar Pro's free trial hasn't been started", map[string]any{"store": st.Store})
		return true
	}
	writeErr(w, http.StatusForbidden, "UNLOCK_REQUIRED", "Toskar Pro's trial has ended; this needs the Unlock", map[string]any{"store": st.Store})
	return true
}
