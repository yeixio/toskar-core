// Package structured gets machine-readable results from model output
// (spec §27): it finds the JSON in an answer, makes safe repairs, and
// checks it against a schema (types, required fields, allowed values).
// What fails can be sent back to the model once with the problems named.
// People see the prose; the JSON stays internal.
package structured

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Schema is the subset of JSON Schema Yggdrasil checks: type, properties,
// required, enum, and items.
type Schema struct {
	Type       string             `json:"type,omitempty"`
	Properties map[string]*Schema `json:"properties,omitempty"`
	Required   []string           `json:"required,omitempty"`
	Enum       []any              `json:"enum,omitempty"`
	Items      *Schema            `json:"items,omitempty"`
}

// Object returns an object schema with the given properties, all required.
func Object(props map[string]string) *Schema {
	s := &Schema{Type: "object", Properties: map[string]*Schema{}}
	for name, typ := range props {
		s.Properties[name] = &Schema{Type: typ}
		s.Required = append(s.Required, name)
	}
	sort.Strings(s.Required)
	return s
}

// ParseSchema reads a JSON Schema document.
func ParseSchema(raw []byte) (*Schema, error) {
	var s Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("the schema is not valid JSON: %w", err)
	}
	return &s, nil
}

// Issue is one problem with a value.
type Issue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

func (i Issue) String() string {
	if i.Path == "" {
		return i.Message
	}
	return i.Path + ": " + i.Message
}

// Describe lists issues for the model, one per line.
func Describe(issues []Issue) string {
	var b strings.Builder
	for _, i := range issues {
		b.WriteString("- " + i.String() + "\n")
	}
	return b.String()
}

// Found is a JSON value located in text.
type Found struct {
	Value any
	// Start and End bound the JSON, and any code fence around it, in the text.
	Start, End int
}

var fenceRe = regexp.MustCompile("(?s)```(?:json|JSON)?\\s*(.*?)```")

// Extract finds the last JSON object or array in text, repairing common
// mistakes: code fences, trailing commas, and curly quotes.
func Extract(text string) (Found, bool) {
	// A fenced block is the clearest signal.
	if ms := fenceRe.FindAllStringSubmatchIndex(text, -1); len(ms) > 0 {
		for k := len(ms) - 1; k >= 0; k-- {
			m := ms[k]
			if v, ok := parseLoose(text[m[2]:m[3]]); ok {
				return Found{Value: v, Start: m[0], End: m[1]}, true
			}
		}
	}
	// Otherwise the last balanced {...} or [...] that parses.
	var best Found
	ok := false
	for i := 0; i < len(text); i++ {
		if text[i] != '{' && text[i] != '[' {
			continue
		}
		end, matched := matchBracket(text, i)
		if !matched {
			continue
		}
		if v, parsed := parseLoose(text[i:end]); parsed {
			best, ok = Found{Value: v, Start: i, End: end}, true
			i = end - 1
		}
	}
	return best, ok
}

var trailingCommaRe = regexp.MustCompile(`,\s*([}\]])`)

// parseLoose parses JSON, retrying after safe repairs.
func parseLoose(s string) (any, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, false
	}
	var v any
	if json.Unmarshal([]byte(s), &v) == nil {
		return v, isContainer(v)
	}
	fixed := strings.NewReplacer("“", `"`, "”", `"`, "‘", "'", "’", "'").Replace(s)
	fixed = trailingCommaRe.ReplaceAllString(fixed, "$1")
	if json.Unmarshal([]byte(fixed), &v) == nil {
		return v, isContainer(v)
	}
	return nil, false
}

func isContainer(v any) bool {
	switch v.(type) {
	case map[string]any, []any:
		return true
	}
	return false
}

func matchBracket(s string, start int) (int, bool) {
	depth := 0
	inString, escaped := false, false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth == 0 {
				return i + 1, true
			}
		}
	}
	return 0, false
}

// Prose is the text without the JSON, for people to read.
func Prose(text string, f Found) string {
	return strings.TrimSpace(text[:f.Start] + text[f.End:])
}

var numberCleanRe = regexp.MustCompile(`[\s$€£¥,]|USD|EUR|GBP`)

// Coerce makes safe repairs so a value fits its schema: numbers written as
// text ("$1,299.00"), booleans written as words ("yes"), and numbers or
// JSON data where text is expected. It never invents a value that is not
// there.
func Coerce(v any, s *Schema) any {
	if s == nil {
		return v
	}
	switch s.Type {
	case "number", "integer":
		if str, ok := v.(string); ok {
			clean := numberCleanRe.ReplaceAllString(strings.TrimSpace(str), "")
			if f, err := strconv.ParseFloat(clean, 64); err == nil {
				return f
			}
		}
	case "boolean":
		if str, ok := v.(string); ok {
			switch strings.ToLower(strings.TrimSpace(str)) {
			case "true", "yes", "y":
				return true
			case "false", "no", "n":
				return false
			}
		}
	case "string":
		switch x := v.(type) {
		case float64:
			return strconv.FormatFloat(x, 'f', -1, 64)
		case bool:
			return strconv.FormatBool(x)
		case map[string]any, []any:
			// Data given where text is expected, such as a file's content.
			if raw, err := json.Marshal(x); err == nil {
				return string(raw)
			}
		}
	case "object":
		if m, ok := v.(map[string]any); ok {
			out := make(map[string]any, len(m))
			for k, val := range m {
				out[k] = Coerce(val, s.Properties[k])
			}
			return out
		}
	case "array":
		if a, ok := v.([]any); ok {
			out := make([]any, len(a))
			for i, val := range a {
				out[i] = Coerce(val, s.Items)
			}
			return out
		}
	}
	return v
}

// Validate checks a value against a schema and returns every problem.
func Validate(v any, s *Schema) []Issue {
	var issues []Issue
	validate(v, s, "", &issues)
	return issues
}

func typeName(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		if x == float64(int64(x)) {
			return "integer"
		}
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return fmt.Sprintf("%T", v)
}

func validate(v any, s *Schema, path string, issues *[]Issue) {
	if s == nil {
		return
	}
	add := func(msg string) { *issues = append(*issues, Issue{Path: path, Message: msg}) }
	got := typeName(v)
	switch s.Type {
	case "":
	case "number":
		if got != "number" && got != "integer" {
			add("must be a number, not " + got)
			return
		}
	case "integer":
		if got != "integer" {
			add("must be a whole number, not " + got)
			return
		}
	default:
		if got != s.Type {
			add("must be " + article(s.Type) + ", not " + got)
			return
		}
	}
	if len(s.Enum) > 0 {
		ok := false
		for _, e := range s.Enum {
			if fmt.Sprint(e) == fmt.Sprint(v) {
				ok = true
				break
			}
		}
		if !ok {
			add(fmt.Sprintf("must be one of %v", s.Enum))
		}
	}
	switch x := v.(type) {
	case map[string]any:
		for _, name := range s.Required {
			if _, ok := x[name]; !ok {
				*issues = append(*issues, Issue{Path: join(path, name), Message: "is required"})
			}
		}
		names := make([]string, 0, len(x))
		for k := range x {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			validate(x[k], s.Properties[k], join(path, k), issues)
		}
	case []any:
		for i, item := range x {
			validate(item, s.Items, fmt.Sprintf("%s[%d]", path, i), issues)
		}
	}
}

func join(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

func article(t string) string {
	switch t {
	case "object", "array", "integer":
		return "an " + t
	}
	return "a " + t
}

// Result is a structured result from model output.
type Result struct {
	Value  any
	Found  Found
	Issues []Issue
}

// OK reports a value that was found and fits its schema.
func (r Result) OK() bool { return r.Value != nil && len(r.Issues) == 0 }

// Parse extracts, repairs, and validates the JSON in text against s.
func Parse(text string, s *Schema) Result {
	f, ok := Extract(text)
	if !ok {
		return Result{Issues: []Issue{{Message: "no JSON " + kind(s) + " was found"}}}
	}
	v := Coerce(f.Value, s)
	return Result{Value: v, Found: f, Issues: Validate(v, s)}
}

func kind(s *Schema) string {
	if s != nil && s.Type == "array" {
		return "array"
	}
	return "object"
}

// FixPrompt asks a model to send the JSON again, naming what was wrong
// and the shape it must have.
func FixPrompt(r Result, s *Schema) string {
	shape := ""
	if s != nil {
		raw, _ := json.Marshal(s)
		shape = "\nIt must match this JSON Schema: " + string(raw)
	}
	return "The JSON in your reply has problems:\n" + Describe(r.Issues) + shape +
		"\nReply with only the corrected JSON, no other text."
}

type schemaKey struct{}

// WithSchema marks a turn whose answer must be JSON matching schema, so the
// runtime can constrain the model's output to it.
func WithSchema(ctx context.Context, schema json.RawMessage) context.Context {
	return context.WithValue(ctx, schemaKey{}, schema)
}

// SchemaFrom returns the schema a turn's answer must match, or nil.
func SchemaFrom(ctx context.Context) json.RawMessage {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(schemaKey{}).(json.RawMessage)
	return s
}
