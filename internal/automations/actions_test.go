package automations

import (
	"strings"
	"testing"
)

func TestActionValidate(t *testing.T) {
	good := []Action{
		{Tool: "homeassistant.call", Args: map[string]string{"service": "light.turn_on", "entity_id": "light.porch"}},
		{Tool: "homeassistant.call", Args: map[string]string{"domain": "scene", "service": "turn_on", "entity_id": "scene.evening"}},
		{Tool: "github.comment", Args: map[string]string{"repo": "o/r"}},
	}
	if err := ValidateActions(good); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Action{
		{Tool: "homeassistant.call", Args: map[string]string{"entity_id": "light.porch"}},
		{Tool: "homeassistant.call", Args: map[string]string{"service": "light.turn_on"}},
		{Tool: "terminal"},
		{Tool: "x.y", Args: map[string]string{"Bad Name": "1"}},
		{Tool: "x.y", Args: map[string]string{"a": ""}},
	} {
		if bad.Validate() == nil {
			t.Errorf("%+v passed", bad)
		}
	}
	many := make([]Action, MaxActions+1)
	for i := range many {
		many[i] = good[2]
	}
	if ValidateActions(many) == nil {
		t.Error("too many passed")
	}
}

func TestActionMatches(t *testing.T) {
	a := Action{Tool: "github.comment", Args: map[string]string{"repo": "o/r"}}
	if !a.Matches("github.comment", map[string]any{"repo": " O/R ", "body": "anything"}) {
		t.Error("same repo")
	}
	if a.Matches("github.comment", map[string]any{"repo": "o/other"}) || a.Matches("github.issue", map[string]any{"repo": "o/r"}) {
		t.Error("other repo or tool")
	}
	if a.Matches("github.comment", map[string]any{}) {
		t.Error("missing argument")
	}
	num := Action{Tool: "x.set", Args: map[string]string{"level": "40"}}
	if !num.Matches("x.set", map[string]any{"level": float64(40)}) {
		t.Error("a number argument")
	}
	if got := ActionTools([]Action{a, num, a}); strings.Join(got, ",") != "github.comment,x.set" {
		t.Errorf("ActionTools = %v", got)
	}
}
