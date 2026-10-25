package structured

import (
	"strings"
	"testing"
)

// The caller's schema is kept whole (#831): every keyword reaches the
// runtime and is checked, not only type, properties, required, enum, and
// items.
func TestFullSchemaKeepsEveryKeyword(t *testing.T) {
	raw := `{"anyOf": [{"type": "object", "properties": {"n": {"type": "integer", "minimum": 1, "maximum": 5}}, "required": ["n"], "additionalProperties": false},
		{"$ref": "#/$defs/word"}], "$defs": {"word": {"type": "string", "minLength": 2, "pattern": "^[a-z]+$"}}}`
	s, err := Compile([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	for _, kw := range []string{"anyOf", "$ref", "$defs", "minimum", "maximum", "additionalProperties", "minLength", "pattern"} {
		if !strings.Contains(string(s.Raw), kw) {
			t.Errorf("%s was dropped from the schema the runtime gets: %s", kw, s.Raw)
		}
	}
	cases := []struct {
		value any
		ok    bool
	}{
		{map[string]any{"n": 3}, true},
		{"juneau", true},
		{map[string]any{"n": 9}, false},             // maximum
		{map[string]any{"n": 3, "extra": 1}, false}, // additionalProperties
		{map[string]any{}, false},                   // required
		{"J", false},                                // minLength and pattern, through $ref
		{42, false},                                 // neither branch of anyOf
	}
	for _, c := range cases {
		issues := s.Validate(c.value)
		if (len(issues) == 0) != c.ok {
			t.Errorf("%v: ok=%v, issues %v", c.value, len(issues) == 0, issues)
		}
	}
}

func TestFullSchemaAtEachLevel(t *testing.T) {
	s := MustCompile(`{"type": "object", "properties": {"labels": {"type": "array", "items": {"enum": ["bug", "docs"]}},
		"severity": {"const": "high"}, "score": {"type": "number", "exclusiveMinimum": 0}}, "required": ["labels"]}`)
	if issues := s.Validate(map[string]any{"labels": []any{"bug"}, "severity": "high", "score": 0.5}); len(issues) != 0 {
		t.Fatalf("a fitting value: %v", issues)
	}
	issues := s.Validate(map[string]any{"labels": []any{"wontfix"}, "severity": "low", "score": 0})
	paths := map[string]bool{}
	for _, i := range issues {
		paths[i.Path] = true
	}
	for _, p := range []string{"labels.0", "severity", "score"} {
		if !paths[p] {
			t.Errorf("no issue at %s: %v", p, issues)
		}
	}
	// The repair names every problem, and the schema as sent.
	r := ParseFull(`{"labels": ["wontfix"], "severity": "low", "score": 0}`, s)
	fix := FixPromptFull(r, s)
	if r.OK() || !strings.Contains(fix, "labels.0") || !strings.Contains(fix, `"const":"high"`) {
		t.Fatalf("fix prompt: %s", fix)
	}
}

func TestCompileRefuses(t *testing.T) {
	for name, raw := range map[string]string{
		"not JSON":             `{"type": "object"`,
		"not JSON Schema":      `{"type": 5}`,
		"an outside reference": `{"$ref": "https://example.com/schema.json"}`,
		"a file reference":     `{"$ref": "file:///etc/passwd"}`,
		"too big":              `{"description": "` + strings.Repeat("x", MaxSchemaBytes) + `"}`,
	} {
		if _, err := Compile([]byte(raw)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
