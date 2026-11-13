package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/yeixio/toskar-sdks/go/api"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Swarms in the terminal (#1135, docs/features/swarms.md): --swarm and /swarm
// ask for one, each agent shows as one line updated in place while it works,
// then the answer comes, and /agents and /open N reach the agents' chats.

// chatRequest is one turn's POST /chat, with the swarm asked for, if any.
type chatRequest struct {
	api.ChatJSONRequestBody
	// Swarm is split or race, or "" for none.
	Swarm string
}

// Swarm modes a person can ask for.
const (
	swarmSplit = "split"
	swarmRace  = "race"
)

// swarmArg takes --swarm and its mode out of args, since bare --swarm means
// split: the args for flag, and the mode or "".
func swarmArg(args []string) ([]string, string, error) {
	out := make([]string, 0, len(args))
	mode := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			out = append(out, args[i:]...)
			break
		}
		name, value, hasValue := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if !strings.HasPrefix(a, "-") || name != "swarm" {
			out = append(out, a)
			continue
		}
		switch {
		case hasValue && (value == swarmSplit || value == swarmRace):
			mode = value
		case hasValue:
			return nil, "", fmt.Errorf("--swarm is split or race, not %q", value)
		case i+1 < len(args) && (args[i+1] == swarmSplit || args[i+1] == swarmRace):
			mode = args[i+1]
			i++
		default:
			mode = swarmSplit
		}
	}
	return out, mode, nil
}

type swarmKey struct{}

// withSwarm asks for a swarm on the chat request sent with ctx.
func withSwarm(ctx context.Context, mode string) context.Context {
	if mode == "" {
		return ctx
	}
	return context.WithValue(ctx, swarmKey{}, mode)
}

// swarmField adds swarm to POST /chat's body, which this SDK's request type
// doesn't have yet (contract 1.88).
type swarmField struct{ next http.RoundTripper }

func (t swarmField) RoundTrip(r *http.Request) (*http.Response, error) {
	mode, _ := r.Context().Value(swarmKey{}).(string)
	if mode == "" || r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/api/v1/chat") || r.Body == nil {
		return t.next.RoundTrip(r)
	}
	raw, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		return nil, err
	}
	body := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	body["swarm"], _ = json.Marshal(mode)
	if raw, err = json.Marshal(body); err != nil {
		return nil, err
	}
	r = r.Clone(r.Context())
	r.Body = io.NopCloser(bytes.NewReader(raw))
	r.ContentLength = int64(len(raw))
	r.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil }
	return t.next.RoundTrip(r)
}

// swarmView is the latest swarm in this chat, as its events tell it. The
// event stream writes it, and the turn shows it.
type swarmView struct {
	mu sync.Mutex
	// seq counts the swarms started, so a turn knows one started in it.
	seq int
	id  string
	// conv is the coordinator's chat, where the answer goes.
	conv  string
	mode  string
	lanes []swarmLane
	// footer says what the swarm as a whole is doing: merging, or done.
	footer string
	// conflicts are agents' changes that couldn't be merged.
	conflicts []string
	// plain are the changed lines not yet shown where nothing can be redrawn.
	plain []plainLine
	// last is each line as last queued in plain, by agent (0 for the footer).
	last map[int]string
	// done says the swarm has finished.
	done bool
}

// swarmLane is one agent's line.
type swarmLane struct {
	n                               int
	part, node, model, status, step string
	conv                            string
	seconds                         float64
}

// inSwarm says whether an event is about the swarm this chat runs: an
// agent's question, or its answer.
func (v *swarmView) inSwarm(p map[string]any) bool {
	id, _ := p["swarm_id"].(string)
	v.mu.Lock()
	defer v.mu.Unlock()
	return id != "" && id == v.id
}

// number reads a JSON number from an event's payload.
func number(p map[string]any, key string) float64 {
	switch n := p[key].(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	}
	return 0
}

func text(p map[string]any, key string) string {
	s, _ := p[key].(string)
	return s
}

// event takes in one swarm.* event for this chat, and says whether there's
// something new to show.
func (v *swarmView) event(kind string, p map[string]any) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	if kind == "swarm.note" {
		// Why a swarm ran as one agent, or took turns, is the answer's
		// notice too, in the person's language: the one quiet line under
		// the answer that every turn shows (showNotice).
		return false
	}
	id := text(p, "swarm_id")
	if kind == "swarm.started" {
		v.seq++
		v.id, v.conv, v.mode = id, text(p, "conversation_id"), text(p, "mode")
		v.lanes, v.footer, v.conflicts, v.plain, v.last, v.done = nil, "", nil, nil, map[int]string{}, false
		agents, _ := p["agents"].([]any)
		for _, raw := range agents {
			a, _ := raw.(map[string]any)
			v.lanes = append(v.lanes, swarmLane{n: int(number(a, "agent")), part: text(a, "part"), node: text(a, "node_id"),
				model: text(a, "model"), status: text(a, "status"), conv: text(a, "conversation_id")})
		}
		for i := range v.lanes {
			v.queueLane(v.lanes[i])
		}
		return true
	}
	if id == "" || id != v.id {
		return false
	}
	lane := func() *swarmLane {
		n := int(number(p, "agent"))
		for i := range v.lanes {
			if v.lanes[i].n == n {
				return &v.lanes[i]
			}
		}
		// A fix-up agent, settling a merge conflict, joins as it starts.
		v.lanes = append(v.lanes, swarmLane{n: n, part: "settling a merge conflict"})
		return &v.lanes[len(v.lanes)-1]
	}
	switch kind {
	case "swarm.agent.started":
		l := lane()
		l.status = "running"
		if node := text(p, "node_id"); node != "" {
			l.node = node
		}
		if model := text(p, "model"); model != "" {
			l.model = model
		}
		if conv := text(p, "agent_conversation_id"); conv != "" {
			l.conv = conv
		}
		v.queueLane(*l)
	case "swarm.agent.progress":
		l := lane()
		l.status = "running"
		l.step = stepLine(text(p, "tool"), text(p, "summary"))
		v.queueLane(*l)
	case "swarm.agent.finished":
		l := lane()
		l.status, l.seconds = text(p, "status"), number(p, "seconds")
		v.queueLane(*l)
	case "swarm.merging":
		n := int(number(p, "agent"))
		switch conflict, _ := p["conflict"].(map[string]any); {
		case conflict != nil:
			var files []string
			list, _ := conflict["files"].([]any)
			for _, f := range list {
				if s, ok := f.(string); ok {
					files = append(files, s)
				}
			}
			line := fmt.Sprintf("Agent %d's change wasn't merged: it conflicts with the others", n)
			if len(files) > 0 {
				line += " in " + strings.Join(files, ", ")
			}
			if branch := text(conflict, "branch"); branch != "" {
				line += ". It's kept on " + branch
			}
			v.conflicts = append(v.conflicts, line)
			v.plain = append(v.plain, plainLine{text: line})
		case n > 0:
			v.footer = fmt.Sprintf("Merging agent %d's change", n)
			if branch := text(p, "branch"); branch != "" {
				v.footer += " from " + branch
			}
			v.queue(0, v.footer)
		case v.mode == swarmRace:
			v.footer = "Picking the best answer"
			v.queue(0, v.footer)
		default:
			v.footer = "Merging the answers"
			v.queue(0, v.footer)
		}
	case "swarm.finished":
		v.done = true
		v.footer = swarmFinished(text(p, "status"), int(number(p, "winner")), int(number(p, "tokens")), number(p, "seconds"))
		v.queue(0, v.footer)
	default:
		return false
	}
	return true
}

// plainLine is a changed line, for where lines can't be redrawn: a lane as it
// was, or text.
type plainLine struct {
	lane *swarmLane
	text string
}

func (p plainLine) line(names map[string]string) string {
	if p.lane != nil {
		return p.lane.line(names)
	}
	return p.text
}

// queue keeps a changed line for where lines can't be redrawn.
func (v *swarmView) queue(key int, line string) {
	if v.last[key] == line {
		return
	}
	v.last[key] = line
	v.plain = append(v.plain, plainLine{text: line})
}

// queueLane keeps a lane that changed.
func (v *swarmView) queueLane(l swarmLane) {
	if line := l.line(nil); v.last[l.n] != line {
		v.last[l.n] = line
		v.plain = append(v.plain, plainLine{lane: &l})
	}
}

// swarmFinished says how a swarm ended.
func swarmFinished(status string, winner, tokens int, seconds float64) string {
	words := map[string]string{"done": "The swarm is done", "stopped": "The swarm stopped", "failed": "The swarm failed"}[status]
	if words == "" {
		words = "The swarm ended"
	}
	var more []string
	if winner > 0 {
		more = append(more, fmt.Sprintf("agent %d's was picked", winner))
	}
	if tokens > 0 {
		more = append(more, fmt.Sprintf("%s tokens", thousands(tokens)))
	}
	if seconds >= 1 {
		more = append(more, duration(seconds))
	}
	if len(more) > 0 {
		words += ": " + strings.Join(more, ", ")
	}
	return words
}

// thousands writes n with commas, such as 12,400.
func thousands(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// duration is seconds as people say it: 42s, or 3m 5s.
func duration(seconds float64) string {
	s := int(seconds + 0.5)
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	return fmt.Sprintf("%dm %ds", s/60, s%60)
}

// line is a lane as one line: Agent 2 · tests · laptop · qwen3.5-9b ·
// running · Writing sum_test.go. names gives computers' names by id.
func (l swarmLane) line(names map[string]string) string {
	parts := []string{fmt.Sprintf("Agent %d", l.n)}
	if part := strings.TrimSpace(l.part); part != "" {
		parts = append(parts, part)
	}
	if l.node != "" {
		name := names[l.node]
		if name == "" {
			name = l.node
		}
		parts = append(parts, name)
	}
	if l.model != "" {
		parts = append(parts, l.model)
	}
	status := strings.ReplaceAll(l.status, "_", " ")
	if status == "" {
		status = "waiting"
	}
	if l.seconds >= 1 && l.status != "running" && l.status != "queued" {
		status += " in " + duration(l.seconds)
	}
	parts = append(parts, status)
	if l.step != "" && l.status == "running" {
		parts = append(parts, l.step)
	}
	return strings.Join(parts, " · ")
}

// snapshot is the swarm's block of lines as they stand, with computers named.
func (v *swarmView) snapshot(names map[string]string) []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	var lines []string
	for _, l := range v.lanes {
		lines = append(lines, l.line(names))
	}
	lines = append(lines, v.conflicts...)
	if v.footer != "" {
		lines = append(lines, v.footer)
	}
	return lines
}

// take hands over the changed lines not yet shown.
func (v *swarmView) take() []plainLine {
	v.mu.Lock()
	defer v.mu.Unlock()
	plain := v.plain
	v.plain = nil
	return plain
}

// running says the latest swarm hasn't finished.
func (v *swarmView) running() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.id != "" && !v.done
}

// current is the latest swarm's number and id.
func (v *swarmView) current() (seq int, id, conv string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.seq, v.id, v.conv
}

// settle brings the lanes up to date from the swarm's record, for events
// still on their way.
func (v *swarmView) settle(rec contracts.Swarm) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if rec.ID != v.id {
		return
	}
	for _, a := range rec.Agents {
		for i := range v.lanes {
			if l := &v.lanes[i]; l.n == a.Agent {
				if a.Status != "" {
					l.status = a.Status
				}
				if a.Seconds > 0 {
					l.seconds = a.Seconds
				}
				if l.conv == "" {
					l.conv = a.ConversationID
				}
			}
		}
	}
}

// partTitle is the title the lanes showed for agent n of swarm id, or "".
func (v *swarmView) partTitle(id string, n int) string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if id != v.id {
		return ""
	}
	for _, l := range v.lanes {
		if l.n == n {
			return l.part
		}
	}
	return ""
}

// noteSwarmEvent takes a swarm event in from the event stream, and tells the
// turn there's something to show.
func (s *chatSession) noteSwarmEvent(kind string, p map[string]any) {
	if !s.swarm.event(kind, p) {
		return
	}
	select {
	case s.swarmChanged <- struct{}{}:
	default:
	}
}

// showSwarm shows what's new in the swarm: on a terminal, its block of lines
// redrawn in place; elsewhere, a plain line for each change, on standard
// error, so a pipe still gets only the answer.
func (s *chatSession) showSwarm() {
	plain := s.swarm.take()
	if !s.io.outTerminal {
		names := s.nodeNames()
		for _, p := range plain {
			fmt.Fprintln(s.io.errOut, p.line(names))
		}
		return
	}
	s.drawLanes(s.swarm.snapshot(s.nodeNames()))
}

// drawLanes draws the swarm's block of lines, over the one drawn before when
// nothing has been written since, each cut to the terminal's width so it
// takes one row.
func (s *chatSession) drawLanes(lines []string) {
	if len(lines) == 0 {
		return
	}
	cols, rows := 0, 0
	if s.io.size != nil {
		cols, rows = s.io.size()
	}
	if cols <= 0 {
		cols = 80
	}
	if same := len(lines) == len(s.lanesDrawn); same && s.lanesShown > 0 {
		for i := range lines {
			same = same && lines[i] == s.lanesDrawn[i]
		}
		if same {
			return
		}
	}
	if s.lanesShown > 0 && (rows <= 0 || s.lanesShown < rows) {
		fmt.Fprintf(s.io.out, "\r\x1b[%dA\x1b[J", s.lanesShown)
	}
	for _, line := range lines {
		fmt.Fprintln(s.io.out, s.dim(fitCells(line, cols-1)))
	}
	s.lanesShown, s.lanesDrawn = len(lines), lines
}

// fitCells cuts line to n columns, ending it with … when cut.
func fitCells(line string, n int) string {
	if cellsOf(line) <= n {
		return line
	}
	var b strings.Builder
	used := 0
	for _, r := range line {
		w := cellsOf(string(r))
		if used+w > n-1 {
			break
		}
		b.WriteRune(r)
		used += w
	}
	return b.String() + "…"
}

// nodeNames are the computers' names by id, asked for once.
func (s *chatSession) nodeNames() map[string]string {
	if s.names != nil {
		return s.names
	}
	s.names = map[string]string{}
	if list, err := s.api.nodes(); err == nil {
		for _, n := range list {
			if n.Name != "" {
				s.names[n.ID] = n.Name
			}
		}
	}
	return s.names
}

// finishSwarm settles the lanes once the answer comes, and says what a task
// swarm did with its change: the branch, the checks, and whether it's in
// the folder.
func (s *chatSession) finishSwarm(id string) {
	rec, err := s.api.swarm(id)
	if err == nil {
		s.swarm.settle(rec)
	}
	s.showSwarm()
	if err != nil {
		return
	}
	var lines []string
	if rec.Folder != "" && rec.Branch != "" {
		line := "The change is on " + rec.Branch
		switch rec.Checks {
		case "passed":
			line += ", and the checks passed"
		case "failed":
			line += ", and the checks failed"
		}
		if rec.Applied {
			line += ". It's applied to " + rec.Folder + " as uncommitted changes."
		} else {
			line += ". It isn't applied to your folder: git merge " + rec.Branch + " takes it."
		}
		lines = append(lines, line)
	}
	if rec.Winner > 0 && rec.Folder == "" {
		lines = append(lines, fmt.Sprintf("Agent %d's answer was picked.", rec.Winner))
	}
	for _, line := range lines {
		if s.io.outTerminal {
			s.lanesShown = 0
			fmt.Fprintln(s.io.out, s.dim(line))
		} else {
			fmt.Fprintln(s.io.errOut, line)
		}
	}
}

// swarmCommand is /swarm: split or race for the next message, alone split,
// and off to stop the swarms --swarm asked for.
func (s *chatSession) swarmCommand(arg string) error {
	switch strings.ToLower(arg) {
	case "", swarmSplit:
		s.nextSwarm = swarmSplit
		s.note("Your next message goes to a swarm: agents split the work and do the parts at once.")
	case swarmRace:
		s.nextSwarm = swarmRace
		s.note("Your next message goes to a race: each model does the whole job, and the best result is kept.")
	case "off":
		s.nextSwarm, s.swarmMode = "", ""
		s.note("Swarms are off in this chat.")
	default:
		return fmt.Errorf("/swarm takes split, race, or off, not %q", arg)
	}
	return nil
}

// lastSwarm is the swarm /agents means: the latest one in this chat, or the
// one the coordinator's chat names.
func (s *chatSession) lastSwarm() (string, error) {
	coordinator := s.conv
	if s.coordinator != "" {
		coordinator = s.coordinator
	}
	if _, id, conv := s.swarm.current(); id != "" && conv == coordinator {
		return id, nil
	}
	list, err := s.api.conversations()
	if err != nil {
		return "", err
	}
	for _, c := range list {
		if c.ID == coordinator && c.SwarmID != "" {
			return c.SwarmID, nil
		}
	}
	return "", errors.New("no swarm has run in this chat yet: /swarm sends your next message to one")
}

// agents is /agents: the last swarm's agents, with their parts, how each
// did, and their chats.
func (s *chatSession) agents() error {
	id, err := s.lastSwarm()
	if err != nil {
		return err
	}
	rec, err := s.api.swarm(id)
	if err != nil {
		return err
	}
	if len(rec.Agents) == 0 {
		// One that ran as one agent says why.
		if rec.Note != "" {
			s.note(rec.Note)
		} else {
			s.note("This swarm has no agents yet.")
		}
		return nil
	}
	for _, a := range rec.Agents {
		part := s.swarm.partTitle(id, a.Agent)
		if part == "" {
			part = fitCells(strings.Join(strings.Fields(a.Part), " "), 60)
		}
		status := strings.ReplaceAll(a.Status, "_", " ")
		fmt.Fprintf(s.io.out, "%d. %s · %s  %s\n", a.Agent, part, status, s.dim(a.ConversationID))
	}
	s.note("/open N continues an agent's chat.")
	return nil
}

// openAgent is /open N: the session goes on in agent N's chat, until /back.
func (s *chatSession) openAgent(arg string) error {
	n, err := strconv.Atoi(strings.TrimSpace(arg))
	if err != nil || n < 1 {
		return errors.New("/open needs an agent's number, as /agents lists them")
	}
	id, err := s.lastSwarm()
	if err != nil {
		return err
	}
	rec, err := s.api.swarm(id)
	if err != nil {
		return err
	}
	for _, a := range rec.Agents {
		if a.Agent != n || a.ConversationID == "" {
			continue
		}
		if s.coordinator == "" {
			s.coordinator = s.conv
		}
		s.conv = a.ConversationID
		s.showLast(s.conv)
		label := fmt.Sprintf("agent %d's chat", n)
		if part := s.swarm.partTitle(id, n); part != "" {
			label += " (" + part + ")"
		}
		s.note("Continuing in " + label + ". /back returns to the swarm's chat.")
		return nil
	}
	return fmt.Errorf("the last swarm has no agent %d; /agents lists them", n)
}

// back is /back: the session returns to the swarm's chat from an agent's.
func (s *chatSession) back() error {
	if s.coordinator == "" {
		return errors.New("you're not in an agent's chat; /open N opens one")
	}
	s.conv, s.coordinator = s.coordinator, ""
	s.note("Back in the swarm's chat.")
	return nil
}

// swarmApply is a task swarm asking to put its change in the person's
// folder. It asks each time.
const swarmApply = "swarm.apply"
