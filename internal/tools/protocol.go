package tools

import (
	"encoding/json"
	"regexp"
	"strings"
)

// ModelCall is a tool request extracted from model text. It is not chat content.
type ModelCall struct {
	ID   string
	Args map[string]any
}

// ModelOutput classifies one completed model generation.
// Text is the only portion that may be shown or saved as the assistant message.
type ModelOutput struct {
	Text      string
	Call      *ModelCall
	Malformed bool
	Format    string
	Sanitized bool
}

// VisibleText is the assistant transcript after protocol removal.
func VisibleText(content string) string {
	return ParseModelOutput(content).Text
}

// ParseModelOutput splits model text into a user-visible answer and at most one tool call.
// Ordinary JSON and code samples stay in the answer. Only known Yggdrasil wrappers are tool calls.
func ParseModelOutput(content string) ModelOutput {
	spans := protocolSpans(content)
	var call *ModelCall
	format := ""
	malformed := false
	for _, span := range spans {
		if span.kind == "result" || span.kind == "token" {
			continue
		}
		parsed, ok, bad := parseProtocolSpan(content[span.start:span.end])
		if format == "" && span.format != "" {
			format = span.format
		}
		if bad {
			malformed = true
		}
		if ok && call == nil {
			call = parsed
			format = span.format
		}
	}
	text := removeSpans(content, spans)
	text = controlTokens.ReplaceAllString(text, "")
	text = emptyFence.ReplaceAllString(text, "")
	text = openCallFence.ReplaceAllString(text, "")
	if call != nil {
		text = suppressToolNarration(text)
	}
	text = cleanupText(text)
	sanitized := text != strings.TrimSpace(content)
	if call == nil && malformed {
		format = "rejected"
	}
	return ModelOutput{
		Text:      text,
		Call:      call,
		Malformed: malformed && call == nil,
		Format:    format,
		Sanitized: sanitized,
	}
}

type span struct {
	start, end int
	kind       string
	format     string
}

var (
	controlTokens = regexp.MustCompile(`(?i)<\|(?:im_start|im_end|eot_id|endoftext|end|start_header_id|end_header_id)\|>(?:.*?<\|(?:im_end|eot_id)\|>)?|\[/?INST\]`)
	// openCallFence is a call's fence with nothing after it: the model stopped.
	openCallFence = regexp.MustCompile("(?s)```(?:tool_call|tool_code)\\s*$")
	emptyFence    = regexp.MustCompile("(?s)```(?:json|JSON|tool_call|tool_code)?\\s*```")
	narration     = regexp.MustCompile(`(?i)(?:thank you for clarifying[.!]?\s*)?(?:i will now|i am going to|i'm going to|let me)\b[^.\n]{0,180}\b(?:tool call|tools?|function call|web tool)\b[^.\n]*[.!?]?`)
)

func protocolSpans(content string) []span {
	var spans []span
	spans = append(spans, tagSpans(content)...)
	spans = append(spans, functionCallSpans(content)...)
	spans = append(spans, jsonProtocolSpans(content)...)
	spans = append(spans, tokenSpans(content)...)
	return mergeSpans(spans)
}

func tagSpans(content string) []span {
	var spans []span
	lower := strings.ToLower(content)
	for _, name := range []string{"tool_call", "tool_result"} {
		open := "<" + name + ">"
		close := "</" + name + ">"
		from := 0
		for {
			start := strings.Index(lower[from:], open)
			if start < 0 {
				break
			}
			start += from
			endRel := strings.Index(lower[start+len(open):], close)
			if endRel < 0 {
				spans = append(spans, span{start: start, end: len(content), kind: tagKind(name), format: "tag"})
				break
			}
			end := start + len(open) + endRel + len(close)
			spans = append(spans, span{start: start, end: end, kind: tagKind(name), format: "tag"})
			from = end
		}
	}
	return spans
}

func tagKind(name string) string {
	if name == "tool_result" {
		return "result"
	}
	return "call"
}

func functionCallSpans(content string) []span {
	var spans []span
	lower := strings.ToLower(content)
	key := "function_call"
	from := 0
	for {
		start := strings.Index(lower[from:], key)
		if start < 0 {
			break
		}
		start += from
		i := start + len(key)
		for i < len(content) && (content[i] == ' ' || content[i] == '\n' || content[i] == '\t') {
			i++
		}
		if i >= len(content) || content[i] != '(' {
			from = start + len(key)
			continue
		}
		end, ok := balanced(content, i, '(', ')')
		if !ok {
			spans = append(spans, span{start: start, end: len(content), kind: "call", format: "function"})
			break
		}
		spans = append(spans, span{start: start, end: end, kind: "call", format: "function"})
		from = end
	}
	return spans
}

func jsonProtocolSpans(content string) []span {
	var spans []span
	for i := 0; i < len(content); i++ {
		if content[i] != '{' {
			continue
		}
		if overlaps(spans, i) {
			continue
		}
		end, ok := jsonObjectEnd(content, i)
		if !ok {
			if strings.Contains(content[i:], `"tool_call"`) || strings.Contains(content[i:], `"tool_calls"`) {
				spans = append(spans, span{start: i, end: len(content), kind: "call", format: "json"})
				break
			}
			continue
		}
		raw := content[i:end]
		if !isProtocolJSON(raw) {
			continue
		}
		kind := "call"
		if strings.Contains(raw, `"tool_result"`) && !strings.Contains(raw, `"tool_call"`) {
			kind = "result"
		}
		format := "json"
		if insideFence(content, i) {
			format = "fenced"
		}
		spans = append(spans, span{start: i, end: end, kind: kind, format: format})
		i = end - 1
	}
	return spans
}

func tokenSpans(content string) []span {
	var spans []span
	for _, loc := range controlTokens.FindAllStringIndex(content, -1) {
		spans = append(spans, span{start: loc[0], end: loc[1], kind: "token", format: "token"})
	}
	return spans
}

func isProtocolJSON(raw string) bool {
	return strings.Contains(raw, `"tool_call"`) || strings.Contains(raw, `"tool_calls"`) || strings.Contains(raw, `"tool_result"`)
}

func insideFence(content string, index int) bool {
	return strings.Count(content[:index], "```")%2 == 1
}

func parseProtocolSpan(raw string) (*ModelCall, bool, bool) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```JSON")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "<tool_result") {
		return nil, false, false
	}
	if strings.HasPrefix(lower, "<tool_call") {
		inner := raw
		if i := strings.Index(raw, ">"); i >= 0 {
			inner = raw[i+1:]
		}
		inner = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(inner, "</tool_call>"), "</TOOL_CALL>"))
		return decodeCall(inner)
	}
	if strings.HasPrefix(lower, "function_call") {
		return decodeFunctionCall(raw)
	}
	return decodeCall(raw)
}

func decodeFunctionCall(raw string) (*ModelCall, bool, bool) {
	open := strings.Index(raw, "(")
	if open < 0 {
		return nil, false, true
	}
	body := strings.TrimSpace(raw[open+1:])
	body = strings.TrimSuffix(body, ")")
	comma := strings.Index(body, ",")
	if comma < 0 {
		return nil, false, true
	}
	id := strings.Trim(strings.TrimSpace(body[:comma]), `"'`)
	argsRaw := strings.TrimSpace(body[comma+1:])
	args, ok := decodeArgs(argsRaw)
	if !ok || !validCall(id, args) {
		return nil, false, true
	}
	return &ModelCall{ID: id, Args: args}, true, false
}

func decodeCall(raw string) (*ModelCall, bool, bool) {
	candidates := []string{raw, repairJSONEscapes(raw), stripTrailingCommas(repairJSONEscapes(raw))}
	sawWrapper := strings.Contains(raw, "tool_call")
	for _, candidate := range candidates {
		if call, ok := unmarshalCall(candidate); ok {
			if _, plain := call.Args[plainArgs]; !plain && validCall(call.ID, call.Args) {
				return call, true, false
			}
			if repaired, ok := repairCall(call); ok {
				return repaired, true, false
			}
			return nil, false, true
		}
	}
	if sawWrapper {
		return nil, false, true
	}
	return nil, false, false
}

type toolCallBody struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Tool       string          `json:"tool"`
	Args       json.RawMessage `json:"args"`
	Arguments  json.RawMessage `json:"arguments"`
	Parameters json.RawMessage `json:"parameters"`
}

func unmarshalCall(raw string) (*ModelCall, bool) {
	var wrapper struct {
		ToolCall  json.RawMessage `json:"tool_call"`
		ToolCalls json.RawMessage `json:"tool_calls"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return nil, false
	}
	if len(wrapper.ToolCalls) > 0 && string(wrapper.ToolCalls) != "null" {
		var list []struct {
			Function struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"function"`
			ID   string          `json:"id"`
			Name string          `json:"name"`
			Args json.RawMessage `json:"args"`
		}
		if err := json.Unmarshal(wrapper.ToolCalls, &list); err != nil || len(list) == 0 {
			return nil, false
		}
		item := list[0]
		id := firstNonEmpty(item.Function.Name, item.Name, item.ID)
		argsRaw := item.Function.Arguments
		if len(argsRaw) == 0 {
			argsRaw = item.Args
		}
		args, ok := decodeArgs(string(argsRaw))
		if !ok {
			return nil, false
		}
		return &ModelCall{ID: id, Args: args}, true
	}
	if len(wrapper.ToolCall) == 0 || string(wrapper.ToolCall) == "null" {
		var body toolCallBody
		if err := json.Unmarshal([]byte(raw), &body); err != nil {
			return nil, false
		}
		id := firstNonEmpty(body.ID, body.Name, body.Tool)
		if id == "" || !KnownTool(id) {
			return nil, false
		}
		argsRaw := firstRaw(body.Args, body.Arguments, body.Parameters)
		args, ok := decodeArgs(string(argsRaw))
		if !ok {
			args = map[string]any{}
		}
		return &ModelCall{ID: id, Args: args}, true
	}
	var body toolCallBody
	if err := json.Unmarshal(wrapper.ToolCall, &body); err != nil {
		return nil, false
	}
	id := firstNonEmpty(body.ID, body.Name, body.Tool)
	if id == "" {
		return nil, false
	}
	argsRaw := firstRaw(body.Args, body.Arguments, body.Parameters)
	args, ok := decodeArgs(string(argsRaw))
	if !ok && len(argsRaw) > 0 {
		var text string
		if json.Unmarshal(argsRaw, &text) != nil || strings.TrimSpace(text) == "" {
			return nil, false
		}
		args = map[string]any{plainArgs: text}
	}
	if args == nil {
		args = map[string]any{}
	}
	return &ModelCall{ID: id, Args: args}, true
}

func decodeArgs(raw string) (map[string]any, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return map[string]any{}, true
	}
	if strings.HasPrefix(raw, `"`) {
		var encoded string
		if err := json.Unmarshal([]byte(raw), &encoded); err != nil {
			return nil, false
		}
		raw = encoded
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		repaired := repairJSONEscapes(raw)
		if err := json.Unmarshal([]byte(repaired), &args); err != nil {
			return nil, false
		}
	}
	return args, true
}

func firstRaw(values ...json.RawMessage) json.RawMessage {
	for _, value := range values {
		if len(value) > 0 && string(value) != "null" {
			return value
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// plainArgs holds arguments a model gave as a bare string, such as a command,
// until the call's tool says which argument that is.
const plainArgs = "\x00plain"

// callNumber is a number a model added to a tool's id to label the call, as
// in terminal_1.
var callNumber = regexp.MustCompile(`[_-]\d+$`)

// repairCall mends how a call is written, never what it asks for, as models
// such as Qwen 3.5 write them: an id that numbers the call, such as
// terminal_1, is the tool; an id that names no tool, such as run_tests, is the
// one tool whose required arguments are exactly the ones given; and arguments
// given as a bare string are the tool's one required argument. Whether the
// tool may run is decided later, as for any call.
func repairCall(call *ModelCall) (*ModelCall, bool) {
	args := call.Args
	text, plain := args[plainArgs].(string)
	id := call.ID
	if !KnownTool(id) {
		switch trimmed := callNumber.ReplaceAllString(id, ""); {
		case trimmed != id && KnownTool(trimmed):
			id = trimmed
		case !plain:
			id = toolForArgs(args)
		default:
			return nil, false
		}
	}
	id = Canonical(id)
	if plain {
		required := requiredArgs(id)
		if len(required) != 1 || len(args) != 1 {
			return nil, false
		}
		args = map[string]any{required[0]: text}
	}
	if id == "" || !validCall(id, args) {
		return nil, false
	}
	return &ModelCall{ID: id, Args: args}, true
}

// toolForArgs is the one tool whose required arguments are exactly these, or
// "" when none or several are.
func toolForArgs(args map[string]any) string {
	found := ""
	for _, def := range Catalog() {
		required := requiredArgs(def.ID)
		if len(required) == 0 || len(required) != len(args) {
			continue
		}
		matches := true
		for _, key := range required {
			if _, ok := args[key]; !ok {
				matches = false
			}
		}
		if !matches || !validCall(def.ID, args) {
			continue
		}
		if found != "" {
			return ""
		}
		found = def.ID
	}
	return found
}

func KnownTool(id string) bool {
	_, ok := Lookup(id)
	return ok
}

func validCall(id string, args map[string]any) bool {
	def, ok := Lookup(id)
	if !ok || args == nil {
		return false
	}
	var schema map[string]string
	if err := json.Unmarshal([]byte(def.Schema), &schema); err != nil {
		return false
	}
	for key, value := range args {
		typ, known := schema[key]
		if !known || !valueMatches(typ, value) {
			return false
		}
	}
	for _, key := range requiredArgs(id) {
		value, present := args[key]
		if !present || strings.TrimSpace(stringify(value)) == "" {
			return false
		}
	}
	return true
}

func requiredArgs(id string) []string {
	switch id {
	case "internet.search", "filesystem.search":
		return []string{"query"}
	case "internet.open":
		return []string{"url"}
	case "filesystem.read":
		return []string{"path"}
	case "filesystem.write":
		return []string{"path", "content"}
	case "terminal":
		return []string{"command"}
	case "git.commit":
		return []string{"message"}
	default:
		return nil
	}
}

func valueMatches(typ string, value any) bool {
	switch typ {
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		_, ok := value.(float64)
		return ok
	case "bool":
		_, ok := value.(bool)
		return ok
	default:
		return true
	}
}

func stringify(value any) string {
	text, _ := value.(string)
	return text
}

func suppressToolNarration(text string) string {
	return strings.TrimSpace(narration.ReplaceAllString(text, ""))
}

func removeSpans(content string, spans []span) string {
	if len(spans) == 0 {
		return content
	}
	var b strings.Builder
	at := 0
	for _, span := range mergeSpans(spans) {
		if span.start > at {
			b.WriteString(content[at:span.start])
		}
		if span.end > at {
			at = span.end
		}
	}
	if at < len(content) {
		b.WriteString(content[at:])
	}
	return b.String()
}

func mergeSpans(spans []span) []span {
	if len(spans) == 0 {
		return nil
	}
	ordered := append([]span(nil), spans...)
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0 && ordered[j].start < ordered[j-1].start; j-- {
			ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
		}
	}
	out := []span{ordered[0]}
	for _, span := range ordered[1:] {
		last := &out[len(out)-1]
		if span.start <= last.end {
			if span.end > last.end {
				last.end = span.end
			}
			continue
		}
		out = append(out, span)
	}
	return out
}

func overlaps(spans []span, index int) bool {
	for _, span := range spans {
		if index >= span.start && index < span.end {
			return true
		}
	}
	return false
}

func cleanupText(text string) string {
	text = strings.TrimSpace(text)
	lines := strings.Split(text, "\n")
	var kept []string
	blank := 0
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
		if strings.TrimSpace(trimmed) == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		kept = append(kept, trimmed)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func balanced(s string, start int, open, close byte) (int, bool) {
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			continue
		}
		if c == open {
			depth++
		}
		if c == close {
			depth--
			if depth == 0 {
				return i + 1, true
			}
		}
	}
	return 0, false
}

func jsonObjectEnd(s string, start int) (int, bool) {
	return balanced(s, start, '{', '}')
}

func repairJSONEscapes(raw string) string {
	var b strings.Builder
	b.Grow(len(raw) + 8)
	inString := false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if !inString {
			if c == '"' {
				inString = true
			}
			b.WriteByte(c)
			continue
		}
		if c != '\\' {
			if c == '"' {
				inString = false
			}
			b.WriteByte(c)
			continue
		}
		if i+1 >= len(raw) {
			b.WriteString(`\\`)
			break
		}
		next := raw[i+1]
		if next == 'u' && i+5 < len(raw) && isHex(raw[i+2:i+6]) {
			b.WriteString(raw[i : i+6])
			i += 5
			continue
		}
		if isValidJSONEscape(next) {
			b.WriteByte('\\')
			b.WriteByte(next)
			i++
			continue
		}
		b.WriteString(`\\`)
	}
	return b.String()
}

func isValidJSONEscape(c byte) bool {
	switch c {
	case '"', '\\', '/', 'b', 'f', 'n', 'r', 't', 'u':
		return true
	default:
		return false
	}
}

func isHex(s string) bool {
	if len(s) != 4 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

func stripTrailingCommas(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	inString := false
	escaped := false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if inString {
			b.WriteByte(c)
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			b.WriteByte(c)
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(raw) && (raw[j] == ' ' || raw[j] == '\n' || raw[j] == '\t' || raw[j] == '\r') {
				j++
			}
			if j < len(raw) && (raw[j] == '}' || raw[j] == ']') {
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}
