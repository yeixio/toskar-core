package tools

import (
	"errors"
	"testing"
)

func TestCheckArgs(t *testing.T) {
	issue := Definition{ID: "github.issue", Schema: `{"repo":"owner/name","number":"integer"}`}
	got, err := CheckArgs(issue, map[string]any{"repo": "o/r", "number": "7"})
	if err != nil || got["number"] != float64(7) || got["repo"] != "o/r" {
		t.Fatalf("repaired = %+v, %v", got, err)
	}
	if _, err := CheckArgs(issue, map[string]any{"repo": "o/r", "number": "seven"}); !errors.Is(err, ErrInvalidArgs) || ErrorKind(err) != ErrKindInvalid {
		t.Fatalf("bad number: %v (%s)", err, ErrorKind(err))
	}
	create := Definition{ID: "files.create", Schema: `{"name":"string","content":"string"}`}
	got, err = CheckArgs(create, map[string]any{"name": "data.json", "content": map[string]any{"a": 1}})
	if err != nil || got["content"] != `{"a":1}` {
		t.Fatalf("content = %+v, %v", got, err)
	}
	if got, err := CheckArgs(Definition{Schema: `{}`}, map[string]any{"x": 1}); err != nil || got["x"] != 1 {
		t.Fatal("a tool without a schema changed")
	}
}
