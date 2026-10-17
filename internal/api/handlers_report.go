package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/diagnostics"
	"github.com/yeixio/toskar-core/internal/version"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// reportLogLines is how many of Core's last warnings and errors a report
// carries.
const reportLogLines = 40

// handleReport gathers what a "Report this" sends with a problem (#588):
// versions, the kind of computer, and for an Admin Core's last warnings
// and errors. Everything is scrubbed (diagnostics.Scrub) first, and the
// person sees all of it before it's sent.
func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.buildReport(r))
}

// reportHost is this computer's name, which scrubbing takes out.
func (s *Server) reportHost(ctx context.Context) (string, *contracts.HardwareInventory) {
	host, _ := os.Hostname()
	if s.deps.Hardware != nil {
		if inv, err := s.deps.Hardware(ctx); err == nil {
			if inv.Hostname != "" {
				host = inv.Hostname
			}
			return host, &inv
		}
	}
	return host, nil
}

func (s *Server) buildReport(r *http.Request) diagnostics.Report {
	offer := version.CurrentOffer()
	host, inv := s.reportHost(r.Context())
	rep := diagnostics.Report{
		Version:  offer.Version,
		Commit:   offer.Commit,
		Contract: contracts.ContractVersion,
	}
	if inv != nil {
		rep.OS, rep.Arch, rep.CPU = inv.OS, inv.Arch, inv.CPU.Model
		rep.MemoryGB = int((inv.Memory.TotalBytes + 1<<29) >> 30)
		for _, a := range inv.Accelerators {
			if name := a.Model; name != "" {
				rep.GPUs = append(rep.GPUs, name)
			}
		}
	}
	if s.deps.GetLog != nil && auth.PrincipalFrom(r.Context()).Person.Role.AtLeast(auth.RoleAdmin) {
		if c, err := s.deps.GetLog(r.Context(), "daemon.log", 256<<10); err == nil {
			rep.Log = diagnostics.ReportLog(c.Content, host, reportLogLines)
		}
	}
	rep.CPU = diagnostics.Scrub(rep.CPU, host)
	return rep
}

// ProblemReport is what POST /diagnostics/report sends on to Toskar's
// report service: what went wrong and where, in the person's words, and
// with Details on, Core's report.
type ProblemReport struct {
	// Product is core for Core, the web UI, and Toskar Pro; apps for the
	// Toskar app on a phone, tablet, TV, or watch.
	Product string              `json:"product"`
	App     string              `json:"app"`
	Where   string              `json:"where"`
	Code    string              `json:"code,omitempty"`
	Error   string              `json:"error"`
	Note    string              `json:"note,omitempty"`
	Email   string              `json:"email,omitempty"`
	Report  *diagnostics.Report `json:"report,omitempty"`
}

// ReportSent is the report service's answer: created for a new issue,
// added when others hit the same thing, and the issue's number.
type ReportSent struct {
	Status string `json:"status"`
	Number int    `json:"number"`
}

// SendReportFunc sends a problem report on to Toskar's report service.
type SendReportFunc func(ctx context.Context, p ProblemReport) (ReportSent, error)

// handleSendReport sends a problem report to Toskar's report service
// (#588), so it becomes an issue without the person writing an email. The
// clients show everything it sends first; Core scrubs the person's words
// and adds its report when Details is on. An Admin can turn sending off,
// and when the service can't be reached the clients send it by email.
func (s *Server) handleSendReport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Product string `json:"product"`
		App     string `json:"app"`
		Where   string `json:"where"`
		Code    string `json:"code"`
		Error   string `json:"error"`
		Note    string `json:"note"`
		Email   string `json:"email"`
		Details bool   `json:"details"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid JSON", nil)
		return
	}
	if s.deps.SendReport == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Sending reports isn't available.", nil)
		return
	}
	host, _ := s.reportHost(r.Context())
	clean := func(v string, max int) string {
		v = strings.TrimSpace(diagnostics.Scrub(v, host))
		if len(v) > max {
			v = v[:max]
		}
		return v
	}
	p := ProblemReport{
		Product: strings.TrimSpace(body.Product),
		App:     clean(body.App, 60),
		Where:   clean(body.Where, 120),
		Code:    strings.TrimSpace(body.Code),
		Error:   clean(body.Error, 2000),
		Note:    clean(body.Note, 4000),
		Email:   strings.TrimSpace(body.Email),
	}
	if p.Product == "" {
		p.Product = "core"
	}
	if p.Product != "core" && p.Product != "apps" {
		writeErr(w, http.StatusBadRequest, "REPORT_INVALID", "product must be core or apps", nil)
		return
	}
	if p.Error == "" && p.Code == "" {
		writeErr(w, http.StatusBadRequest, "REPORT_INVALID", "a report needs the error, or its code", nil)
		return
	}
	if p.Email != "" && (len(p.Email) > 254 || !strings.Contains(p.Email, "@") || strings.ContainsAny(p.Email, " \t\r\n<>")) {
		writeErr(w, http.StatusBadRequest, "REPORT_INVALID", "that isn't an email address", nil)
		return
	}
	if body.Details {
		rep := s.buildReport(r)
		p.Report = &rep
	}
	sent, err := s.deps.SendReport(r.Context(), p)
	if err != nil {
		code, _ := contracts.ErrorCode(err)
		switch code {
		case "REPORTS_OFF":
			writeErrFrom(w, http.StatusForbidden, code, err)
		case "REPORT_RATE_LIMITED":
			writeErrFrom(w, http.StatusTooManyRequests, code, err)
		case "REPORT_INVALID":
			writeErrFrom(w, http.StatusBadRequest, code, err)
		default:
			writeErrFrom(w, http.StatusBadGateway, "REPORTS_UNAVAILABLE", err)
		}
		return
	}
	writeJSON(w, http.StatusOK, sent)
}
