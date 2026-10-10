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

	"github.com/yeixio/toskar-core/internal/mimir"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestKnowledgeRoutes(t *testing.T) {
	srv := NewServer(Dependencies{})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/api/v1/knowledge/sources", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("unbound knowledge: got %d", rec.Code)
	}

	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	srv.BindKnowledge(mimir.NewStore(db.SQL, t.TempDir()))

	do := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, localRequest(method, path, strings.NewReader(body)))
		return rec
	}
	rec = do(http.MethodPost, "/api/v1/knowledge/sources", `{"kind":"text","filename":"hours.md","text":"Open 9 to 5 on weekdays."}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var src mimir.Source
	_ = json.Unmarshal(rec.Body.Bytes(), &src)
	if src.Status != mimir.StatusReady || src.Path != "" || src.Filename != "hours.md" {
		t.Fatalf("created %+v", src)
	}

	if rec = do(http.MethodGet, "/api/v1/knowledge/sources/"+src.ID+"/content", ""); !strings.Contains(rec.Body.String(), "Open 9 to 5") {
		t.Fatalf("content: %d %s", rec.Code, rec.Body)
	}
	rec = do(http.MethodPost, "/api/v1/knowledge/search", `{"query":"weekday hours"}`)
	var hits []mimir.Hit
	_ = json.Unmarshal(rec.Body.Bytes(), &hits)
	if rec.Code != http.StatusOK || len(hits) != 1 || hits[0].SourceName != "hours.md" {
		t.Fatalf("search: %d %s", rec.Code, rec.Body)
	}

	if rec = do(http.MethodPost, "/api/v1/knowledge/sources", `{"kind":"path","path":"/definitely/not/here"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing path: %d", rec.Code)
	}
	if rec = do(http.MethodGet, "/api/v1/knowledge/sources/nope", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id: %d", rec.Code)
	}
	if rec = do(http.MethodDelete, "/api/v1/knowledge/sources/"+src.ID, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
}

func TestKnowledgeDatabaseSourceThroughAPI(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	shop := filepath.Join(t.TempDir(), "shop.db")
	if _, err := db.SQL.Exec(`ATTACH DATABASE ? AS shop; CREATE TABLE shop.hours (day TEXT, opens TEXT); INSERT INTO shop.hours VALUES ('Monday', '9am'); DETACH DATABASE shop`, shop); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Dependencies{})
	srv.BindKnowledge(mimir.NewStore(db.SQL, t.TempDir()))
	rec := httptest.NewRecorder()
	body := `{"kind":"database","name":"Hours","remote":{"driver":"sqlite","database":"` + filepath.ToSlash(shop) + `","query":"SELECT day, opens FROM hours"}}`
	srv.Handler().ServeHTTP(rec, localRequest(http.MethodPost, "/api/v1/knowledge/sources", strings.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var src mimir.Source
	_ = json.Unmarshal(rec.Body.Bytes(), &src)
	if src.Kind != mimir.KindDatabase || src.ChunkCount != 1 || src.Remote == nil || src.Remote.Query != "SELECT day, opens FROM hours" {
		t.Fatalf("source = %+v", src)
	}
	rec = httptest.NewRecorder()
	body = `{"kind":"database","remote":{"driver":"sqlite","database":"x.db","query":"DROP TABLE hours"}}`
	srv.Handler().ServeHTTP(rec, localRequest(http.MethodPost, "/api/v1/knowledge/sources", strings.NewReader(body)))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "only SELECT") {
		t.Fatalf("write query: %d %s", rec.Code, rec.Body)
	}
}

// Save to Knowledge on the phone adds a source and the profiles that use it
// in one request; a wrong profile saves nothing (toskar-apps#23).
func TestCreateKnowledgeForProfiles(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	profiles := map[string]contracts.AIProfile{
		"general":  {ID: "general", Name: "General", KnowledgeSources: []string{"older"}},
		"research": {ID: "research", Name: "Research"},
	}
	srv := NewServer(Dependencies{
		GetProfile: func(_ context.Context, id string) (contracts.AIProfile, error) {
			p, ok := profiles[id]
			if !ok {
				return contracts.AIProfile{}, errors.New("not found")
			}
			return p, nil
		},
		UpdateProfile: func(_ context.Context, p contracts.AIProfile) (contracts.AIProfile, error) {
			profiles[p.ID] = p
			return p, nil
		},
	})
	knowledge := mimir.NewStore(db.SQL, t.TempDir())
	srv.BindKnowledge(knowledge)
	do := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, localRequest(http.MethodPost, "/api/v1/knowledge/sources", strings.NewReader(body)))
		return rec
	}

	rec := do(`{"kind":"text","filename":"hours.md","text":"Open 9 to 5.","profile_ids":["general","nope"]}`)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "PROFILE_NOT_FOUND") {
		t.Fatalf("unknown profile: %d %s", rec.Code, rec.Body)
	}
	if list, _ := knowledge.List(context.Background()); len(list) != 0 {
		t.Fatalf("a wrong profile still saved %d sources", len(list))
	}

	rec = do(`{"kind":"text","filename":"hours.md","text":"Open 9 to 5.","profile_ids":["general","research"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var src mimir.Source
	_ = json.Unmarshal(rec.Body.Bytes(), &src)
	if got := profiles["general"].KnowledgeSources; len(got) != 2 || got[0] != "older" || got[1] != src.ID {
		t.Fatalf("general uses %v", got)
	}
	if got := profiles["research"].KnowledgeSources; len(got) != 1 || got[0] != src.ID {
		t.Fatalf("research uses %v", got)
	}
}
