package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// POST /models/warm passes its profile and model to the warm-up and answers
// with what it did at once (#498).
func TestWarmModelRoute(t *testing.T) {
	mgr, err := config.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var got contracts.ModelWarmRequest
	srv := NewServer(Dependencies{
		Config: mgr,
		WarmModel: func(_ context.Context, req contracts.ModelWarmRequest) (contracts.ModelWarmResponse, error) {
			got = req
			return contracts.ModelWarmResponse{ModelID: "llama-3b", Status: "loading"}, nil
		},
	})
	r := localRequest(http.MethodPost, "/api/v1/models/warm", strings.NewReader(`{"profile_id":"general","model_id":"auto"}`))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out contracts.ModelWarmResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Status != "loading" || out.ModelID != "llama-3b" {
		t.Fatalf("answer = %+v", out)
	}
	if got.ProfileID != "general" || got.ModelID != "auto" {
		t.Fatalf("request = %+v", got)
	}
}

// Anyone who may chat may warm a model, and so may a paired device: it's
// how a phone or watch gets its answer sooner.
func TestWarmModelReach(t *testing.T) {
	if !auth.DeviceMayReach(http.MethodPost, "/api/v1/models/warm") {
		t.Fatal("a paired device can't warm a model")
	}
	if need := auth.RoleNeeded(http.MethodPost, "/api/v1/models/warm"); need != auth.RoleNeeded(http.MethodPost, "/api/v1/chat") {
		t.Fatalf("warm needs %v, chat needs %v", need, auth.RoleNeeded(http.MethodPost, "/api/v1/chat"))
	}
}
