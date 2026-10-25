package structured

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// MaxSchemaBytes is the largest schema a caller may send (#831): turning a
// schema into a grammar costs time and memory that grow with it, so a huge
// one is refused, as xgrammar refuses one since CVE-2025-32381.
const MaxSchemaBytes = 64 << 10

// FullSchema is a caller's JSON Schema, whole (#831): Raw goes to the
// runtime as it was sent, so llama.cpp builds its grammar from every
// keyword it supports, and answers are checked against every keyword too.
// Schema, the subset, still guides the safe repairs.
type FullSchema struct {
	Raw      json.RawMessage
	subset   *Schema
	compiled *jsonschema.Schema
}

// Compile checks a caller's schema: JSON, no bigger than MaxSchemaBytes,
// valid JSON Schema (draft 2020-12 unless it says another), and refers to
// nothing outside itself.
func Compile(raw []byte) (*FullSchema, error) {
	if len(raw) > MaxSchemaBytes {
		return nil, fmt.Errorf("the schema is %d bytes; at most %d are allowed", len(raw), MaxSchemaBytes)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("the schema is not valid JSON: %w", err)
	}
	c := jsonschema.NewCompiler()
	c.UseLoader(noLoader{})
	const loc = "urn:toskar:schema"
	if err := c.AddResource(loc, doc); err != nil {
		return nil, fmt.Errorf("the schema is not valid JSON Schema: %w", err)
	}
	compiled, err := c.Compile(loc)
	if err != nil {
		return nil, fmt.Errorf("the schema is not valid JSON Schema: %s", firstLine(err))
	}
	subset, _ := ParseSchema(raw)
	if subset == nil {
		subset = &Schema{}
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return nil, fmt.Errorf("the schema is not valid JSON: %w", err)
	}
	return &FullSchema{Raw: compact.Bytes(), subset: subset, compiled: compiled}, nil
}

// MustCompile is Compile for a schema Core writes itself.
func MustCompile(raw string) *FullSchema {
	s, err := Compile([]byte(raw))
	if err != nil {
		panic(err)
	}
	return s
}

// noLoader refuses every reference outside the schema: a caller's schema
// can't make Core fetch a URL or read a file.
type noLoader struct{}

func (noLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("the schema refers to %s; only references inside it ($defs) are allowed", url)
}

func firstLine(err error) string {
	s := err.Error()
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimPrefix(s, "jsonschema: ")
}

// Kind is what the answer must be, for the message asking again.
func (s *FullSchema) Kind() string { return kind(s.subset) }

// Validate checks a value against the whole schema and returns every
// problem, by where it is in the value.
func (s *FullSchema) Validate(v any) []Issue {
	// The validator reads numbers as json.Number, as it parsed the schema.
	raw, err := json.Marshal(v)
	if err != nil {
		return []Issue{{Message: err.Error()}}
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return []Issue{{Message: err.Error()}}
	}
	err = s.compiled.Validate(inst)
	if err == nil {
		return nil
	}
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return []Issue{{Message: err.Error()}}
	}
	seen := map[string]bool{}
	var issues []Issue
	var walk func(u jsonschema.OutputUnit)
	walk = func(u jsonschema.OutputUnit) {
		if u.Error != nil && len(u.Errors) == 0 {
			path := strings.TrimPrefix(strings.ReplaceAll(u.InstanceLocation, "/", "."), ".")
			msg := u.Error.String()
			if !seen[path+"\x00"+msg] {
				seen[path+"\x00"+msg] = true
				issues = append(issues, Issue{Path: path, Message: msg})
			}
		}
		for _, e := range u.Errors {
			walk(e)
		}
	}
	walk(*ve.DetailedOutput())
	if len(issues) == 0 {
		issues = append(issues, Issue{Message: firstLine(err)})
	}
	sort.SliceStable(issues, func(i, j int) bool { return issues[i].Path < issues[j].Path })
	return issues
}

// ParseFull extracts, repairs, and validates the JSON in text against the
// whole schema.
func ParseFull(text string, s *FullSchema) Result {
	f, ok := Extract(text)
	if !ok {
		return Result{Issues: []Issue{{Message: "no JSON " + s.Kind() + " was found"}}}
	}
	v := Coerce(f.Value, s.subset)
	return Result{Value: v, Found: f, Issues: s.Validate(v)}
}

// FixPromptFull asks a model to send the JSON again, naming what was wrong
// against the whole schema.
func FixPromptFull(r Result, s *FullSchema) string {
	return fixPrompt(r, string(s.Raw))
}
