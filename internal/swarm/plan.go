package swarm

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/yeixio/toskar-core/internal/structured"
	"github.com/yeixio/toskar-core/internal/tools"
)

// Part is one agent's share of the job.
type Part struct {
	// Title is a few words, for the agent's lane and chat: "tests".
	Title string `json:"title"`
	// Instructions are what the agent is asked to do.
	Instructions string `json:"instructions"`
}

// Caps on a planned part.
const (
	maxTitleRunes        = 60
	maxInstructionsRunes = 1200
)

// PlannerInstructions ask the coordinator's planner for a split's parts. It
// extends Team's plan: each part has a title as well as its instructions.
// %d is the most parts.
const PlannerInstructions = "You coordinate several assistants who will work on one request at the same time, each on its own. " +
	"Split the request into the parts they can do separately, from 2 to %d parts. " +
	"Each part needs a short title of a few words and clear instructions that make sense on their own. " +
	"Parts must not overlap: give work that belongs together to one part. Reply with JSON only: " +
	`{"parts": [{"title": "...", "instructions": "..."}]}. ` +
	"If the request cannot be split, reply with one part: the whole request."

// ParseParts reads a planner's reply, keeping at most maxParts parts. A part
// may be an object with a title and instructions, as PlannerInstructions
// asks, or a plain string, as Team's planner writes.
func ParseParts(content string, maxParts int) []Part {
	found, ok := structured.Extract(tools.VisibleText(content))
	if !ok {
		return nil
	}
	var list []any
	switch v := found.Value.(type) {
	case map[string]any:
		list, _ = v["parts"].([]any)
	case []any:
		list = v
	}
	var parts []Part
	for _, item := range list {
		var p Part
		switch v := item.(type) {
		case string:
			p.Instructions = v
		case map[string]any:
			p.Title, _ = v["title"].(string)
			p.Instructions, _ = v["instructions"].(string)
		}
		p.Title = clipRunes(strings.TrimSpace(p.Title), maxTitleRunes)
		p.Instructions = clipRunes(strings.TrimSpace(p.Instructions), maxInstructionsRunes)
		if p.Instructions == "" {
			p.Instructions = p.Title
		}
		if p.Instructions == "" {
			continue
		}
		if p.Title == "" {
			p.Title = TitleOf(p.Instructions)
		}
		parts = append(parts, p)
		if maxParts > 0 && len(parts) == maxParts {
			break
		}
	}
	return parts
}

// RaceParts are a race's parts: the whole request, once per agent.
func RaceParts(request string, agents int) []Part {
	parts := make([]Part, agents)
	for i := range parts {
		parts[i] = Part{Title: "whole request", Instructions: request}
	}
	return parts
}

// TitleOf makes a short title from instructions: their first few words.
func TitleOf(text string) string {
	words := strings.Fields(text)
	if len(words) > 6 {
		words = words[:6]
	}
	return clipRunes(strings.TrimRight(strings.Join(words, " "), ".,:;"), maxTitleRunes)
}

// FirstMessage is agent n's first message: the whole request, its part, and
// what the other agents are doing, so it stays in its lane.
func FirstMessage(mode, request string, parts []Part, n int) string {
	var b strings.Builder
	if mode == ModeRace {
		fmt.Fprintf(&b, "You are agent %d of %d. Each agent answers the same request on its own, and the best answer is kept or the answers are combined. Answer the whole request as well as you can.\n\n", n, len(parts))
		fmt.Fprintf(&b, "The request:\n%s", request)
		return b.String()
	}
	fmt.Fprintf(&b, "You are agent %d of %d working on one request at the same time. Do only your part: the others are doing theirs, and a coordinator combines everyone's work into the answer. Give a complete answer for your part, with the facts, figures, names, and links that matter.\n\n", n, len(parts))
	fmt.Fprintf(&b, "The whole request:\n%s\n\n", request)
	fmt.Fprintf(&b, "Your part (%s):\n%s\n", parts[n-1].Title, parts[n-1].Instructions)
	if len(parts) > 1 {
		b.WriteString("\nWhat the other agents are doing:\n")
		for i, p := range parts {
			if i != n-1 {
				fmt.Fprintf(&b, "- Agent %d: %s\n", i+1, p.Title)
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// JobName names the project a swarm makes for a chat that has none: the
// request's first words.
func JobName(request string) string {
	words := strings.Fields(request)
	if len(words) > 8 {
		words = words[:8]
	}
	name := clipRunes(strings.TrimRight(strings.Join(words, " "), ".,:;?!"), maxTitleRunes)
	if name == "" {
		return "Swarm"
	}
	return name
}

func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) > n {
		return strings.TrimSpace(string([]rune(s)[:n])) + "…"
	}
	return s
}
