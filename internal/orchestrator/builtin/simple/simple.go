package simple

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/internal/contextusage"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/internal/mimir"
	"github.com/yeixio/toskar-core/internal/structured"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/internal/untrusted"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

const id = "simple"

// maxToolCalls is the Balanced budget's cap; huginn.Budget sets each turn's.
const maxToolCalls = 10

// EventEffort reports the effort a turn ran at, after Auto is resolved.
const EventEffort = "chat.effort"

// stoppedNotes is the partial answer kept when a plan is stopped.
func stoppedNotes(notes string) string {
	body := strings.TrimSpace(strings.TrimPrefix(notes, "Notes from each part of the request:"))
	return "_Stopped before the answer was written. Here is what was found so far._\n\n" + body
}

// Orchestrator runs single-model chat with optional tools.
type Orchestrator struct{}

func New() *Orchestrator { return &Orchestrator{} }

func (o *Orchestrator) ID() string          { return id }
func (o *Orchestrator) DisplayName() string { return "Simple" }

func (o *Orchestrator) Capabilities() pluginapi.OrchestratorCapabilities {
	return pluginapi.OrchestratorCapabilities{
		SupportsTools: true,
		SupportsTeam:  false,
		Roles:         []string{"assistant", "worker"},
	}
}

func (o *Orchestrator) ValidateProfile(ctx context.Context, profile contracts.AIProfile) error {
	if len(profile.Roles) == 0 {
		return fmt.Errorf("simple orchestrator requires at least one role")
	}
	hasModel := false
	for _, r := range profile.Roles {
		if r.ModelID != "" {
			hasModel = true
			break
		}
	}
	if !hasModel {
		return fmt.Errorf("simple orchestrator requires a model assignment")
	}
	return nil
}

func (o *Orchestrator) Run(
	ctx context.Context,
	task contracts.Task,
	profile contracts.AIProfile,
	env pluginapi.ExecutionEnvironment,
) (<-chan pluginapi.OrchestrationEvent, error) {
	role := PickRole(profile.Roles)
	ch := make(chan pluginapi.OrchestrationEvent, 8)
	go func() {
		defer close(ch)
		ch <- pluginapi.OrchestrationEvent{Type: "agent.started", Role: role}

		instructions := "Format answers in Markdown with short paragraphs and lists. Do not wrap the whole answer in a code fence. " +
			"Link only to addresses from the reference material, tool results, or the conversation; otherwise name the site instead of guessing a page's address, since remembered addresses are often wrong."
		reference := referenceMaterial(ctx, env, task.Prompt)
		// The effort the user chose, or Auto's pick for this kind of request,
		// sets how much planning, reading, and checking the turn gets (§15).
		plan, hasParts := huginn.MakePlan(task.Prompt)
		kind := huginn.Classify(task.Prompt)
		if (hasParts || reference != "") && kind == huginn.Chat {
			// A request with several parts, or one answered from the user's
			// files or knowledge, is not a quick question.
			kind = huginn.Research
		}
		// The profile's own controls apply on top of the effort's budget (§40).
		budget := huginn.BudgetFor(huginn.EffortFrom(ctx), kind).With(profile.Orchestration)
		// An answer that must be JSON (§27) is one constrained reply: no
		// plan, tool calls, file, or figure check, which would each need
		// output of another shape. A web look-up still runs first.
		jsonOnly := len(structured.SchemaFrom(ctx)) > 0
		// The profile's strategy (§20). A quick question is answered
		// directly whatever the strategy: escalate only when it helps (§6).
		strat := strategyOf(profile)
		escalate := kind != huginn.Chat && !jsonOnly
		switch {
		case strat.single:
			budget.Plan = false
		case strat.planned:
			budget.Plan = true
		case strat.team && escalate:
			budget.Plan, budget.Verify = true, true
			budget.Corrections = max(budget.Corrections, 1)
		}
		// A message with pictures is answered by the model that sees them,
		// in one reply: plan steps and checks would read only text (#191).
		var images []string
		if ti, ok := env.(pluginapi.TurnImages); ok {
			images = ti.TurnImages()
		}
		if jsonOnly || len(images) > 0 {
			budget.Plan, budget.Verify = false, false
		}
		env.Emit(EventEffort, map[string]any{"effort": string(budget.Effort), "chosen": string(huginn.EffortFrom(ctx))})
		// Offer only the tools this request needs (spec §16). The profile
		// still decides what is allowed; this decides what is shown.
		offered := huginn.ToolsFor(kind, task.Prompt, enabledIDs(profile))
		// An attached audio file is transcribed, and an attached image can
		// be edited, when the profile allows it, whatever the message says
		// ("summarize this", "make it brighter").
		for _, id := range []string{"speech.transcribe", "image.edit"} {
			if strings.Contains(reference, "call "+id) && slices.Contains(enabledIDs(profile), id) && !slices.Contains(offered, id) {
				offered = append(offered, id)
			}
		}
		// The profile before tools are narrowed to this request, for a
		// look-up when the answer turns out to send the person off to search.
		webProfile := profile
		profile = offerOnly(profile, offered)
		// A request with several parts is worked through part by part;
		// otherwise a current question is looked up first.
		planned := false
		// lookedUp records that the web was read for this turn.
		lookedUp := false
		// Planning set to Always, and the Team strategy, ask the planner to
		// split a request that has no obvious parts (§12). Team keeps a plan
		// of one part, so a worker drafts and the answer is written from it.
		if !hasParts && budget.Plan && strat.alwaysPlan && escalate {
			limit := budget.MaxWorkers
			if limit == 0 {
				limit = defaultTeamParts
			}
			if p, ok := askPlanner(ctx, env, ch, plannerRole(profile, role), task.Prompt, limit); ok && (len(p.Steps) > 1 || strat.team) {
				plan, hasParts = p, true
			}
		}
		if hasParts && budget.Plan {
			if budget.Sequential {
				plan.Parallel = false
			}
			if budget.MaxWorkers > 0 && len(plan.Steps) > budget.MaxWorkers {
				plan.Steps = plan.Steps[:budget.MaxWorkers]
			}
			notes := runPlan(ctx, env, ch, profile, role, plan, task.Prompt, reference, budget.Pages)
			if ctx.Err() != nil {
				// Stopped while working through the parts: keep what is done (§67).
				if notes != "" {
					ch <- pluginapi.OrchestrationEvent{Type: "agent.message", Role: role, Content: stoppedNotes(notes)}
				}
				ch <- pluginapi.OrchestrationEvent{Type: "agent.error", Role: role, Error: ctx.Err().Error(), Done: true}
				return
			}
			if notes != "" {
				reference = joinReference(reference, notes)
				instructions += "\n" + planGuidance
				if plan.NeedsWeb && webAllowed(profile) {
					profile = withoutWeb(profile)
					lookedUp = true
				}
				planned = true
			}
		}
		// A picture or clip to make isn't looked up on the web first.
		_, wantsMedia := askedForMedia(task.Prompt, reference)
		if !planned && !wantsMedia && len(images) == 0 {
			// A message about a connected service is answered from that
			// service, not the web.
			if found, ok := serviceFirst(ctx, env, profile); ok {
				reference = joinReference(reference, found)
				instructions += "\n" + serviceGuidance
				profile = withoutFetched(profile)
			} else if found, ok := lookUpFirst(ctx, env, profile, role, task.Prompt, budget.Pages); ok {
				reference = joinReference(reference, found)
				instructions += "\n" + lookupGuidance
				profile = withoutWeb(profile)
				lookedUp = true
			}
		}
		if climateQuestion(task.Prompt) {
			instructions += "\n" + climateGuidance
		}
		// Deliberate compares drafts by their final answers (#459); small talk
		// isn't deliberated, and neither is an answer that must be JSON, one
		// about pictures, or a Team profile's, which has its own reviewer.
		deliberate := deliberating(profile) && !jsonOnly && len(images) == 0 && !strat.team && !huginn.SmallTalk(task.Prompt)
		if deliberate {
			instructions += "\n" + draftGuidance
		}
		reference = transcribeFirst(ctx, env, profile, reference)
		// evidence is what the answer may draw figures from, for the check.
		evidence := reference
		toolPrompt := tools.PromptFor(profile)
		if jsonOnly || profile.ModelCallsNoTools {
			toolPrompt = ""
		}
		sys := instructions
		if toolPrompt != "" {
			sys += "\n" + toolPrompt
		}
		// plainSys is the same prompt without tools, for a retry when the
		// model describes tools instead of answering. The context gauge counts
		// it as instructions, so turn guidance (personalization, memories, the
		// guide) is counted too (#230).
		plainSys := instructions
		if extra := turnGuidance(ctx, env, task.Prompt); extra != "" {
			sys = extra + "\n\n" + sys
			plainSys = extra + "\n\n" + plainSys
		}
		// count is the answering model's tokenizer, or an estimate (§66).
		count := tokenCounter(ctx, env, role)
		userMsg := withReference(task.Prompt, reference)
		messages := []pluginapi.ChatMessage{{Role: "system", Content: sys}}
		prior := priorMessages(ctx, env, count, userMsg, sys, profile.Orchestration.ContextShare)
		messages = append(messages, prior...)
		messages = append(messages, pluginapi.ChatMessage{Role: "user", Content: userMsg, Images: images})

		var metrics *pluginapi.GenerationMetrics
		var usage contextusage.Usage
		// A model that cannot call tools gets the look-ups above as
		// reference, and no tool loop.
		toolsOn := len(tools.Enabled(profile, nil)) > 0 && !profile.ModelCallsNoTools
		nodeID, _ := env.NodeForRole(role)
		// The calls that write the answer take the caller's temperature
		// and token cap, when it sent them (#70).
		answerCtx := ctx
		if o := pluginapi.AnswerOptionsFrom(ctx); o != (pluginapi.GenerateOptions{}) {
			answerCtx = pluginapi.WithGenerateOptions(ctx, o)
		}

		if jsonOnly {
			content, m, err := generate(answerCtx, env, role, messages, true)
			if err != nil {
				ch <- pluginapi.OrchestrationEvent{Type: "agent.error", Role: role, Error: err.Error(), Done: true}
				return
			}
			promptTokens := 0
			if m != nil {
				promptTokens = m.PromptTokens
			}
			streamText(ch, role, nodeID, content, m, contextusage.Measure(count, plainSys, toolPrompt, messages, promptTokens))
			return
		}
		// A picture or clip the message asks for is made by Toskar, so a
		// model that doesn't pick the tool still makes it.
		if !jsonOnly {
			if reply, m, made := makeMediaFirst(ctx, env, webProfile, role, messages, task.Prompt, reference); made {
				promptTokens := 0
				if m != nil {
					promptTokens = m.PromptTokens
				}
				streamText(ch, role, nodeID, reply, m, contextusage.Measure(count, plainSys, toolPrompt, messages, promptTokens))
				return
			}
		}
		if reply, m, made := makeFileFirst(ctx, env, profile, role, messages, task.Prompt); made {
			promptTokens := 0
			if m != nil {
				promptTokens = m.PromptTokens
			}
			streamText(ch, role, nodeID, reply, m, contextusage.Measure(count, plainSys, toolPrompt, messages, promptTokens))
			return
		}
		calls := 0
		malformed := 0
		// changed records that a tool that changes things ran.
		changed := false
		retriedPlain := false
		// retriedEmpty records that a reply with only a call that couldn't
		// run was asked for again in plain text.
		retriedEmpty := false
		retriedLookup := false
		// retriedRecall records that a personal question's answer was asked
		// for again from the chat.
		retriedRecall := false
		retriedFile := false
		triedMedia := false
		// retriedHandover records that an answer handing over a destructive
		// command was asked for again; declined, that the person said no to
		// a tool (#557).
		retriedHandover := false
		declined := false

		for {
			content, m, err := generate(answerCtx, env, role, messages, true)
			if err != nil {
				if content != "" {
					ch <- pluginapi.OrchestrationEvent{Type: "agent.message", Role: role, NodeID: nodeID, Content: content}
				}
				ch <- pluginapi.OrchestrationEvent{Type: "agent.error", Role: role, Error: err.Error(), Done: true}
				return
			}
			if m != nil {
				metrics = m
			}
			promptTokens := 0
			if m != nil {
				promptTokens = m.PromptTokens
			}
			usage = contextusage.Measure(count, plainSys, toolPrompt, messages, promptTokens)

			parsed := tools.ParseModelOutput(content)
			if parsed.Sanitized {
				env.Emit(events.ToolParsed, map[string]any{"sanitized": true})
			}
			if parsed.Call == nil && toolsOn {
				// Small models sometimes write the call out instead of
				// sending it; take it when it names an offered tool.
				if call, ok := looseToolCall(parsed.Text, profile); ok {
					parsed.Call, parsed.Text = call, ""
				}
			}
			if parsed.Call != nil {
				parsed.Call.ID = tools.Canonical(parsed.Call.ID)
			}
			if parsed.Call != nil && !toolEnabled(profile, parsed.Call.ID) && malformed < 2 {
				// A tool that was not offered is refused, whatever the profile
				// allows: the model cannot widen its own tools (spec §17).
				// That holds when no tools were offered at all.
				malformed++
				env.Emit(events.ToolFailed, map[string]any{"tool_id": parsed.Call.ID, "kind": tools.ErrKindNotOffered, "error": tools.ErrNotOffered.Error()})
				messages = append(messages,
					pluginapi.ChatMessage{Role: "assistant", Content: content},
					pluginapi.ChatMessage{Role: "user", Content: "The tool " + parsed.Call.ID + " is not available for this request. Use one of the listed tools, or answer in plain text."},
				)
				continue
			}
			if parsed.Call != nil && toolsOn && toolEnabled(profile, parsed.Call.ID) {
				if calls >= budget.MaxToolCalls {
					streamText(ch, role, nodeID, "I stopped after "+fmt.Sprint(budget.MaxToolCalls)+" tool calls so this request would not loop.", metrics, usage)
					return
				}
				calls++
				env.Emit(events.ToolParsed, map[string]any{
					"accepted": true,
					"tool_id":  parsed.Call.ID,
					"format":   parsed.Format,
				})
				// Text written before the call ("Let me find that for you") is
				// not part of the answer; the tool's own step says what's
				// happening (#280).
				result, err := env.ExecuteTool(ctx, parsed.Call.ID, parsed.Call.Args)
				if err == nil {
					if def, ok := tools.Lookup(parsed.Call.ID); ok && def.Risk != tools.RiskRead {
						changed = true
					}
				}
				var resultNote string
				if err != nil {
					payload, _ := json.Marshal(map[string]any{"ok": false, "error": publicToolError(err)})
					resultNote = "The tool failed. Details for you, not for the user:\n" + string(payload)
					if declinedByPerson(err) {
						declined = true
						resultNote += "\n" + declinedNote
					}
				} else {
					raw, _ := json.Marshal(map[string]any{"ok": true, "result": untrusted.Value(result)})
					resultNote = "Tool result for you, not for the user. " + untrustedNote + "\n" + string(raw)
				}
				followUp := answerAfterTools
				if err == nil && parsed.Call.ID == "internet.search" && calls < budget.MaxToolCalls {
					if page, opened := followLiveSearch(ctx, env, profile, task.Prompt, parsed.Call.Args, result); opened {
						calls++
						raw, _ := json.Marshal(map[string]any{"ok": true, "result": untrusted.Value(page)})
						resultNote += "\n\nPage for you, not for the user. " + untrustedNote + "\n" + string(raw)
						followUp = answerFromPage
					}
				}
				if err == nil {
					evidence = joinReference(evidence, resultNote)
				}
				messages = append(messages,
					pluginapi.ChatMessage{Role: "assistant", Content: content},
					pluginapi.ChatMessage{Role: "user", Content: resultNote + followUp},
				)
				continue
			}
			if toolsOn && parsed.Malformed && malformed < 2 && calls < budget.MaxToolCalls {
				malformed++
				env.Emit(events.ToolParsed, map[string]any{"accepted": false, "format": "rejected", "error": "invalid tool call"})
				env.Emit(events.ToolFailed, map[string]any{"malformed": true, "error": "invalid tool call"})
				messages = append(messages,
					pluginapi.ChatMessage{Role: "assistant", Content: content},
					pluginapi.ChatMessage{Role: "user", Content: "That tool call was not valid and was not run. Reply with one JSON object {\"tool_call\":{\"id\":\"...\",\"args\":{}}} using a listed tool, or answer in plain text."},
				)
				continue
			}
			// An answer that talks about tools instead of using them is not an
			// answer (spec §24, tool success). Ask once more without tools,
			// from the material already gathered.
			if toolsOn && !retriedPlain && narratesTools(parsed.Text) {
				retriedPlain = true
				toolsOn = false
				env.Emit(events.ToolFailed, map[string]any{"narrated": true, "error": "described tools instead of answering"})
				messages[0] = pluginapi.ChatMessage{Role: "system", Content: plainSys}
				continue
			}
			// An answer that tells the person to make the file themselves,
			// when the profile allows files.create, is asked for once more
			// with files.create offered, though the message didn't ask for a
			// file in so many words (#281).
			// A model that won't make a picture the message is about, or
			// sends the person to another site for one, gets it made: a
			// backstop for wordings the check before the loop misses.
			if !triedMedia && !changed && !jsonOnly {
				triedMedia = true
				if reply, m, made := makeMediaAfterRefusal(ctx, env, webProfile, role, messages, task.Prompt, parsed.Text); made {
					promptTokens := 0
					if m != nil {
						promptTokens = m.PromptTokens
					}
					streamText(ch, role, nodeID, reply, m, contextusage.Measure(count, plainSys, toolPrompt, messages, promptTokens))
					return
				}
			}
			if !retriedFile && !changed && !jsonOnly && !profile.ModelCallsNoTools && calls < budget.MaxToolCalls && toolEnabled(webProfile, "files.create") && huginn.TellsToMakeFile(parsed.Text) {
				retriedFile = true
				env.Emit(events.ToolFailed, map[string]any{"deflected": true, "error": "told the person to make the file"})
				if !toolEnabled(profile, "files.create") {
					offered = append(offered, "files.create")
					profile = offerOnly(webProfile, offered)
					toolPrompt = tools.PromptFor(profile)
					messages[0] = pluginapi.ChatMessage{Role: "system", Content: plainSys + "\n" + toolPrompt}
				}
				toolsOn = true
				messages = append(messages,
					pluginapi.ChatMessage{Role: "assistant", Content: content},
					pluginapi.ChatMessage{Role: "user", Content: makeTheFile},
				)
				continue
			}
			// A question about the person's own life ("Where does my sister
			// live?") isn't on the web. An answer that says it doesn't know,
			// or deflects, is asked for once more from the chat, whose summary
			// a small model can pass over (#834).
			if huginn.AboutThemselves(task.Prompt) {
				if !retriedRecall && !jsonOnly && len(prior) > 0 && (huginn.Deflects(parsed.Text) || saysUnknown(parsed.Text)) {
					retriedRecall = true
					messages = append(messages,
						pluginapi.ChatMessage{Role: "assistant", Content: content},
						pluginapi.ChatMessage{Role: "user", Content: recallAgain},
					)
					continue
				}
			} else if !lookedUp && !retriedLookup && !jsonOnly && !huginn.SmallTalk(task.Prompt) && huginn.Deflects(parsed.Text) && webAllowed(webProfile) {
				// An answer that sends the person off to search, or pretends
				// to, is looked up and written again from what the web says
				// (§21). Core tells the model not to; a small model still does.
				// Small talk ("as an AI, I don't have feelings") is not looked
				// up.
				retriedLookup = true
				if found, ok := lookUp(ctx, env, webProfile, searchQuery(ctx, env, role, task.Prompt), task.Prompt, budget.Pages); ok {
					lookedUp = true
					reference = joinReference(reference, found)
					evidence = joinReference(evidence, found)
					toolsOn = false
					rewrite := []pluginapi.ChatMessage{{Role: "system", Content: plainSys + "\n" + lookupGuidance}}
					rewrite = append(rewrite, prior...)
					messages = append(rewrite, pluginapi.ChatMessage{Role: "user", Content: withReference(task.Prompt, reference), Images: images})
					continue
				}
			}
			// Asked to do something destructive, a model that hands the
			// person the command to run themselves sidesteps the approval
			// step (#557). It's asked once more: to use the terminal, so
			// the person is asked to approve, or, with no tool that could
			// do it or after the person said no, to say it wasn't done.
			// A command still in the answer after that is taken out. A
			// question about how to do it keeps its command.
			if !jsonOnly && !changed && huginn.AsksToDo(task.Prompt) && huginn.DestructiveCommand(parsed.Text) != "" {
				if !retriedHandover {
					retriedHandover = true
					env.Emit(events.ToolFailed, map[string]any{"handed_over": true, "error": "gave the person a destructive command to run instead of asking to run it"})
					note := notDoneHere
					if !declined && !profile.ModelCallsNoTools && calls < budget.MaxToolCalls && toolEnabled(webProfile, "terminal") {
						if !toolEnabled(profile, "terminal") {
							offered = append(offered, "terminal")
							profile = offerOnly(webProfile, offered)
							toolPrompt = tools.PromptFor(profile)
							messages[0] = pluginapi.ChatMessage{Role: "system", Content: plainSys + "\n" + toolPrompt}
						}
						toolsOn = true
						note = useTheTerminal
					}
					messages = append(messages,
						pluginapi.ChatMessage{Role: "assistant", Content: content},
						pluginapi.ChatMessage{Role: "user", Content: note},
					)
					continue
				}
				parsed.Text = huginn.WithoutDestructive(parsed.Text)
				env.Emit(EventCommandRemoved, map[string]any{})
			}
			// A reply that is only a call that can't run here is not an
			// answer; it's asked for once more in plain text, and never sent
			// empty.
			if strings.TrimSpace(parsed.Text) == "" && parsed.Call != nil && !retriedEmpty {
				retriedEmpty = true
				messages = append(messages,
					pluginapi.ChatMessage{Role: "assistant", Content: content},
					pluginapi.ChatMessage{Role: "user", Content: "That tool can't be used now. Answer in plain text from what you already have."},
				)
				continue
			}
			answer := parsed.Text
			if deliberate {
				answer = deliberateAnswer(ctx, env, ch, profile, task.Prompt, messages, plainSys, answer, role, nodeID)
			}
			if budget.Verify {
				answer = verifyAnswer(ctx, env, reviewerRole(profile, role), messages, parsed.Text, evidence, task.Prompt, budget.Corrections)
				answer = checkCode(ctx, env, reviewerRole(profile, role), messages, answer, budget.Corrections)
				answer = checkLinks(ctx, env, reviewerRole(profile, role), messages, answer, evidence, budget.Corrections)
				if budget.Effort == huginn.EffortThorough && budget.Corrections > 0 {
					answer = checkConsistency(ctx, env, reviewerRole(profile, role), messages, answer, evidence)
				}
			}
			// The Team strategy's reviewer reads every answer it escalated.
			if strat.team && escalate {
				answer = reviewAnswer(ctx, env, ch, reviewerRole(profile, role), task.Prompt, answer, evidence)
			}
			// The environment's own check comes last, so nothing rewrites
			// what it allowed (#345).
			if c, ok := env.(pluginapi.AnswerCheck); ok && ctx.Err() == nil {
				answer = c.CheckAnswer(ctx, reviewerRole(profile, role), task.Prompt, answer)
			}
			// An answer that says it changed something, when nothing that
			// changes things ran, is called out (§24; found by §64).
			if !changed && huginn.ClaimsAction(answer) {
				env.Emit(EventUnconfirmedAction, map[string]any{})
			}
			streamText(ch, role, nodeID, answer, metrics, usage)
			return
		}
	}()
	return ch, nil
}

func generateText(
	ctx context.Context,
	env pluginapi.ExecutionEnvironment,
	role string,
	messages []pluginapi.ChatMessage,
) (string, *pluginapi.GenerationMetrics, error) {
	return generate(ctx, env, role, messages, false)
}

// EventReasoning says a reasoning model is thinking before it answers
// (#701): state started, then done with ms, how long it thought. For the
// answer itself, done also has text, the thinking, cut at maxReasoningText.
const EventReasoning = "chat.reasoning"

// maxReasoningText is as much of a model's thinking as an answer keeps.
const maxReasoningText = 20000

// generate is generateText; answer says it writes the answer the person
// sees, whose thinking is kept with it.
func generate(
	ctx context.Context,
	env pluginapi.ExecutionEnvironment,
	role string,
	messages []pluginapi.ChatMessage,
	answer bool,
) (string, *pluginapi.GenerationMetrics, error) {
	stream, err := env.Generate(ctx, role, messages)
	if err != nil {
		return "", nil, err
	}
	var b strings.Builder
	var metrics *pluginapi.GenerationMetrics
	var thinking strings.Builder
	var thinkingSince time.Time
	thought := false
	// The thinking ends when the answer starts, or with the stream.
	endThinking := func() {
		if thinkingSince.IsZero() || thought {
			return
		}
		thought = true
		payload := map[string]any{"state": "done", "ms": time.Since(thinkingSince).Milliseconds(), "role": role}
		if answer {
			payload["answer"] = true
			if text := strings.TrimSpace(thinking.String()); text != "" {
				if r := []rune(text); len(r) > maxReasoningText {
					text = string(r[:maxReasoningText]) + "…"
				}
				payload["text"] = text
			}
		}
		env.Emit(EventReasoning, payload)
	}
	for chunk := range stream {
		if chunk.Error != "" {
			endThinking()
			return b.String(), metrics, fmt.Errorf("%s", chunk.Error)
		}
		if chunk.Metrics != nil {
			metrics = chunk.Metrics
		}
		if chunk.Reasoning != "" && !thought {
			if thinkingSince.IsZero() {
				thinkingSince = time.Now()
				env.Emit(EventReasoning, map[string]any{"state": "started", "role": role})
			}
			if thinking.Len() <= maxReasoningText*4 {
				thinking.WriteString(chunk.Reasoning)
			}
		}
		if chunk.Content != "" {
			endThinking()
			b.WriteString(chunk.Content)
		}
		if chunk.Done {
			break
		}
	}
	endThinking()
	return b.String(), metrics, nil
}

func streamText(ch chan<- pluginapi.OrchestrationEvent, role, nodeID, content string, metrics *pluginapi.GenerationMetrics, usage contextusage.Usage) {
	visible := tools.VisibleText(content)
	if visible != "" {
		ch <- pluginapi.OrchestrationEvent{Type: "agent.message", Role: role, NodeID: nodeID, Content: visible}
	}
	ch <- pluginapi.OrchestrationEvent{
		Type:    "agent.completed",
		Role:    role,
		NodeID:  nodeID,
		Content: visible,
		Metrics: metrics,
		Payload: map[string]any{"context": usage.Map()},
		Done:    true,
	}
}

// untrustedNote tells the model that retrieved content is data (§58).
const untrustedNote = mimir.UntrustedNote

// referenceSource is implemented by environments that retrieve reference
// material, such as connected knowledge, for a turn.
type referenceSource interface {
	ReferenceMaterial(ctx context.Context, prompt string) string
}

func referenceMaterial(ctx context.Context, env pluginapi.ExecutionEnvironment, prompt string) string {
	src, ok := env.(referenceSource)
	if !ok {
		return ""
	}
	return strings.TrimSpace(src.ReferenceMaterial(ctx, prompt))
}

func withReference(prompt, reference string) string { return mimir.WithReference(prompt, reference) }

// turnInstructions is implemented by environments that add instructions for
// one turn: a specialized AI's system instructions and connected knowledge.
type turnInstructions interface {
	TurnInstructions(ctx context.Context, prompt string) string
}

func turnGuidance(ctx context.Context, env pluginapi.ExecutionEnvironment, prompt string) string {
	src, ok := env.(turnInstructions)
	if !ok {
		return ""
	}
	return strings.TrimSpace(src.TurnInstructions(ctx, prompt))
}

type conversationMemory interface {
	PriorMessages(ctx context.Context) []pluginapi.ChatMessage
	ContextLimit() int
}

type tokenCounting interface {
	TokenCounter(ctx context.Context, role string) contextusage.Counter
}

// tokenCounter is the tokenizer of the model a role uses, or nil to estimate.
func tokenCounter(ctx context.Context, env pluginapi.ExecutionEnvironment, role string) contextusage.Counter {
	if src, ok := env.(tokenCounting); ok {
		return src.TokenCounter(ctx, role)
	}
	return nil
}

// priorMessages fits earlier messages into the room the model's window
// leaves, in tokens. share, when set by the profile (§40), caps earlier
// messages at that part of the window.
func priorMessages(ctx context.Context, env pluginapi.ExecutionEnvironment, count contextusage.Counter, prompt, reserved string, share float64) []pluginapi.ChatMessage {
	src, ok := env.(conversationMemory)
	if !ok {
		return nil
	}
	limit := src.ContextLimit()
	if limit <= 0 {
		limit = contextusage.DefaultWindow
	}
	// The answer's room: more for a model that thinks first (#840).
	reply := contextusage.ReplyReserve(limit, 0, false)
	if r, ok := env.(interface{ ReplyReserve(context.Context) int }); ok {
		reply = r.ReplyReserve(ctx)
	}
	room := contextusage.Room(limit, reply, count, reserved, prompt)
	if share > 0 {
		room = min(room, int(share*float64(limit)))
	}
	return contextusage.FitPrior(src.PriorMessages(ctx), room, count)
}

func publicToolError(err error) string {
	msg := strings.TrimSpace(err.Error())
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	if len(msg) > 240 {
		msg = msg[:240]
	}
	return msg
}

func parseToolCall(content string) (toolID string, args map[string]any, ok bool) {
	parsed := tools.ParseModelOutput(content)
	if parsed.Call == nil {
		return "", nil, false
	}
	return parsed.Call.ID, parsed.Call.Args, true
}

func userVisibleReply(content string) string {
	return tools.VisibleText(content)
}

// PickRole is the role that answers: the first required one, else the first.
func PickRole(roles []contracts.ModelRole) string {
	for _, r := range roles {
		if r.Required && r.Role != "" {
			return r.Role
		}
	}
	if len(roles) > 0 {
		return roles[0].Role
	}
	return "assistant"
}

// declinedByPerson reports a tool call the person declined when asked.
func declinedByPerson(err error) bool {
	return strings.Contains(err.Error(), "denied by user")
}
