package api

import (
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/features"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// One-step setup (#1355): each feature and what it still needs here, and
// one call that sets all of it up. Admins only, since it installs.

func (s *Server) featureRoutes(api *mux.Router) {
	api.HandleFunc("/features", s.atLeast(auth.RoleAdmin, s.handleFeatures)).Methods(http.MethodGet)
	api.HandleFunc("/features/{id}/setup", s.atLeast(auth.RoleAdmin, s.handleSetUpFeature)).Methods(http.MethodPost)
}

func (s *Server) handleFeatures(w http.ResponseWriter, r *http.Request) {
	if s.deps.Features == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Feature setup isn't available.", nil)
		return
	}
	writeJSON(w, http.StatusOK, s.deps.Features(r.Context()))
}

// handleSetUpFeature starts installing what a feature needs and answers
// with the feature; what started keeps going when one part fails.
func (s *Server) handleSetUpFeature(w http.ResponseWriter, r *http.Request) {
	if s.deps.SetUpFeature == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Feature setup isn't available.", nil)
		return
	}
	f, err := s.deps.SetUpFeature(r.Context(), mux.Vars(r)["id"])
	switch {
	case err == nil:
		writeJSON(w, http.StatusAccepted, f)
	case errors.Is(err, features.ErrUnknown):
		writeErrFrom(w, http.StatusNotFound, "FEATURE_UNKNOWN", err)
	default:
		// A part's own code, such as a license to accept, says it best.
		if own, _ := contracts.ErrorCode(err); own != "" {
			writeErrFrom(w, http.StatusConflict, own, err)
			return
		}
		writeErr(w, http.StatusConflict, "FEATURE_SETUP_FAILED", err.Error(), map[string]any{"detail": err.Error()})
	}
}
