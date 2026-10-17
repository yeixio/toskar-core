package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/api"
	"github.com/yeixio/toskar-core/internal/diagnostics"
	"github.com/yeixio/toskar-core/internal/egress"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Report this sends to the report service, says when others hit the same
// thing, writes what left in the ledger, and an Admin's switch stops it
// (#588).
func TestSendReport(t *testing.T) {
	var got []api.ProblemReport
	answer := `{"status":"created","number":7}`
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p api.ProblemReport
		_ = json.NewDecoder(r.Body).Decode(&p)
		got = append(got, p)
		if !strings.HasPrefix(r.UserAgent(), "Toskar/") {
			t.Errorf("user agent %q", r.UserAgent())
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, answer)
	}))
	defer srv.Close()
	t.Setenv("TOSKAR_REPORT_URL", srv.URL+"/api/report")
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	a, err := New(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })
	ctx := context.Background()

	p := api.ProblemReport{Product: "core", App: "web 1.9.0", Where: "Chat", Code: "EMPTY_ANSWER", Error: "No answer", Email: "me@example.com",
		Report: &diagnostics.Report{Version: "1.9.0", Contract: "1.40"}}
	sent, err := a.sendReport(ctx, p)
	if err != nil || sent.Status != "created" || sent.Number != 7 {
		t.Fatalf("sent: %+v %v", sent, err)
	}
	if len(got) != 1 || got[0].Code != "EMPTY_ANSWER" || got[0].Report == nil || got[0].Email != "me@example.com" {
		t.Fatalf("the service got %+v", got)
	}
	recs, _ := a.Egress.List(ctx, egress.Filter{})
	if len(recs) != 1 || recs[0].Kind != egress.ProblemReport || !strings.Contains(recs[0].Detail, "EMPTY_ANSWER, Chat, with details, with a reply address") || strings.Contains(recs[0].Detail, "me@example.com") {
		t.Fatalf("ledger: %+v", recs)
	}

	// Others hit it too.
	answer = `{"status":"added","number":7}`
	if sent, _ := a.sendReport(ctx, p); sent.Status != "added" {
		t.Fatalf("added: %+v", sent)
	}

	// The service's own refusals keep their codes; anything else is
	// unavailable, for the clients to send by email.
	for _, c := range []struct {
		status int
		body   string
		code   string
	}{
		{http.StatusTooManyRequests, `{"error":{"code":"REPORT_RATE_LIMITED","message":"slow"}}`, "REPORT_RATE_LIMITED"},
		{http.StatusServiceUnavailable, `{"error":{"code":"REPORTS_UNAVAILABLE","message":"not set up"}}`, "REPORTS_UNAVAILABLE"},
		{http.StatusInternalServerError, `oops`, "REPORTS_UNAVAILABLE"},
	} {
		status, answer = c.status, c.body
		_, err := a.sendReport(ctx, p)
		if code, _ := contracts.ErrorCode(err); code != c.code {
			t.Errorf("%d: %v", c.status, err)
		}
	}
	status, answer = http.StatusOK, `{"status":"created","number":8}`

	// An Admin turned sending off: nothing goes.
	if err := a.applySettingsPatch(ctx, map[string]any{settingProblemReports: false}); err != nil {
		t.Fatal(err)
	}
	before := len(got)
	if _, err := a.sendReport(ctx, p); !errors.Is(err, errReportsOff) || len(got) != before {
		t.Fatalf("off: %v, %d sent", err, len(got)-before)
	}
	if v, _ := a.settingsView(ctx); v.ProblemReports {
		t.Fatal("settings still say reports are on")
	}

	// Unreachable.
	if err := a.applySettingsPatch(ctx, map[string]any{settingProblemReports: true}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TOSKAR_REPORT_URL", "http://127.0.0.1:1/api/report")
	if _, err := a.sendReport(ctx, p); err == nil {
		t.Fatal("sent to nowhere")
	} else if code, _ := contracts.ErrorCode(err); code != "REPORTS_UNAVAILABLE" {
		t.Fatalf("unreachable: %v", err)
	}
}
