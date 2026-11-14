package swarm

import (
	"fmt"
	"path"
	"slices"
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
	// Paths are the files or folders a task's part expects to change,
	// relative to the folder; empty when the planner didn't say.
	Paths []string `json:"paths,omitempty"`
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
			p.Paths = cleanPaths(v["paths"])
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

// Caps on a part's paths.
const (
	maxPaths     = 20
	maxPathRunes = 200
)

// cleanPaths reads a part's paths: a list, or one path as a string. Each is
// relative to the folder, with forward slashes; one that points outside it,
// or at all of it, is dropped.
func cleanPaths(v any) []string {
	var raw []string
	switch v := v.(type) {
	case string:
		raw = []string{v}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				raw = append(raw, s)
			}
		}
	}
	var out []string
	for _, p := range raw {
		p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
		if p == "" || utf8.RuneCountInString(p) > maxPathRunes || strings.HasPrefix(p, "/") {
			continue
		}
		p = strings.TrimSuffix(path.Clean(p), "/")
		if p == "." || p == ".." || strings.HasPrefix(p, "../") || slices.Contains(out, p) {
			continue
		}
		out = append(out, p)
		if len(out) == maxPaths {
			break
		}
	}
	return out
}

// pathsOverlap reports two parts that expect to change the same file or
// folder, or one inside the other.
func pathsOverlap(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y || strings.HasPrefix(y, x+"/") || strings.HasPrefix(x, y+"/") {
				return true
			}
		}
	}
	return false
}

// MergeOverlapping gives parts whose paths overlap to one agent, so their
// changes don't conflict (docs/features/swarms.md, the coordinator's plan).
// Parts keep their order, each merged part where its first one was.
func MergeOverlapping(parts []Part) []Part {
	group := make([]int, len(parts))
	for i := range group {
		group[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if group[i] != i {
			group[i] = find(group[i])
		}
		return group[i]
	}
	for i := range parts {
		for j := i + 1; j < len(parts); j++ {
			if pathsOverlap(parts[i].Paths, parts[j].Paths) {
				a, b := find(i), find(j)
				group[max(a, b)] = min(a, b)
			}
		}
	}
	var out []Part
	at := map[int]int{}
	for i, p := range parts {
		root := find(i)
		k, ok := at[root]
		if !ok {
			at[root] = len(out)
			out = append(out, Part{Title: p.Title, Instructions: p.Instructions, Paths: append([]string(nil), p.Paths...)})
			continue
		}
		m := &out[k]
		m.Title = clipRunes(m.Title+" + "+p.Title, maxTitleRunes)
		m.Instructions = clipRunes(m.Instructions+"\n\n"+p.Instructions, 2*maxInstructionsRunes)
		for _, path := range p.Paths {
			if !slices.Contains(m.Paths, path) {
				m.Paths = append(m.Paths, path)
			}
		}
	}
	return out
}

// TaskPlannerInstructions ask the coordinator's planner for a task's parts,
// after its look at the folder. %d is the most parts.
const TaskPlannerInstructions = "You coordinate several assistants who will work on one task in a project folder at the same time, each in its own copy of the folder. " +
	"First look at the folder: the list of its files is below, and you can read files to see how the project is laid out. You can't change anything or run commands. " +
	"Then split the task into parts they can do separately, from 2 to %d parts. " +
	"Each part needs a short title of a few words, clear instructions that make sense on their own, and the paths of the files or folders it expects to change, relative to the folder. " +
	"Parts must not change the same files: give work on the same files to one part. When you're ready, reply with JSON only: " +
	`{"parts": [{"title": "...", "instructions": "...", "paths": ["..."]}]}. ` +
	"If the task cannot be split, reply with one part: the whole task."

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

// SearchQuery is what agent n looks up in the profile's knowledge: its part.
// Not its first message, whose first words are the instructions every agent
// gets, and not the whole request, whose other parts outscore its own: in
// both, another part's file came back and its own was left out (#1182). A
// race's agents each answer the whole request.
func SearchQuery(mode, request string, parts []Part, n int) string {
	if mode == ModeRace || n < 1 || n > len(parts) {
		return request
	}
	p := parts[n-1]
	if q := strings.TrimSpace(p.Title + "\n" + p.Instructions); q != "" {
		return q
	}
	return request
}

// TaskMessage is a task agent n's first message: the whole task, its part,
// and what the other agents are doing. shared says the agents take turns in
// the person's folder, rather than each working in a copy of its own.
func TaskMessage(mode, request string, parts []Part, n int, shared bool) string {
	var b strings.Builder
	if mode == ModeRace {
		fmt.Fprintf(&b, "You are agent %d of %d. Each agent does the same task on its own, in its own copy of the project folder, and the best result is kept. Do the whole task, and check that it works. Toskar commits your changes when you're done, so don't commit them.\n\n", n, len(parts))
		fmt.Fprintf(&b, "The task:\n%s", request)
		return b.String()
	}
	if shared {
		fmt.Fprintf(&b, "You are agent %d of %d working on one task. The agents take turns in the same project folder, so agents before you may already have changed it. Do only your part, and leave the others' changes as they are. When you're done, say what you changed and how you checked it.\n\n", n, len(parts))
	} else {
		fmt.Fprintf(&b, "You are agent %d of %d working on one task at the same time, each in its own copy of the project folder. Do only your part: the others are doing theirs, and Toskar merges everyone's changes. Change only what your part needs. Toskar commits your changes when you're done, so don't commit them. When you're done, say what you changed and how you checked it.\n\n", n, len(parts))
	}
	fmt.Fprintf(&b, "The whole task:\n%s\n\n", request)
	p := parts[n-1]
	fmt.Fprintf(&b, "Your part (%s):\n%s\n", p.Title, p.Instructions)
	if len(p.Paths) > 0 {
		fmt.Fprintf(&b, "Files you expect to change: %s\n", strings.Join(p.Paths, ", "))
	}
	if len(parts) > 1 {
		b.WriteString("\nWhat the other agents are doing:\n")
		for i, o := range parts {
			if i == n-1 {
				continue
			}
			fmt.Fprintf(&b, "- Agent %d: %s", i+1, o.Title)
			if len(o.Paths) > 0 {
				fmt.Fprintf(&b, " (%s)", strings.Join(o.Paths, ", "))
			}
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// FixupMessage is the first message of the agent that settles a merge
// conflict between two agents' changes, in the merge's own worktree.
func FixupMessage(request string, ours, theirs Agent, files []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Two agents worked on parts of one task at the same time, and merging agent %d's change into the others' work conflicts. ", theirs.N)
	b.WriteString("Settle the conflict in the files below so both changes work together: keep what each part needs, and remove every conflict marker (the lines starting <<<<<<<, =======, and >>>>>>>). ")
	b.WriteString("Then run the project's checks if it has any. Toskar commits the merge when you're done, so don't commit it.\n\n")
	fmt.Fprintf(&b, "The whole task:\n%s\n\n", request)
	fmt.Fprintf(&b, "Agent %d's part (%s):\n%s\n\n", ours.N, ours.Part.Title, ours.Part.Instructions)
	fmt.Fprintf(&b, "Agent %d's part (%s):\n%s\n\n", theirs.N, theirs.Part.Title, theirs.Part.Instructions)
	b.WriteString("Files with conflicts:\n")
	for _, f := range files {
		fmt.Fprintf(&b, "- %s\n", f)
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
