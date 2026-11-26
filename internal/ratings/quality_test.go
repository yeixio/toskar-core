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

// Off in Settings, nothing is asked for; on, each read that isn't kept
// from the last hour is recorded (#1415).
func TestQualityResultsSwitchAndRecord(t *testing.T) {
	qualityCache.Clear()
	t.Cleanup(qualityCache.Clear)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()
	s, settings, _ := newService(t, srv)
	s.Client.QualityURL = srv.URL + "/quality.json"
	var recorded []string
	s.RecordQuality = func(_ context.Context, url string) { recorded = append(recorded, url) }
	ctx := context.Background()

	_ = settings.SetBool(ctx, SettingQuality, false)
	got, err := s.QualityResults(ctx)
	if err != nil || !got.Off || got.Live || calls != 0 || len(recorded) != 0 {
		t.Fatalf("off: %+v %v, %d calls, recorded %v", got, err, calls, recorded)
	}
	_ = settings.SetBool(ctx, SettingQuality, true)
	for range 2 {
		if got, err := s.QualityResults(ctx); err != nil || got.Off || !got.Live {
			t.Fatalf("on: %+v %v", got, err)
		}
	}
	if calls != 1 || len(recorded) != 1 || recorded[0] != srv.URL+"/quality.json" {
		t.Fatalf("%d calls, recorded %v", calls, recorded)
	}
}
