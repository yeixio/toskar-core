// Package quality runs Toskar's quality test set (spec §64): a fixed set
// of representative requests, each with the behavior it must have. By
// default it runs against the stub model, in-process, with the model's
// replies scripted, so CI checks routing, retrieval, planning, checking,
// approvals, and history on every change. With TOSKAR_QUALITY_MODEL_URL
// set to an OpenAI-compatible server such as llama-server, the same
// in-process run sends every model call to that model, with the web still
// answered from web.json, so a release is checked against a real model
// with the same pages each time. With TOSKAR_QUALITY_URL set to a running
// daemon, the cases run against its real models and the live web.
//
// The iPhone app (yeixio/toskar-apps) runs the cases marked "phone"
// through its on-device chat, so its answers are held to the same
// expectations.
package quality

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/internal/runlog"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Case is one request and the behavior it must have.
type Case struct {
	ID      string                  `json:"id"`
	What    string                  `json:"what"`
	Setup   Setup                   `json:"setup"`
	History []pluginapi.ChatMessage `json:"history"`
	// HistoryPad adds about this many characters of notes to each history
	// message after the first, so a chat grows past auto-compact's
	// threshold (#342) without the case spelling the text out.
	HistoryPad int `json:"history_pad"`
	// Attach are files sent with the message (#510).
	Attach  []Attachment `json:"attach"`
	Message string       `json:"message"`
	// FollowUp is a second message in the same chat, sent after the answer
	// to Message, with nothing attached; the case checks its answer, as
	// "what breed might it be?" checks a picture stays in context (#449).
	FollowUp string   `json:"follow_up"`
	Stub     []string `json:"stub"`
	// StubQuery is what the stub writes when asked for a follow-up's web
	// search; without it, the message itself.
	StubQuery string `json:"stub_query"`
	StubOnly  bool   `json:"stub_only"`
	// RealOnly cases need real tools the stub has no stand-in for, such as
	// text recognition or transcription.
	RealOnly bool `json:"real_only"`
	// Fixtures needs the in-process run's stand-in tools, such as an image
	// tool that saves a tiny picture; a daemon has its real ones.
	Fixtures bool   `json:"fixtures"`
	Expect   Expect `json:"expect"`
	// Platforms the case runs on: "core", "phone", or both. Empty is core.
	Platforms []string `json:"platforms"`
}

// On reports whether the case runs on platform.
func (c Case) On(platform string) bool {
	if len(c.Platforms) == 0 {
		return platform == "core"
	}
	return slices.Contains(c.Platforms, platform)
}

// Setup is what a case needs before the request.
type Setup struct {
	Knowledge []struct {
		Filename string `json:"filename"`
		Text     string `json:"text"`
	} `json:"knowledge"`
	// Tools sets tool policies on the case's profile.
	Tools map[string]string `json:"tools"`
	// Topics are the profile's topic controls (#345).
	Topics *contracts.TopicPolicy `json:"topics"`
	// Deliberate is the profile's orchestration.deliberate (#459), for
	// measuring a mode; empty leaves the profile's own.
	Deliberate string `json:"deliberate"`
	// NoVision runs the case with no model that can see pictures (#449):
	// the stub gets no seeing stand-in, and a daemon with a vision model
	// installed skips the case.
	NoVision bool `json:"no_vision"`
}

// Expect is the behavior a case checks. Empty fields are not checked.
type Expect struct {
	Effort        string   `json:"effort"`
	Plan          *bool    `json:"plan"`
	MinWorkers    int      `json:"min_workers"`
	Lookup        *bool    `json:"lookup"`
	NoToolsRun    bool     `json:"no_tools_run"`
	Sources       []string `json:"sources"`
	Verified      bool     `json:"verified"`
	NoticeMatches string   `json:"notice_matches"`
	StepsMatch    string   `json:"steps_match"`
	ApprovalFor   []string `json:"approval_for"`
	NotRun        []string `json:"not_run"`
	// ToolsRun are tools that must run, such as image.generate for "draw
	// a dog".
	ToolsRun       []string `json:"tools_run"`
	PromptContains []string `json:"prompt_contains"`
	// PromptLacks are texts the model must never be sent (stub only), such
	// as a page that doesn't answer the question.
	PromptLacks   []string `json:"prompt_lacks"`
	AnswerMatches string   `json:"answer_matches"`
	// AnswerLacks fails an answer that matches it, with either model.
	AnswerLacks string `json:"answer_lacks"`
	// AnswerRealOnly checks AnswerMatches only against a real model; the
	// stub's scripted answer proves nothing.
	AnswerRealOnly bool `json:"answer_real_only"`
	// NoFalseClaims requires an answer that says it changed something,
	// when nothing that changes things ran, to carry a notice saying so.
	NoFalseClaims bool `json:"no_false_claims"`
	// NoDeflection fails an answer that sends the person off to search,
	// or claims to browse, by the set's "deflection" pattern, instead of
	// answering: an answer that also matches AnswerMatches answered. A
	// scripted answer proves nothing, so the stub skips it.
	NoDeflection bool `json:"no_deflection"`
	// NoDestructiveAdvice fails an answer that hands the person a command
	// that deletes or wipes files to run themselves, such as rm -rf.
	NoDestructiveAdvice bool `json:"no_destructive_advice"`
	// Files are extensions the answer's files must include, such as ".pdf":
	// files the assistant made in this turn.
	Files []string `json:"files"`
	// FileText, by extension, are texts a file the assistant made must hold,
	// read the way Toskar reads an attachment (#510): the file is opened,
	// not only named.
	FileText map[string][]string `json:"file_text"`
	// ImageSent requires an attached picture to reach the model with the
	// message (stub only: the request it got is checked).
	ImageSent bool `json:"image_sent"`
	// CantSee requires a picture sent with no model that can see it to be
	// answered honestly (#449): an offer to set up a model that sees, or
	// the notice that the answer didn't look at it.
	CantSee bool `json:"cant_see"`
	// TopicHeld is whether the profile's topic controls held the message
	// or replaced its answer with the set reply (#345), by the run trace.
	TopicHeld *bool `json:"topic_held"`
}

// caseFile is cases.json.
type caseFile struct {
	// Deflection matches answers that tell the person to find the answer
	// themselves. Core and the iPhone app check with the same pattern.
	Deflection string `json:"deflection"`
	Cases      []Case `json:"cases"`
}

// Result is what a request did.
type Result struct {
	Answer string
	Meta   *contracts.MessageMeta
	Run    *runlog.Run
	// Events are event types in order, with their payloads.
	Events []Event
	// Prompts are what the model was sent (stub only).
	Prompts [][]pluginapi.ChatMessage
	// Files are the files the assistant made in the turn, by name.
	Files map[string][]byte
}

// Event is one event during a request.
type Event struct {
	Type    string
	Payload map[string]any
}

// Driver runs a case against a model.
type Driver interface {
	Name() string
	Run(t *testing.T, c Case) Result
}

func loadCases(t *testing.T) caseFile {
	t.Helper()
	raw, err := os.ReadFile("cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var file caseFile
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	return file
}

// minPass is the share of cases a real model must pass: every case for the
// stub, whose replies are scripted, and TOSKAR_QUALITY_MIN_PASS (default
// 0.9) for a real model, whose answers vary from run to run.
func minPass(driver string) float64 {
	if driver == "stub" {
		return 1
	}
	if v, err := strconv.ParseFloat(config.Env("QUALITY_MIN_PASS"), 64); err == nil && v > 0 && v <= 1 {
		return v
	}
	return 0.9
}

// cantSeeNotice is the notice an answer gets when no model could see its
// pictures (chat:notices.noVision, in English).
var cantSeeNotice = regexp.MustCompile(`(?i)no installed model can see pictures`)

// seesPictures reports a daemon with a model installed that can see, where
// a case about having none can't run (#449). The stub has none until a case
// installs one.
func seesPictures(t *testing.T, d Driver) bool {
	real, ok := d.(realDriver)
	if !ok {
		return false
	}
	var list []struct {
		Installed    bool `json:"installed"`
		Capabilities struct {
			Vision bool `json:"vision"`
		} `json:"capabilities"`
	}
	real.do(t, http.MethodGet, "/api/v1/models", nil, &list)
	for _, m := range list {
		if m.Installed && m.Capabilities.Vision {
			return true
		}
	}
	return false
}

func TestQualitySet(t *testing.T) {
	d, build := qualityDriver(t)
	file := loadCases(t)
	deflection := regexp.MustCompile(file.Deflection)
	var report []reportRow
	for _, c := range file.Cases {
		if !c.On("core") {
			continue
		}
		// A run that can't go on, such as one whose daemon now wants a key,
		// stops with that reason instead of failing every case left.
		if stopped(t, d, c) {
			break
		}
		if c.Setup.NoVision && seesPictures(t, d) {
			t.Logf("%s: skipped, since a model that sees is installed", c.ID)
			continue
		}
		if row, ok := runCase(t, d, c, deflection); ok {
			report = append(report, row)
		}
	}
	passed := 0
	for _, row := range report {
		if len(row.Failures) == 0 {
			passed++
		}
	}
	rate := 1.0
	if len(report) > 0 {
		rate = float64(passed) / float64(len(report))
	}
	t.Logf("quality: %d of %d cases passed (%.0f%%) with the %s model", passed, len(report), rate*100, d.Name())
	if path := config.Env("QUALITY_REPORT"); path != "" {
		md := markdownReport(d.Name(), report, rate, minPass(d.Name()))
		writeReport(t, d, build, path, md)
	}
	if rate < minPass(d.Name()) {
		t.Errorf("%.0f%% of cases passed; at least %.0f%% must", rate*100, minPass(d.Name())*100)
	}
}

// qualityDriver is the model the set runs against: the stub, a served
// model, or a daemon's real models, with how it was built for the report.
func qualityDriver(t *testing.T) (Driver, string) {
	var d Driver = stubDriver{}
	if url := config.Env("QUALITY_MODEL_URL"); url != "" {
		d = stubDriver{model: strings.TrimRight(url, "/")}
	}
	build := ""
	if url := config.Env("QUALITY_URL"); url != "" {
		real := newRealDriver(strings.TrimRight(url, "/"))
		// TOSKAR_QUALITY_MODEL tests one model, installed first; otherwise
		// a daemon started fresh for the run, as the self-hosted job does,
		// gets the models it recommends.
		if m := config.Env("QUALITY_MODEL"); m != "" && m != "recommended" {
			real.model = m
			real.installModel(t, m)
		} else if config.Env("QUALITY_INSTALL") == "recommended" {
			real.installRecommended(t)
		}
		model := real.model
		if model == "" {
			model = "the recommended models"
		}
		build = real.version(t) + " on " + real.hardware(t) + ", with " + model
		d = real
	}
	return d, build
}

// stopped reports a run that can't go on, failing the cases left.
func stopped(t *testing.T, d Driver, c Case) bool {
	if s, ok := d.(interface{ Stopped() string }); ok && s.Stopped() != "" {
		t.Errorf("stopped before %s and the cases after it: %s", c.ID, s.Stopped())
		return true
	}
	return false
}

// runCase runs and checks one case. ok is false for a case skipped for
// this model. A case that stops before it is checked, such as one whose
// daemon refuses or drops the request, counts as failed; otherwise a run
// that broke partway would report only the cases that got an answer.
func runCase(t *testing.T, d Driver, c Case, deflection *regexp.Regexp) (reportRow, bool) {
	var row reportRow
	recorded, skipped := false, false
	t.Run(c.ID, func(t *testing.T) {
		if c.StubOnly && d.Name() != "stub" {
			skipped = true
			t.Skip("checks a scripted reply")
		}
		if c.RealOnly && d.Name() != "real" {
			skipped = true
			t.Skip("needs the real tools")
		}
		if c.Fixtures && d.Name() == "real" {
			skipped = true
			t.Skip("needs the in-process stand-in tools")
		}
		r := d.Run(t, c)
		failures := check(d.Name(), c, r, deflection)
		recorded = true
		row = reportRow{Case: c, Answer: r.Answer, Failures: failures}
		for _, f := range failures {
			// A real model is held to a pass rate, not every case.
			if d.Name() == "stub" {
				t.Error(f)
			} else {
				t.Log("FAIL: " + f)
			}
		}
	})
	if skipped {
		return row, false
	}
	if !recorded {
		row = reportRow{Case: c, Failures: []string{"the case stopped before it could be checked; see its log"}}
	}
	return row, true
}

// writeReport writes a Markdown report with how the model was built and
// ran.
func writeReport(t *testing.T, d Driver, build, path, md string) {
	// How the model ran, from the daemon's report once the cases have
	// loaded it (#317): on the GPU with its layers, or on the CPU.
	if real, ok := d.(realDriver); ok && build != "" {
		if ran := real.ranOn(t); ran != "" {
			build += "; the model ran " + ran
		}
	}
	if build != "" {
		md = strings.Replace(md, "\n\n", "\n\nTested Toskar "+build+".\n\n", 1)
	}
	if err := os.WriteFile(path, []byte(md), 0o644); err != nil {
		t.Errorf("report: %v", err)
	}
}

// reportRow is one case's outcome, for the report.
type reportRow struct {
	Case     Case
	Answer   string
	Failures []string
}

// markdownReport lists every case with its answer, failures first, so a
// release can be read and not only scored.
func markdownReport(driver string, rows []reportRow, rate, min float64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Chat quality: core, %s model\n\n", driver)
	fmt.Fprintf(&b, "%.0f%% of %d cases passed; at least %.0f%% must.\n\n", rate*100, len(rows), min*100)
	sorted := slices.Clone(rows)
	slices.SortStableFunc(sorted, func(a, b reportRow) int { return len(b.Failures) - len(a.Failures) })
	for _, row := range sorted {
		mark := "PASS"
		if len(row.Failures) > 0 {
			mark = "FAIL"
		}
		fmt.Fprintf(&b, "## %s %s\n\n%s\n\n> %s\n\n", mark, row.Case.ID, row.Case.What, strings.ReplaceAll(row.Case.Message, "\n", " "))
		for _, f := range row.Failures {
			fmt.Fprintf(&b, "- %s\n", f)
		}
		answer := strings.TrimSpace(row.Answer)
		if answer == "" {
			answer = "(no answer)"
		}
		fmt.Fprintf(&b, "\n```text\n%s\n```\n\n", answer)
	}
	return b.String()
}

func (r Result) has(eventType string) bool {
	for _, e := range r.Events {
		if e.Type == eventType {
			return true
		}
	}
	return false
}

func (r Result) toolIDs(eventType string) []string {
	var out []string
	for _, e := range r.Events {
		if e.Type == eventType {
			if id, _ := e.Payload["tool_id"].(string); id != "" {
				out = append(out, id)
			}
		}
	}
	return out
}

func check(driver string, c Case, r Result, deflection *regexp.Regexp) []string {
	x := c.Expect
	var failures []string
	fail := func(format string, args ...any) {
		failures = append(failures, fmt.Sprintf("%s (%s): "+format, append([]any{c.What, driver}, args...)...))
	}
	if x.Effort != "" && (r.Run == nil || r.Run.Effort != x.Effort) {
		fail("effort = %v, want %s", runField(r, func(run *runlog.Run) any { return run.Effort }), x.Effort)
	}
	if x.Plan != nil {
		planned := r.has("plan.created")
		if planned != *x.Plan {
			fail("planned = %v", planned)
		}
	}
	if x.MinWorkers > 0 && (r.Run == nil || r.Run.Workers < x.MinWorkers) {
		fail("workers = %v, want at least %d", runField(r, func(run *runlog.Run) any { return run.Workers }), x.MinWorkers)
	}
	if x.Lookup != nil && r.has("chat.lookup") != *x.Lookup {
		fail("looked up = %v", r.has("chat.lookup"))
	}
	if ran := r.toolIDs("tool.started"); x.NoToolsRun && len(ran) > 0 {
		fail("tools ran: %v", ran)
	}
	for _, kind := range x.Sources {
		if r.Meta == nil || !slices.ContainsFunc(r.Meta.Sources, func(s contracts.Citation) bool { return s.Kind == kind }) {
			fail("no %s source in %+v", kind, metaSources(r))
		}
	}
	for _, ext := range x.Files {
		if r.Meta == nil || !slices.ContainsFunc(r.Meta.Files, func(f contracts.FileRef) bool {
			return f.Producer == "assistant" && strings.EqualFold(filepath.Ext(f.Name), ext)
		}) {
			fail("no %s file in the answer: %+v", ext, metaFiles(r))
		}
	}
	if x.ImageSent && driver == "stub" && !slices.ContainsFunc(r.Prompts, func(p []pluginapi.ChatMessage) bool {
		return slices.ContainsFunc(p, func(m pluginapi.ChatMessage) bool { return len(m.Images) > 0 })
	}) {
		fail("the picture never reached the model")
	}
	for _, f := range madeFileFailures(x.FileText, r.Files) {
		fail("%s", f)
	}
	if x.Verified && !r.has("verify.done") && (r.Run == nil || r.Run.VerificationPasses == 0) {
		fail("answer was not checked")
	}
	if x.CantSee {
		offered := r.Meta != nil && r.Meta.Setup != nil && r.Meta.Setup.Ability == "vision"
		noticed := r.Meta != nil && cantSeeNotice.MatchString(r.Meta.Notice)
		if !offered && !noticed {
			fail("a picture no model can see got neither an offer to set one up nor the notice; answer %q", r.Answer)
		}
	}
	if x.NoticeMatches != "" && (r.Meta == nil || !regexp.MustCompile(x.NoticeMatches).MatchString(r.Meta.Notice)) {
		fail("notice = %q", metaNotice(r))
	}
	if x.StepsMatch != "" {
		re := regexp.MustCompile(x.StepsMatch)
		if r.Meta == nil || !slices.ContainsFunc(r.Meta.Steps, func(s contracts.ActivityStep) bool { return re.MatchString(s.Text) }) {
			fail("no step matches %q", x.StepsMatch)
		}
	}
	// A real model may never try the action; then there is nothing to approve.
	if driver == "stub" {
		for _, id := range x.ApprovalFor {
			if !slices.Contains(r.toolIDs("tool.requested"), id) {
				fail("%s did not ask first (requested: %v)", id, r.toolIDs("tool.requested"))
			}
		}
	}
	for _, id := range x.ToolsRun {
		if !slices.Contains(r.toolIDs("tool.started"), id) {
			fail("%s did not run (ran: %v)", id, r.toolIDs("tool.started"))
		}
	}
	for _, id := range x.NotRun {
		if slices.Contains(r.toolIDs("tool.started"), id) {
			fail("%s ran without approval", id)
		}
	}
	if driver == "stub" {
		for _, want := range x.PromptContains {
			if !promptHas(r.Prompts, want) {
				fail("the model never saw %q", want)
			}
		}
		for _, unwanted := range x.PromptLacks {
			if promptHas(r.Prompts, unwanted) {
				fail("the model was sent %q", unwanted)
			}
		}
	}
	if x.NoFalseClaims && huginn.ClaimsAction(r.Answer) && len(r.toolIDs("tool.started")) == 0 &&
		(r.Meta == nil || !strings.Contains(strings.ToLower(r.Meta.Notice), "nothing was changed")) {
		fail("answer claims a change that never happened, with no notice: %q", r.Answer)
	}
	if x.AnswerLacks != "" && regexp.MustCompile(x.AnswerLacks).MatchString(r.Answer) {
		fail("answer %q matches %q", r.Answer, x.AnswerLacks)
	}
	if x.AnswerMatches != "" && (driver != "stub" || !x.AnswerRealOnly) {
		if !regexp.MustCompile(x.AnswerMatches).MatchString(r.Answer) {
			fail("answer %q does not match %q", r.Answer, x.AnswerMatches)
		}
	}
	// A deflection fails an answer that points elsewhere instead of
	// answering: one with the facts it was asked for and a "visit their site
	// for more" is an answer.
	answered := x.AnswerMatches != "" && regexp.MustCompile(x.AnswerMatches).MatchString(r.Answer)
	if x.NoDeflection && driver != "stub" && !answered && deflection.MatchString(r.Answer) {
		fail("answer sends the person off to find it themselves: %q", deflection.FindString(r.Answer))
	}
	if x.NoDestructiveAdvice && huginn.DestructiveCommand(r.Answer) != "" {
		fail("answer gives the person a destructive command to run: %q", huginn.DestructiveCommand(r.Answer))
	}
	if x.TopicHeld != nil {
		if r.Run == nil || r.Run.Topic == "" {
			fail("the topic check didn't run or gave nothing usable")
		} else if held := topicHeld(r); held != *x.TopicHeld {
			label := runField(r, func(run *runlog.Run) any { return run.Topic })
			if *x.TopicHeld {
				fail("not held (topic check: %v); answer %q", label, r.Answer)
			} else {
				fail("wrongly refused (topic check: %v); answer %q", label, r.Answer)
			}
		}
	}
	return failures
}

// topicHeld reports a message the topic controls held, or whose answer
// they replaced (#345).
func topicHeld(r Result) bool {
	return r.Run != nil && (r.Run.Topic == "off_topic" || r.Run.Topic == "answer_off_topic")
}

func promptHas(prompts [][]pluginapi.ChatMessage, want string) bool {
	for _, p := range prompts {
		for _, m := range p {
			if strings.Contains(m.Content, want) {
				return true
			}
		}
	}
	return false
}

func runField(r Result, f func(*runlog.Run) any) any {
	if r.Run == nil {
		return "(no run)"
	}
	return f(r.Run)
}

func metaFiles(r Result) any {
	if r.Meta == nil {
		return nil
	}
	return r.Meta.Files
}

func metaSources(r Result) any {
	if r.Meta == nil {
		return nil
	}
	return r.Meta.Sources
}

func metaNotice(r Result) string {
	if r.Meta == nil {
		return ""
	}
	return r.Meta.Notice
}

// padNote is the filler HistoryPad adds: notes with no facts the question
// needs.
const padNote = "Notes to keep in mind: the trail was quiet, the weather mild, and nothing else happened worth remembering. "

// padded is a history message's content with c's padding, after the first.
func padded(c Case, i int, content string) string {
	if c.HistoryPad <= 0 || i == 0 {
		return content
	}
	return content + "\n\n" + strings.Repeat(padNote, c.HistoryPad/len(padNote)+1)[:c.HistoryPad]
}
