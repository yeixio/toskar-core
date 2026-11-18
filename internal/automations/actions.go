package automations

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Action is a call an automation may make on its own (#586): a tool, and
// the arguments that must match, such as Home Assistant's call with domain
// light, service turn_on, and entity_id light.porch. A call that matches
// runs without asking, even in a run started by what a trigger delivered;
// any other call that changes things is skipped, as before. What a trigger
// delivered can't add to the list.
type Action struct {
	Tool string `json:"tool"`
	// Args must each equal the call's argument of that name, ignoring case
	// and spaces at the ends. An argument not listed may be anything, so an
	// action lists every one that matters.
	Args map[string]string `json:"args,omitempty"`
}

const (
	// MaxActions is how many actions one automation may list.
	MaxActions = 20
	maxArgs    = 8
)

var actionToolRe = regexp.MustCompile(`^[a-z][a-z0-9_-]*(\.[a-z0-9_-]+)+$`)
var actionArgRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

// HomeAssistantCall is the tool that calls a Home Assistant service.
const HomeAssistantCall = "homeassistant.call"

// Clean trims an action's tool and arguments.
func (a Action) Clean() Action {
	out := Action{Tool: strings.TrimSpace(a.Tool)}
	if len(a.Args) > 0 {
		out.Args = map[string]string{}
		for k, v := range a.Args {
			out.Args[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

// Validate checks an action can be matched. A Home Assistant call names
// its service and device, so an action can't allow every service at once.
func (a Action) Validate() error {
	if !actionToolRe.MatchString(a.Tool) {
		return fmt.Errorf("an action's tool is a tool ID such as %s, not %q", HomeAssistantCall, a.Tool)
	}
	if len(a.Args) > maxArgs {
		return fmt.Errorf("an action matches %d arguments at most", maxArgs)
	}
	for k, v := range a.Args {
		if !actionArgRe.MatchString(k) {
			return fmt.Errorf("%q isn't an argument name", k)
		}
		if v == "" || len(v) > 200 {
			return fmt.Errorf("the action's %s must be 1 to 200 characters", k)
		}
	}
	if a.Tool == HomeAssistantCall {
		args := haArgs(a.Args)
		if args["service"] == "" || args["entity_id"] == "" {
			return fmt.Errorf("a Home Assistant action names its service and device, such as light.turn_on on light.porch")
		}
	}
	return nil
}

// ValidateActions checks an automation's actions.
func ValidateActions(list []Action) error {
	if len(list) > MaxActions {
		return fmt.Errorf("an automation may list %d actions at most", MaxActions)
	}
	for _, a := range list {
		if err := a.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// haArgs reads a Home Assistant call the way the connector does: a service
// such as light.turn_on carries its domain, and a missing domain comes
// from the device.
func haArgs(args map[string]string) map[string]string {
	domain, service, entity := strings.ToLower(args["domain"]), strings.ToLower(args["service"]), strings.ToLower(args["entity_id"])
	if d, s, ok := strings.Cut(service, "."); ok {
		domain, service = d, s
	}
	if domain == "" {
		domain, _, _ = strings.Cut(entity, ".")
	}
	out := map[string]string{}
	for k, v := range args {
		out[k] = strings.ToLower(v)
	}
	out["domain"], out["service"], out["entity_id"] = domain, service, entity
	return out
}

func stringArgs(args map[string]any) map[string]string {
	out := map[string]string{}
	for k, v := range args {
		switch x := v.(type) {
		case string:
			out[k] = strings.TrimSpace(x)
		case nil:
		default:
			out[k] = strings.TrimSpace(fmt.Sprint(x))
		}
	}
	return out
}

// Matches reports a call the action allows.
func (a Action) Matches(toolID string, args map[string]any) bool {
	if a.Tool != toolID {
		return false
	}
	call, want := stringArgs(args), a.Args
	if toolID == HomeAssistantCall {
		call, want = haArgs(call), haArgs(want)
		// Home Assistant takes one device per action: a list of them, or
		// "all", isn't the device the action names.
		if strings.ContainsAny(call["entity_id"], ", ") || call["entity_id"] == "all" {
			return false
		}
	}
	for k, v := range want {
		if !strings.EqualFold(call[k], v) {
			return false
		}
	}
	return true
}

// Allows reports whether one of the actions allows the call.
func Allows(list []Action, toolID string, args map[string]any) bool {
	for _, a := range list {
		if a.Matches(toolID, args) {
			return true
		}
	}
	return false
}

// ActionTools are the tools the actions name, sorted, once each.
func ActionTools(list []Action) []string {
	seen := map[string]bool{}
	var out []string
	for _, a := range list {
		if !seen[a.Tool] {
			seen[a.Tool] = true
			out = append(out, a.Tool)
		}
	}
	sort.Strings(out)
	return out
}

// Describe is an action in words for the run's instructions, such as
// "homeassistant.call with domain light, entity_id light.porch, service
// turn_on".
func (a Action) Describe() string {
	if len(a.Args) == 0 {
		return a.Tool + " (any arguments)"
	}
	keys := make([]string, 0, len(a.Args))
	for k := range a.Args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+" "+a.Args[k])
	}
	return a.Tool + " with " + strings.Join(parts, ", ")
}
