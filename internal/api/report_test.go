package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/diagnostics"
	"github.com/yeixio/toskar-core/internal/logs"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A report has the kind of computer for anyone, and Core's log, scrubbed,
// only for an Admin (#588).
func TestReport(t *testing.T) {
	srv := NewServer(Dependencies{
		Hardware: func(context.Context) (contracts.HardwareInventory, error) {
			return contracts.HardwareInventory{
				OS: "darwin", Arch: "arm64", Hostname: "Mikes-Mac",
				CPU:          contracts.CPUInfo{Model: "Apple M3 Pro"},
				Memory:       contracts.MemoryInfo{TotalBytes: 36 << 30},
				Accelerators: []contracts.Accelerator{{Model: "Apple M3 Pro"}},
			}, nil
		},
		GetLog: func(_ context.Context, name string, _ int64) (logs.Content, error) {
			return logs.Content{Name: name, Content: `{"time":"t","level":"ERROR","msg":"bind failed on Mikes-Mac","error":"from 8.8.8.8"}`}, nil
		},
	})
	call := func(role auth.Role) diagnostics.Report {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/report", nil)
		r = r.WithContext(auth.WithPrincipal(r.Context(), auth.Principal{Person: auth.Person{ID: "p", Role: role}}))
		w := httptest.NewRecorder()
		srv.handleReport(w, r)
		var rep diagnostics.Report
		if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil || w.Code != http.StatusOK {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		return rep
	}
	member := call(auth.RoleMember)
	if member.OS != "darwin" || member.MemoryGB != 36 || len(member.GPUs) != 1 || member.Contract != contracts.ContractVersion {
		t.Fatalf("member report: %+v", member)
	}
	if len(member.Log) != 0 {
		t.Fatalf("a Member's report has the log: %q", member.Log)
	}
	admin := call(auth.RoleAdmin)
	if got := strings.Join(admin.Log, "\n"); got != `t ERROR bind failed on <computer> error="from <public IPv4>"` {
		t.Fatalf("admin log: %q", got)
	}
}

// Sending a report (#588): the person's words are scrubbed, Details adds
// Core's report, Members and paired devices may send, and each refusal has
// its status.
func TestSendReport(t *testing.T) {
	var sent []ProblemReport
	var fail error
	srv := NewServer(Dependencies{
		Hardware: func(context.Context) (contracts.HardwareInventory, error) {
			return contracts.HardwareInventory{OS: "linux", Arch: "amd64", Hostname: "linix-rig"}, nil
		},
		SendReport: func(_ context.Context, p ProblemReport) (ReportSent, error) {
			if fail != nil {
				return ReportSent{}, fail
			}
			sent = append(sent, p)
			return ReportSent{Status: "created", Number: 3}, nil
		},
	})
	call := func(body string) (int, string) {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/diagnostics/report", strings.NewReader(body))
		r = r.WithContext(auth.WithPrincipal(r.Context(), auth.Principal{Person: auth.Person{ID: "p", Role: auth.RoleMember}}))
		w := httptest.NewRecorder()
		srv.handleSendReport(w, r)
		var out struct {
			Status string
			Error  struct{ Code string }
		}
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out.Status + out.Error.Code
	}
	if status, got := call(`{"app":"web 1.9.0","where":"Chat","code":"EMPTY_ANSWER","error":"failed on linix-rig at 8.8.8.8","note":"mail me at me@example.com","email":"me@example.com","details":true}`); status != http.StatusOK || got != "created" {
		t.Fatalf("send: %d %s", status, got)
	}
	p := sent[0]
	if p.Product != "core" || p.Report == nil || p.Report.OS != "linux" || strings.Contains(p.Error, "linix-rig") || strings.Contains(p.Error, "8.8.8.8") ||
		strings.Contains(p.Note, "me@example.com") || p.Email != "me@example.com" {
		t.Fatalf("sent %+v", p)
	}
	if _, got := call(`{"product":"apps","app":"phone 1.9.0","error":"x"}`); got != "created" || sent[1].Report != nil || sent[1].Product != "apps" {
		t.Fatalf("without details: %+v", sent[1])
	}
	for body, want := range map[string]string{
		`{"product":"web","error":"x"}`:      "REPORT_INVALID",
		`{"error":""}`:                       "REPORT_INVALID",
		`{"error":"x","email":"not an one"}`: "REPORT_INVALID",
		`nope`:                               "INVALID_JSON",
	} {
		if status, got := call(body); status != http.StatusBadRequest || got != want {
			t.Errorf("%s: %d %s", body, status, got)
		}
	}
	for code, want := range map[string]int{
		"REPORTS_OFF":         http.StatusForbidden,
		"REPORT_RATE_LIMITED": http.StatusTooManyRequests,
		"REPORTS_UNAVAILABLE": http.StatusBadGateway,
	} {
		fail = contracts.NewError(code, nil, errors.New(code))
		if status, got := call(`{"error":"x"}`); status != want || got != code {
			t.Errorf("%s: %d %s", code, status, got)
		}
	}
	for _, route := range []string{"GET /api/v1/diagnostics/report", "POST /api/v1/diagnostics/report"} {
		method, path, _ := strings.Cut(route, " ")
		if auth.RoleNeeded(method, path) != auth.RoleMember || !auth.DeviceMayReach(method, path) {
			t.Errorf("%s isn't open to Members and their devices", route)
		}
	}
}
