package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/yeixio/yggdrasil-core/internal/gjallarhorn"
)

// BindNotifications attaches the notification center routes (Gjallarhorn).
func (s *Server) BindNotifications(hub *gjallarhorn.Hub) { s.notifications = hub }

func (s *Server) notificationRoutes(api *mux.Router) {
	api.HandleFunc("/notifications", s.notificationHandler(s.handleListNotifications)).Methods(http.MethodGet)
	api.HandleFunc("/notifications/read", s.notificationHandler(s.handleReadNotifications)).Methods(http.MethodPost)
	api.HandleFunc("/notifications/{id}/dismiss", s.notificationHandler(s.handleDismissNotification)).Methods(http.MethodPost)
}

func (s *Server) notificationHandler(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.notifications == nil {
			writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Notifications are not available.", nil)
			return
		}
		h(w, r)
	}
}

// handleListNotifications returns recent notifications, newest first, and
// the unread count. ?unread=1 lists unread ones only.
func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	list, unread, err := s.notifications.List(r.Context(), r.URL.Query().Get("unread") == "1", 50)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "NOTIFICATIONS_FAILED", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notifications": list, "unread": unread})
}

// handleReadNotifications marks the given ids read, or all with an empty list.
func (s *Server) handleReadNotifications(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
			return
		}
	}
	if err := s.notifications.MarkRead(r.Context(), body.IDs); err != nil {
		writeErr(w, http.StatusInternalServerError, "NOTIFICATIONS_FAILED", err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDismissNotification(w http.ResponseWriter, r *http.Request) {
	err := s.notifications.Dismiss(r.Context(), mux.Vars(r)["id"])
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "NOT_FOUND", "notification not found", nil)
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "NOTIFICATIONS_FAILED", err.Error(), nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
