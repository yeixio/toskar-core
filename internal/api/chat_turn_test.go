package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Someone else's chat, or none, is not found (#1017).
func TestChatTurn(t *testing.T) {
	srv := NewServer(Dependencies{
		ChatTurn: func(_ context.Context, id string) (contracts.ChatTurn, error) {
			if id != "mine" {
				return contracts.ChatTurn{}, errors.New("not found")
			}
			return contracts.ChatTurn{Running: true, Output: "ok"}, nil
		},
	})
	get := func(id string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+id+"/turn", nil)
		r = mux.SetURLVars(r, map[string]string{"id": id})
		srv.handleChatTurn(rec, r)
		return rec
	}
	rec := get("mine")
	var turn contracts.ChatTurn
	if err := json.Unmarshal(rec.Body.Bytes(), &turn); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || !turn.Running || turn.Output != "ok" {
		t.Errorf("mine: %d %s", rec.Code, rec.Body)
	}
	if rec := get("theirs"); rec.Code != http.StatusNotFound {
		t.Errorf("theirs: %d", rec.Code)
	}
}
