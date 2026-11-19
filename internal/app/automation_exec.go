package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/yeixio/toskar-core/internal/artifacts"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/egress"
	"github.com/yeixio/toskar-core/internal/gjallarhorn"
	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/internal/locale"
	"github.com/yeixio/toskar-core/internal/runlog"
	"github.com/yeixio/toskar-core/internal/share"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// automationExecutor runs a scheduled prompt through the profile's orchestrator.
// Placement goes through Norn. A model started for the run is marked used afterward
// so the normal idle sweeper can unload it. The run does not pin the model.
type automationExecutor struct {
	app *App
}

// Execute runs an automation and traces the run (§35).
func (e automationExecutor) Execute(ctx context.Context, automation automations.Automation) (automations.Execution, error) {
	// It runs as the person whose it is, with their chats, memories, and
	// files (#206), while they may still use Toskar.
	ctx, err := e.app.asAutomationPerson(ctx, automation)
	if err != nil {
		return automations.Execution{}, err
	}
	run := runlog.New(uuid.NewString(), "", automation.ProfileID, egress.SourceAutomation)
	run.SetLanguage(e.app.appLanguage(ctx))
	run.Note("automation", map[string]any{"name": automation.Name})
	result, err := e.execute(runlog.With(ctx, run), automation)
	status, runErr := runlog.StatusCompleted, error(nil)
	switch {
	case ctx.Err() != nil:
		status = runlog.StatusStopped
	case err != nil:
		status, runErr = runlog.StatusFailed, codedError(err)
		// The runner retries by the error's code, not its English (#204).
		err = runErr
	}
	if e.app != nil && e.app.RunLog != nil {
		if saveErr := e.app.RunLog.Save(context.WithoutCancel(ctx), run.Finish(status, runErr)); saveErr != nil && e.app.Logger != nil {
			e.app.Logger.Warn("save automation run", "automation", automation.Name, "error", saveErr)
		}
	}
	return result, err
}

func (e automationExecutor) execute(ctx context.Context, automation automations.Automation) (automations.Execution, error) {
	if e.app == nil || e.app.Profiles == nil || e.app.OrchRegistry == nil {
		return automations.Execution{}, fmt.Errorf("automation executor is not configured")
	}
	if strings.TrimSpace(automation.ProfileID) == "" {
		return automations.Execution{}, fmt.Errorf("profile is required")
	}
	if strings.TrimSpace(automation.ModelID) == "" {
		return automations.Execution{}, fmt.Errorf("model is required")
	}
	profile, err := e.app.Profiles.Get(ctx, automation.ProfileID)
	if err != nil {
		return automations.Execution{}, err
	}
	// A chat on this computer goes first; the run waits for it instead of
	// loading a model alongside it (§60).
	ctx = egress.WithRun(ctx, egress.Run{Source: egress.SourceAutomation, TaskID: "automation:" + automation.ID})
	// Its answer's "today" is in the time zone its schedule runs in.
	ctx = locale.WithTimeZone(ctx, automation.Schedule.TimeZone)
	work, err := e.app.enterWork(ctx, share.Automation, automation.Name, nil)
	if err != nil {
		return automations.Execution{}, err
	}
	defer work.Done()
	// Same stack as chat (spec §30): Auto picks the model, and the run gets
	// connected knowledge, relevant memories, and Huginn's effort budget.
	modelID := automation.ModelID
	if modelID == huginn.AutoModelID {
		lang := e.app.replyLanguage(ctx, "", automation.Prompt, automation.ResponseLanguage).Tag
		choice, err := e.app.chooseAuto(ctx, automation.Prompt, e.app.turnHasData(ctx, "", profile), lang)
		if err != nil {
			return automations.Execution{}, err
		}
		modelID = choice.Model.ID
	}
	// A visitor is greeted the moment they're seen, and talked with, when
	// the automation talks with them: Mike chose that over waiting about
	// 20 s for the run to look at the picture and write a greeting (#667).
	if automation.Say != nil && automation.Say.Listen {
		if out, ok, err := e.app.greetAtDoor(ctx, automation, modelID); ok {
			return out, err
		}
	}
	// A camera's picture goes to a model that can see it, on this
	// computer, as a photo attached in chat does (#667, #191).
	var pictures []artifacts.Artifact
	if len(automation.Cameras) > 0 {
		pictures, err = e.app.lookAtCameras(ctx, automation)
		if err != nil {
			return automations.Execution{}, err
		}
		if !e.app.seesImages(modelID) {
			m, ok := e.app.seeingModel(ctx)
			if !ok {
				return automations.Execution{}, fmt.Errorf("looking at a camera needs a model that can see pictures; install one on the Models page")
			}
			modelID = m.ID
		}
	}
	profile = tools.WithConnected(withChatModel(profile, modelID))
	// A Settings deny holds here too; ask still runs a tool the person
	// granted the automation (#1206).
	profile = e.app.withComputerToolRules(ctx, profile)
	if len(pictures) > 0 {
		profile = applyExecutionPolicy(profile, "local")
	}
	orch, err := e.app.OrchRegistry.Get(profile.OrchestratorID)
	if err != nil {
		return automations.Execution{}, err
	}
	if err := orch.ValidateProfile(ctx, profile); err != nil {
		return automations.Execution{}, err
	}

	var disabled map[string]struct{}
	if e.app.Tools != nil {
		disabled = e.app.Tools.Disabled()
	}
	env := &automationEnv{
		base: &chatExecEnv{
			app:           e.app,
			ctx:           ctx,
			profile:       profile,
			modelOverride: modelID,
			taskID:        automation.ID,
			turnPrompt:    automations.TaskPrompt(automation.Prompt),
			trace:         &turnTrace{lang: e.app.appLanguage(ctx)},
			// Results in the automation's response language (§22).
			responseLanguage: automation.ResponseLanguage,
			pictures:         pictures,
		},
		granted: automation.Tools,
		actions: automation.Actions,
	}
	// What a trigger delivered, a page, posts, files, or a webhook's body,
	// was written by someone else, so the run treats it as data, as chat
	// does after reading a page (§58, #204).
	// A camera's picture is someone else's too: a sign held up to the door
	// is data, not instructions (#667).
	if automations.ChangeNote(ctx) != "" || len(automation.Cameras) > 0 {
		env.base.trace.untrusted = true
		env.fromTrigger = true
	}
	// A profile that talks with strangers never gets the owner's memories,
	// as a doorbell's run doesn't (#667).
	if e.app.Muninn != nil && e.app.Settings != nil && !publicAudience(profile) && profile.Orchestration.Memory != "off" {
		if on, _ := e.app.Settings.GetBool(ctx, "memory_enabled", true); on {
			if mems, err := e.app.Muninn.Relevant(ctx, automation.Prompt, ""); err == nil {
				env.base.memories = mems
			}
		}
	}
	defer env.releaseModels()
	e.app.warmDoor(ctx, automation)

	stream, err := orch.Run(ctx, contracts.Task{
		ID:        automation.ID,
		ProfileID: profile.ID,
		// The task with its condition's instruction, which the server adds
		// rather than the saved prompt carrying it (#204).
		Prompt: withChangeNote(withChangeNote(withChangeNote(automations.RunPrompt(automation), runNote(ctx, automation)), camerasNote(automation.Cameras)), withChangeNote(actionsNote(automation.Actions), automations.SayNote(automation.Say))),
		Status: contracts.TaskRunning,
	}, tools.ForUnattended(profile, withActionTools(automation.Tools, automation.Actions), disabled), env)
	if err != nil {
		return env.execution(""), err
	}
	text, nodeID, runErr := collectAutomationEvents(stream)
	if runErr == nil && ctx.Err() == nil {
		text = e.ensureStructured(ctx, env, automation, text)
	}
	out := env.execution(text)
	if nodeID != "" {
		out.NodeID = nodeID
	}
	out.SourceHash = env.sources.sum()
	out.Page = runPage(automation.Trigger, env.sources.firstPage())
	// "Notify on change" compares what changed; when the values and sources
	// can't tell, the run's model, still loaded, judges (#204).
	if prev, ok := automations.PreviousFrom(ctx); ok && runErr == nil && ctx.Err() == nil && automations.NeedsJudgment(automation.Notification, prev, out) {
		out.Change = e.judgeChange(ctx, env, automation, prev.Text, out.Text)
	}
	out.Skipped = env.skippedTools()
	out.Acted = env.actedCalls()
	// The answer is said at the door, for an automation that speaks (#667).
	if automation.Say != nil && runErr == nil && ctx.Err() == nil {
		said, err := e.app.sayAtDoor(ctx, automation, out.Text)
		if err != nil {
			runErr = fmt.Errorf("couldn't say the answer at the door: %w", err)
		} else if said != "" {
			out.Acted = append(out.Acted, automations.Action{Tool: automation.Say.Connector + ".say", Args: map[string]string{"text": said}}.Describe())
			// Then it talks with them, and the visit is the result (#667).
			if automation.Say.Listen {
				visit, err := e.app.talkAtDoor(ctx, automation, modelID, said)
				switch {
				case visit != "" && err != nil:
					out.Text = visit + "\n\n" + fmt.Sprintf("The conversation stopped: %v", err)
				case visit != "":
					out.Text = visit
				case err != nil:
					runErr = fmt.Errorf("couldn't hear the visitor: %w", err)
				}
			}
		}
	}
	e.reportSkipped(ctx, automation, out.Skipped)
	if runErr == nil && ctx.Err() == nil && automations.ChangeNote(ctx) == "" {
		if page := unopenedPage(automation, env.sources.firstPage()); page != "" {
			e.reportUnopened(ctx, automation, page)
		}
	}
	return out, runErr
}

// pageTools open a web page for a run to read.
var pageTools = []string{"internet.open", "browser.open"}

var promptURL = regexp.MustCompile(`https?://[^\s"'<>)\]]+`)

// unopenedPage is the page an automation's prompt names that its run never
// opened, though it may: its model answered without reading it, as a small
// model that can't use tools does (#1269). "" when it opened a page, names
// none, or may not open one.
func unopenedPage(automation automations.Automation, opened string) string {
	if opened != "" {
		return ""
	}
	may := false
	for _, id := range pageTools {
		if slices.Contains(automation.Tools, id) {
			may = true
		}
	}
	if !may {
		return ""
	}
	return strings.TrimRight(promptURL.FindString(automation.Prompt), ".,;:!?")
}

// reportUnopened tells the person a run answered without reading the page
// it was about, so its result may be made up (#1269).
func (e automationExecutor) reportUnopened(ctx context.Context, automation automations.Automation, page string) {
	if e.app.Notifications == nil {
		return
	}
	m := notice("pageNotOpenedUnnamed", map[string]any{"page": page}, "pageNotOpenedBody", nil)
	if name := strings.TrimSpace(automation.Name); name != "" {
		m = notice("pageNotOpened", map[string]any{"automation": name, "page": page}, "pageNotOpenedBody", nil)
	}
	_, _ = e.app.Notifications.Notify(context.WithoutCancel(ctx), gjallarhorn.Request{
		SourceType: "automation",
		SourceID:   automation.ID,
		Category:   gjallarhorn.CategoryAutomation,
		Severity:   gjallarhorn.SeverityWarning,
		Message:    m,
		Link:       "/automations?id=" + automation.ID,
		DedupeKey:  "automation.unopened:" + automation.ID,
		Channels:   []string{"desktop"},
	})
}

// reportSkipped tells the person which actions a run skipped because nobody
// approved them, and where to approve them for later runs (spec §59).
func (e automationExecutor) reportSkipped(ctx context.Context, automation automations.Automation, skipped []string) {
	if len(skipped) == 0 || e.app.Notifications == nil {
		return
	}
	names := make([]string, 0, len(skipped))
	for _, id := range skipped {
		if def, ok := tools.Lookup(id); ok {
			names = append(names, def.Name)
		} else {
			names = append(names, id)
		}
	}
	tools := strings.Join(names, ", ")
	m := notice("toolsSkippedUnnamed", map[string]any{"tools": tools}, "toolsSkippedBody", nil)
	if name := strings.TrimSpace(automation.Name); name != "" {
		m = notice("toolsSkipped", map[string]any{"automation": name, "tools": tools}, "toolsSkippedBody", nil)
	}
	_, _ = e.app.Notifications.Notify(context.WithoutCancel(ctx), gjallarhorn.Request{
		SourceType: "automation",
		SourceID:   automation.ID,
		Category:   gjallarhorn.CategoryApproval,
		Severity:   gjallarhorn.SeverityWarning,
		Message:    m,
		Link:       "/automations?id=" + automation.ID,
		DedupeKey:  "automation.skipped:" + automation.ID,
		Channels:   []string{"desktop"},
	})
}

type automationEnv struct {
	base    *chatExecEnv
	granted []string
	// actions are the calls the automation may make on its own, even from
	// what a trigger delivered (#586).
	actions []automations.Action
	// fromTrigger is a run started by what a trigger delivered: a page,
	// posts, files, or a webhook's body, written by someone else (#204).
	fromTrigger bool
	// sources fingerprints what the run's read-only tools returned (#204).
	sources sourceRecorder

	mu      sync.Mutex
	skipped []string
	// acted are the allowed actions the run took, in words.
	acted []string
}

// ReferenceMaterial and TurnInstructions give a scheduled run the same
// knowledge and memories a chat turn gets.
func (e *automationEnv) ReferenceMaterial(ctx context.Context, prompt string) string {
	return e.base.ReferenceMaterial(ctx, prompt)
}

func (e *automationEnv) TurnInstructions(ctx context.Context, prompt string) string {
	return e.base.TurnInstructions(ctx, prompt)
}

// TurnImages are the pictures the run is shown: its cameras' (#667).
func (e *automationEnv) TurnImages() []string {
	return e.base.TurnImages()
}

func (e *automationEnv) skippedTools() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.skipped...)
}

func (e *automationEnv) Generate(ctx context.Context, role string, messages []pluginapi.ChatMessage) (<-chan pluginapi.ChatChunk, error) {
	return e.base.Generate(ctx, role, messages)
}

func (e *automationEnv) ExecuteTool(ctx context.Context, toolID string, args map[string]any) (map[string]any, error) {
	if e.base.app != nil && e.base.app.Tools != nil && e.base.app.Tools.IsDisabled(toolID) {
		return nil, fmt.Errorf("tool %q is disabled", toolID)
	}
	allowed := automations.Allows(e.actions, toolID, args)
	policy, err := tools.UnattendedPolicy(e.base.profile, withActionTools(e.granted, e.actions), toolID)
	// A tool the automation has only for its actions runs only as one of
	// them (#586).
	if err == nil && !allowed && !slices.Contains(e.granted, toolID) && slices.Contains(automations.ActionTools(e.actions), toolID) {
		err = tools.ErrNeedsApproval
	}
	// Someone else's words can't steer a tool that changes things outside
	// Toskar, even one approved for this automation (#204), unless the call
	// is one of the automation's actions, which the person chose (#586).
	if err == nil && e.fromTrigger && !allowed {
		if def, ok := tools.Lookup(toolID); !ok || !tools.Contained(def.Risk) {
			err = tools.ErrNeedsApproval
		}
	}
	if errors.Is(err, tools.ErrNeedsApproval) {
		// Skip and report; never ask or widen with nobody watching (§59).
		e.mu.Lock()
		if !slices.Contains(e.skipped, toolID) {
			e.skipped = append(e.skipped, toolID)
		}
		e.mu.Unlock()
		return nil, fmt.Errorf("skipped: %s needs the person's approval for this automation; continue without it", toolID)
	}
	if err != nil {
		return nil, err
	}
	result, err := e.base.app.Tools.Execute(ctx, toolID, args, policy, "scheduled automation", map[string]any{
		"automation_id": e.base.taskID,
	})
	// A command that always asks, with no one here to answer (#1017).
	if errors.Is(err, tools.ErrNeedsApproval) {
		e.mu.Lock()
		if !slices.Contains(e.skipped, toolID) {
			e.skipped = append(e.skipped, toolID)
		}
		e.mu.Unlock()
		return nil, fmt.Errorf("skipped: %w; continue without it", err)
	}
	if err == nil {
		e.sources.add(toolID, args, result)
		if allowed {
			if def, ok := tools.Lookup(toolID); ok && !tools.Contained(def.Risk) {
				e.mu.Lock()
				e.acted = append(e.acted, automations.Action{Tool: toolID, Args: actionArgs(args)}.Describe())
				e.mu.Unlock()
			}
		}
	}
	return result, err
}

func (e *automationEnv) actedCalls() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.acted...)
}

// actionsNote tells the run which calls it may make on its own (#586), so
// it takes them when the task calls for them and doesn't try others.
func actionsNote(actions []automations.Action) string {
	if len(actions) == 0 {
		return ""
	}
	lines := make([]string, len(actions))
	for i, a := range actions {
		lines[i] = "- " + a.Describe()
	}
	return "The person allowed this automation to make these calls on its own, when the task calls for them:\n" + strings.Join(lines, "\n") +
		"\nOther calls that change things are skipped. Say in the result which of these you made."
}

// withActionTools is the tools granted to an automation and those its
// actions name, which the model needs to see to take them (#586).
func withActionTools(granted []string, actions []automations.Action) []string {
	out := append([]string(nil), granted...)
	for _, id := range automations.ActionTools(actions) {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// actionArgs are a call's arguments as text, for the run's record.
func actionArgs(args map[string]any) map[string]string {
	out := map[string]string{}
	for k, v := range args {
		if v != nil {
			out[k] = strings.TrimSpace(fmt.Sprint(v))
		}
	}
	return out
}

func (e *automationEnv) Emit(eventType string, payload map[string]any) {
	e.base.Emit(eventType, payload)
}

func (e *automationEnv) NodeForRole(role string) (string, error) {
	return e.base.NodeForRole(role)
}

func (e *automationEnv) execution(text string) automations.Execution {
	return automations.Execution{Text: text, ModelID: e.base.modelID()}
}

// releaseModels returns every model this run started on this computer to the
// normal idle policy. It does not stop a model the user already had loaded.
func (e *automationEnv) releaseModels() {
	if e.base == nil || e.base.app == nil {
		return
	}
	e.base.mu.Lock()
	var models []string
	for role, modelID := range e.base.roleModels {
		if modelID == "" || !e.base.app.modelOnThisComputer(e.base.roleNodes[role]) {
			continue
		}
		models = append(models, modelID)
	}
	e.base.mu.Unlock()
	seen := map[string]struct{}{}
	for _, id := range models {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		e.base.app.markModelUsed(id)
	}
}

func (a *App) modelOnThisComputer(nodeID string) bool {
	if nodeID == "" || nodeID == "local" {
		return true
	}
	if a == nil || a.Config == nil {
		return false
	}
	return nodeID == a.Config.Get().NodeID
}

func collectAutomationEvents(stream <-chan pluginapi.OrchestrationEvent) (text, nodeID string, err error) {
	var b strings.Builder
	for evt := range stream {
		if evt.NodeID != "" {
			nodeID = evt.NodeID
		}
		if evt.Error != "" {
			err = fmt.Errorf("%s", evt.Error)
		}
		if evt.Content != "" && evt.Done {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(evt.Content)
		}
	}
	return strings.TrimSpace(b.String()), nodeID, err
}

// runNote is what a trigger found, or, for a run it didn't start, such as
// Run now, what it watches (#460).
func runNote(ctx context.Context, automation automations.Automation) string {
	if note := automations.ChangeNote(ctx); note != "" {
		return note
	}
	return automations.WatchNote(automation.Trigger)
}

// withChangeNote adds what a trigger found to a run's prompt (#204).
func withChangeNote(prompt, note string) string {
	if note == "" {
		return prompt
	}
	return prompt + "\n\n" + note
}
