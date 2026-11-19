package app

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/connectors"
	"github.com/yeixio/toskar-core/internal/egress"
	"github.com/yeixio/toskar-core/internal/locale"
	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/internal/voice"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Talking with a visitor (#667): after the first words, an automation whose
// Say listens hears the visitor through the doorbell's microphone, answers
// through its speaker, and takes turns until it knows who came and why, or
// they leave. It doesn't listen while it speaks, since the microphone hears
// the speaker. The visit is then summed up for the people who live there,
// as the run's result, which the app notifies them with.

// doorEar hears a camera's microphone: connectors.Manager.
type doorEar interface {
	Listen(ctx context.Context, service string) (*connectors.Hearing, error)
}

// doorLine is one thing said in a visit.
type doorLine struct {
	Visitor bool
	Text    string
}

// doorVisit is one conversation at the door.
type doorVisit struct {
	ear        doorEar
	talk       doorTalker
	parts      voice.Parts
	automation automations.Automation
	lang       string
	// reply answers what the visitor just said, given the visit so far.
	reply func(ctx context.Context, lines []doorLine) (string, error)
	now   func() time.Time

	// wait is how long the visitor has to start talking; longest is the
	// longest turn heard; echo is how long after speaking the door isn't
	// heard, while its own words may still be playing.
	wait, longest, echo time.Duration
	// grace is how long the door still listens after it said goodbye, in
	// case the visitor has more to say: a visit ended too quickly for Mike
	// (#667). The hub's person signal is a pulse of a few seconds, not
	// whether someone is still there, so it can't tell when they've left.
	grace time.Duration
	// turns finds when the visitor talks, learning the street's noise over
	// the visit.
	turns *voice.Turns
}

// doneMark ends the visit: the model adds it to its goodbye.
const doneMark = "[done]"

var doneMarkRe = regexp.MustCompile(`(?i)\[done\]`)

// errNoOneThere is a door that heard nothing: the visitor left or didn't
// answer, which ends the visit.
var errNoOneThere = errors.New("nobody answered")

func (v *doorVisit) run(ctx context.Context, greeting string) ([]doorLine, error) {
	if v.now == nil {
		v.now = time.Now
	}
	connector := v.automation.Say.Connector
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	h, err := v.ear.Listen(ctx, connector)
	if err != nil {
		return []doorLine{{Text: greeting}}, err
	}
	lines := []doorLine{{Text: greeting}}
	// The greeting may still be playing as listening starts.
	v.drain(ctx, h, v.echo)
	wait := v.wait
	for turn := 0; turn < automations.MaxVisitTurns; turn++ {
		samples, err := v.hear(ctx, h, wait)
		if errors.Is(err, errNoOneThere) {
			return lines, nil
		}
		if err != nil {
			return lines, err
		}
		heard, err := v.parts.Transcriber.Transcribe(ctx, voice.WAV(samples, connectors.ReolinkListenRate), v.lang)
		if err != nil {
			return lines, fmt.Errorf("couldn't make out what the visitor said: %w", err)
		}
		heard = strings.TrimSpace(heard)
		if heard == "" {
			continue
		}
		lines = append(lines, doorLine{Visitor: true, Text: heard})
		answer, err := v.reply(ctx, lines)
		if err != nil {
			return lines, err
		}
		done := strings.Contains(strings.ToLower(answer), doneMark)
		// In any case, such as [Done], so it's never said aloud (#1339).
		answer = strings.TrimSpace(doneMarkRe.ReplaceAllString(answer, ""))
		said, err := sayAnswer(ctx, v.parts.Speaker, v.talk, v.automation, answer, v.lang, v.now())
		if err != nil {
			return lines, err
		}
		if said != "" {
			lines = append(lines, doorLine{Text: said})
		}
		if said == "" || (done && v.grace <= 0) {
			return lines, nil
		}
		wait = v.wait
		if done {
			// Goodbye said: the visit ends unless they speak again.
			wait = v.grace
		}
		v.drain(ctx, h, v.echo)
	}
	return lines, nil
}

// hear waits for the visitor to talk and returns what they said, as 16-bit
// samples at ReolinkListenRate, once they stop.
// doorTurns hears when a visitor starts and stops talking. A Reolink
// doorbell's microphone is quiet: measured on Mike's (#667), someone talking
// at the door is about -34 dBFS, 20 ms frames mostly 0.02 to 0.05 of full
// scale, over a background near -65 dBFS. Threshold 0.8, a floor of 0.048,
// heard almost none of it, and the visit ended before the visitor had been
// heard; 0.2 is a floor of 0.012, about 20 dB above that background, and
// a noisier street still raises it, to four times its own level.
func doorTurns() *voice.Turns {
	return voice.NewTurns(voice.TurnDetection{Type: "server_vad", Threshold: 0.2, PrefixPaddingMs: 300, SilenceDurationMs: 800})
}

func (v *doorVisit) hear(ctx context.Context, h *connectors.Hearing, wait time.Duration) ([]int16, error) {
	turns := v.turns
	if turns == nil {
		turns = doorTurns()
		v.turns = turns
	}
	// A third of a second before they start, so the first word is whole.
	prefix := connectors.ReolinkListenRate * 3 / 10
	var heard []int16
	speaking := turns.Speaking()
	start := time.NewTimer(wait)
	defer start.Stop()
	longest := time.NewTimer(wait + v.longest)
	defer longest.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-start.C:
			if !speaking {
				return nil, errNoOneThere
			}
		case <-longest.C:
			if speaking {
				return heard, nil
			}
			return nil, errNoOneThere
		case chunk, ok := <-h.Audio:
			if !ok {
				if err := h.Err(); err != nil {
					return nil, err
				}
				return nil, errors.New("the door stopped being heard")
			}
			heard = append(heard, chunk...)
			started, stopped := turns.Feed(voice.Resample(chunk, connectors.ReolinkListenRate, voice.SampleRate))
			if started && !speaking {
				speaking = true
				longest.Reset(v.longest)
			}
			if !speaking && len(heard) > prefix {
				heard = append(heard[:0], heard[len(heard)-prefix:]...)
			}
			if speaking && stopped {
				return heard, nil
			}
		}
	}
}

// drain lets go of what the door heard, and hears for d more without
// keeping it: the door's own words, coming back through its microphone.
func (v *doorVisit) drain(ctx context.Context, h *connectors.Hearing, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			return
		case _, ok := <-h.Audio:
			if !ok {
				return
			}
		}
	}
}

// doorGrace is how long the door listens after saying goodbye.
const doorGrace = 60 * time.Second

// doorTalkStyle is how Toskar talks with a visitor.
const doorTalkStyle = `You're talking with a visitor at the front door, through its camera's speaker; they answer through its microphone. You already greeted them. Find out, politely and briefly, who they are and what they need, one question at a time. Never say whether anyone is home, never offer to open the door or let anyone in, and say nothing about the people who live here. If it's a delivery, ask them to leave it at the door. Call them by a name only once they've said it's theirs: a name they mention may be someone else's, such as who sent a package. Keep each reply to one or two short sentences, with no formatting. When you know who they are and what they want, or they say goodbye or leave, thank them, say you'll pass the message on, and end that reply with ` + doneMark + `.`

// doorSummaryStyle is how the visit is told to the people who live there.
const doorSummaryStyle = `Below is a conversation at the front door between the house's assistant and a visitor. Write a short note for the people who live there: who came, what they wanted, and anything they should do, in at most three short sentences. Say only what the conversation says; if the visitor didn't say who they are or why they came, say so. A name the visitor mentions may be someone else's, such as who sent a package: say whose it is as they did. It's read as a phone notification: plain sentences, with no headings, lists, bold, or notes about yourself, this conversation, or how you were set up.`

// talkAtDoor talks with the visitor an automation greeted, and returns the
// run's result: the visit summed up, then what was said.
func (a *App) talkAtDoor(ctx context.Context, automation automations.Automation, modelID, greeting string) (string, error) {
	if a.Connectors == nil {
		return "", errors.New("hearing a camera isn't available on this computer")
	}
	parts, err := a.voiceParts(ctx)
	if err != nil {
		return "", err
	}
	lang := a.replyLanguage(ctx, "", automation.Prompt, automation.ResponseLanguage).Tag
	// One visit is at most a few minutes.
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	v := &doorVisit{ear: a.Connectors, talk: a.Connectors, parts: parts, automation: automation, lang: lang,
		reply: func(ctx context.Context, lines []doorLine) (string, error) {
			return a.doorAnswer(ctx, automation, modelID, doorTalkStyle, lines)
		},
		wait: 10 * time.Second, longest: 30 * time.Second, echo: time.Second, grace: doorGrace}
	lines, err := v.run(ctx, greeting)
	if a.Egress != nil {
		a.Egress.Add(ctx, egress.Connector, a.Connectors.ServiceName(automation.Say.Connector), "heard the door")
	}
	if len(lines) < 2 {
		// Nobody answered: the greeting is the result, as before.
		if err != nil {
			return "", err
		}
		return "", nil
	}
	summary, sumErr := a.doorAnswer(context.WithoutCancel(ctx), automation, modelID, doorSummaryStyle, nil, transcript(lines, "en"))
	if sumErr != nil {
		summary = ""
	}
	summary = plainSummary(summary)
	result := strings.TrimSpace(summary + "\n\n" + transcript(lines, lang))
	return result, err
}

var (
	listMarkRe = regexp.MustCompile(`^\s*(?:[-*•]|\d+[.)])\s+`)
	inlineMark = regexp.MustCompile("[*_`#]+")
)

// plainSummary is the summary as a phone notification shows it: plain
// sentences on one line. A small model wrote headings, bold, a list, and a
// note about its own settings, for Mike's first visit (#667).
func plainSummary(summary string) string {
	var out []string
	for _, line := range strings.Split(summary, "\n") {
		line = inlineMark.ReplaceAllString(listMarkRe.ReplaceAllString(line, ""), "")
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if line == "" || strings.HasPrefix(lower, "(note") || strings.HasPrefix(lower, "note:") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, " ")
}

// transcript is the visit, a line each, labelled in lang (#1340).
func transcript(lines []doorLine, lang string) string {
	var b strings.Builder
	you, visitor := locale.T(lang, "automations:door.you", nil), locale.T(lang, "automations:door.visitor", nil)
	for _, l := range lines {
		who := you
		if l.Visitor {
			who = visitor
		}
		fmt.Fprintf(&b, "%s: %s\n", who, l.Text)
	}
	return strings.TrimSpace(b.String())
}

// doorAnswer asks the run's model, with the automation's profile and no
// tools or memories: the visitor's last words with the visit before them,
// or, with no lines, message alone.
func (a *App) doorAnswer(ctx context.Context, automation automations.Automation, modelID, style string, lines []doorLine, message ...string) (string, error) {
	var history []pluginapi.ChatMessage
	msg := strings.Join(message, "\n")
	if len(lines) > 0 {
		for _, l := range lines[:len(lines)-1] {
			role := "assistant"
			if l.Visitor {
				role = "user"
			}
			history = append(history, pluginapi.ChatMessage{Role: role, Content: l.Text})
		}
		msg = lines[len(lines)-1].Text
	}
	ctx = turnopts.With(ctx, &turnopts.Options{History: history, System: style, FixedProfile: true, Tools: []string{}, ReadOnlyTools: true})
	if len(lines) > 0 {
		ctx = turnopts.WithSpoken(ctx)
	}
	ctx = pluginapi.WithAnswerOptions(ctx, pluginapi.GenerateOptions{NoThinking: true})
	chunks, err := a.RunChat(ctx, automation.ProfileID, "", msg, true, modelID, "local")
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for chunk := range chunks {
		if chunk.Error != "" {
			for range chunks {
			}
			return "", errors.New(chunk.Error)
		}
		b.WriteString(chunk.Content)
	}
	return strings.TrimSpace(b.String()), nil
}
