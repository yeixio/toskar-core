package ratings

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQualityResults(t *testing.T) {
	qualityCache.Clear()
	t.Cleanup(qualityCache.Clear)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"schema":1,"method":"m","suites":{"quality":"q"},"results":[{"model":"a","name":"A","date":"2026-11-02","hardware":"h","method_version":1,"scores":{"quality":{"passed":5,"of":6}},"seconds_per_question":1.5}]}`))
	}))
	defer srv.Close()
	c := &Client{QualityURL: srv.URL, HTTP: srv.Client()}
	got, err := c.QualityResults(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Live || got.Method != "m" || len(got.Results) != 1 || got.Results[0].Scores["quality"].Passed != 5 || got.Results[0].MethodVersion != 1 {
		t.Fatalf("%+v", got)
	}
	if _, err := c.QualityResults(context.Background()); err != nil || calls != 1 {
		t.Fatalf("not kept: %d calls, %v", calls, err)
	}
}

func TestQualityResultsFailure(t *testing.T) {
	qualityCache.Clear()
	t.Cleanup(qualityCache.Clear)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer srv.Close()
	c := &Client{QualityURL: srv.URL, HTTP: srv.Client()}
	if _, err := c.QualityResults(context.Background()); err == nil {
		t.Fatal("no error")
	}
}
