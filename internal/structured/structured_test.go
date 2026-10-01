package structured

import (
	"strings"
	"testing"
)

var price = Object(map[string]string{"price": "number", "currency": "string"})

func TestParseRepairsAndValidates(t *testing.T) {
	for name, tc := range map[string]struct {
		text   string
		ok     bool
		price  float64
		prose  string
		issues string
	}{
		"plain":            {text: `The laptop is $420.` + "\n" + `{"price": 420, "currency": "USD"}`, ok: true, price: 420, prose: "The laptop is $420."},
		"fenced":           {text: "Here it is.\n```json\n{\"price\": 1299.5, \"currency\": \"USD\"}\n```", ok: true, price: 1299.5, prose: "Here it is."},
		"price as text":    {text: `{"price": "$1,299.00", "currency": "USD"}`, ok: true, price: 1299},
		"trailing comma":   {text: `{"price": 420, "currency": "USD",}`, ok: true, price: 420},
		"curly quotes":     {text: `{“price”: 420, “currency”: “USD”}`, ok: true, price: 420},
		"missing field":    {text: `{"price": 420}`, issues: "currency: is required"},
		"wrong type":       {text: `{"price": "about four hundred", "currency": "USD"}`, issues: "price: must be a number, not string"},
		"no json":          {text: `It costs $420.`, issues: "no JSON object was found"},
		"last object wins": {text: `{"price": 1, "currency": "USD"} then {"price": 2, "currency": "USD"}`, ok: true, price: 2},
	} {
		r := Parse(tc.text, price)
		if r.OK() != tc.ok {
			t.Errorf("%s: ok=%v issues=%v", name, r.OK(), r.Issues)
			continue
		}
		if tc.ok {
			if got := r.Value.(map[string]any)["price"]; got != tc.price {
				t.Errorf("%s: price = %v", name, got)
			}
			if tc.prose != "" && Prose(tc.text, r.Found) != tc.prose {
				t.Errorf("%s: prose = %q", name, Prose(tc.text, r.Found))
			}
		} else if !strings.Contains(Describe(r.Issues), tc.issues) {
			t.Errorf("%s: issues = %q", name, Describe(r.Issues))
		}
	}
}

func TestSchemaFeatures(t *testing.T) {
	s, err := ParseSchema([]byte(`{"type":"object","required":["items"],"properties":{
		"status":{"type":"string","enum":["open","closed"]},
		"items":{"type":"array","items":{"type":"object","required":["n"],"properties":{"n":{"type":"integer"},"ok":{"type":"boolean"}}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	r := Parse(`{"status":"pending","items":[{"n":"3","ok":"yes"},{"n":2.5},{}]}`, s)
	got := Describe(r.Issues)
	for _, want := range []string{"items[1].n: must be a whole number, not number", "items[2].n: is required", "status: must be one of [open closed]"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	first := r.Value.(map[string]any)["items"].([]any)[0].(map[string]any)
	if first["n"] != float64(3) || first["ok"] != true {
		t.Errorf("repairs = %+v", first)
	}
	if _, err := ParseSchema([]byte(`{`)); err == nil {
		t.Error("bad schema accepted")
	}
	if fix := FixPrompt(r, s); !strings.Contains(fix, "is required") || !strings.Contains(fix, "JSON Schema") {
		t.Errorf("fix prompt = %q", fix)
	}
}
