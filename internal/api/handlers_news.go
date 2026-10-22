package api

import (
	"context"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/yeixio/toskar-core/internal/news"
)

// News is the AI news from toskar.ai (#750).
type News interface {
	Get(ctx context.Context) (news.View, error)
}

// BindNews attaches the news route.
func (s *Server) BindNews(n News) { s.news = n }

func (s *Server) newsRoutes(api *mux.Router) {
	api.HandleFunc("/news", s.handleGetNews).Methods(http.MethodGet)
}

// handleGetNews is the news, read from toskar.ai when the copy kept here is
// over six hours old. With the setting off it's enabled: false and empty;
// when toskar.ai can't be reached, the kept copy comes back as stale.
func (s *Server) handleGetNews(w http.ResponseWriter, r *http.Request) {
	if s.news == nil {
		writeJSON(w, http.StatusOK, news.View{Items: []news.Item{}})
		return
	}
	v, err := s.news.Get(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, "NEWS_UNAVAILABLE", "the AI news couldn't be read from toskar.ai: "+err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
