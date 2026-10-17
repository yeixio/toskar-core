package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/internal/api"
	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/egress"
	"github.com/yeixio/toskar-core/internal/version"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Problem reports (#588): Report this sends what went wrong to Toskar's
// report service on toskar.ai, which files it as an issue, grouped with
// others for the same error and version. The person sees everything first;
// an Admin can turn sending off; each one is in What left this computer.

const (
	// settingProblemReports is whether Report this may send to Toskar.
	settingProblemReports = "problem_reports"
	// defaultReportURL is the report service; TOSKAR_REPORT_URL replaces it.
	defaultReportURL = "https://toskar.ai/api/report"
)

var errReportsOff = contracts.NewError("REPORTS_OFF", nil, errors.New("an Admin turned off sending problem reports to Toskar"))

func reportURL() string {
	if u := strings.TrimSpace(config.Env("REPORT_URL")); u != "" {
		return u
	}
	return defaultReportURL
}

// sendReport sends a problem report on to the report service.
func (a *App) sendReport(ctx context.Context, p api.ProblemReport) (api.ReportSent, error) {
	if on, _ := a.Settings.GetBool(ctx, settingProblemReports, true); !on {
		return api.ReportSent{}, errReportsOff
	}
	body, err := json.Marshal(p)
	if err != nil {
		return api.ReportSent{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	target := reportURL()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return api.ReportSent{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Toskar/"+version.Version+" (+https://github.com/yeixio/toskar-core)")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return api.ReportSent{}, contracts.NewError("REPORTS_UNAVAILABLE", nil, fmt.Errorf("the report service couldn't be reached: %w", err))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error struct{ Code, Message string } `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		cause := fmt.Errorf("the report service answered %d %s", resp.StatusCode, e.Error.Message)
		switch e.Error.Code {
		case "REPORT_RATE_LIMITED":
			return api.ReportSent{}, contracts.NewError("REPORT_RATE_LIMITED", nil, cause)
		case "REPORT_INVALID":
			return api.ReportSent{}, contracts.NewError("REPORT_INVALID", nil, cause)
		}
		return api.ReportSent{}, contracts.NewError("REPORTS_UNAVAILABLE", nil, cause)
	}
	var sent api.ReportSent
	if err := json.Unmarshal(raw, &sent); err != nil {
		return api.ReportSent{}, contracts.NewError("REPORTS_UNAVAILABLE", nil, fmt.Errorf("the report service's answer: %w", err))
	}
	if a.Egress != nil {
		host := target
		if u, err := url.Parse(target); err == nil && u.Host != "" {
			host = u.Host
		}
		what := p.Code
		if what == "" {
			what = "an error"
		}
		detail := fmt.Sprintf("problem report: %s, %s", what, p.Where)
		if p.Report != nil {
			detail += ", with details"
		}
		if p.Email != "" {
			detail += ", with a reply address"
		}
		a.Egress.Add(ctx, egress.ProblemReport, host, detail)
	}
	return sent, nil
}
