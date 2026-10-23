package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"mime"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/mux"
	"gopkg.in/yaml.v3"
)

// The SDKs are generated from api/openapi.yaml (toskar-core#666), so the
// spec has to say what the daemon really sends. Every response this
// package's tests see through Server.Handler is checked against it: the
// status is documented, a body has a described content type, and a JSON
// body has the described types and required fields and no field the spec
// leaves out.
//
// What doesn't match yet is listed in testdata/openapi_drift.txt. A new
// mismatch fails the tests; fix the spec (or the handler) rather than
// adding it to the list. UPDATE_OPENAPI_DRIFT=1 rewrites the list from a
// full run, once entries are fixed.

const driftFile = "testdata/openapi_drift.txt"

var specResponses = struct {
	sync.Mutex
	spec  map[string]any
	drift map[string]bool
	// reached is every operation a test got a successful answer from.
	reached map[string]bool
}{drift: map[string]bool{}, reached: map[string]bool{}}

func watchOpenAPIResponses() error {
	raw, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		return err
	}
	var spec map[string]any
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		return err
	}
	specResponses.spec = normalize(spec).(map[string]any)
	wrapHandler = func(router *mux.Router) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var match mux.RouteMatch
			template := ""
			if router.Match(r, &match) && match.Route != nil {
				template, _ = match.Route.GetPathTemplate()
			}
			rec := &teeWriter{ResponseWriter: w, status: http.StatusOK}
			router.ServeHTTP(rec, r)
			if template == "" {
				return
			}
			drift := checkResponse(specResponses.spec, strings.ToLower(r.Method), template, rec.status, rec.Header().Get("Content-Type"), rec.body.Bytes())
			specResponses.Lock()
			defer specResponses.Unlock()
			if rec.status >= 200 && rec.status < 300 {
				specResponses.reached[strings.ToLower(r.Method)+" "+template] = true
			}
			for _, d := range drift {
				specResponses.drift[d] = true
			}
		})
	}
	return nil
}

// checkOpenAPIDrift compares what the tests saw with the recorded drift,
// and returns the exit code.
func checkOpenAPIDrift() int {
	seen := make([]string, 0, len(specResponses.drift))
	for d := range specResponses.drift {
		seen = append(seen, d)
	}
	sort.Strings(seen)
	if os.Getenv("OPENAPI_REACHED") != "" {
		fmt.Fprintf(os.Stderr, "openapi: the tests got a successful answer from %d operations\n", len(specResponses.reached))
	}
	if os.Getenv("UPDATE_OPENAPI_DRIFT") != "" {
		text := ""
		if len(seen) > 0 {
			text = strings.Join(seen, "\n") + "\n"
		}
		if err := os.WriteFile(driftFile, []byte(text), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	known := map[string]bool{}
	if raw, err := os.ReadFile(driftFile); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				known[line] = true
			}
		}
	}
	var added []string
	for _, d := range seen {
		if !known[d] {
			added = append(added, d)
		}
	}
	if len(added) == 0 {
		return 0
	}
	fmt.Fprintf(os.Stderr, "Responses that api/openapi.yaml doesn't describe (toskar-core#666); describe them in the spec:\n  %s\n", strings.Join(added, "\n  "))
	return 1
}

// teeWriter keeps a copy of the response, and still streams.
type teeWriter struct {
	http.ResponseWriter
	status  int
	written bool
	body    bytes.Buffer
}

func (t *teeWriter) WriteHeader(status int) {
	if !t.written {
		t.status, t.written = status, true
	}
	t.ResponseWriter.WriteHeader(status)
}

func (t *teeWriter) Write(b []byte) (int, error) {
	t.written = true
	if t.body.Len() < 1<<20 {
		t.body.Write(b)
	}
	return t.ResponseWriter.Write(b)
}

func (t *teeWriter) Flush() {
	if f, ok := t.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (t *teeWriter) Unwrap() http.ResponseWriter { return t.ResponseWriter }

// checkResponse returns what doesn't match the spec, one line each, as
// "get /api/v1/x 200: $.field: what". Error responses (4xx and 5xx) all
// share the error shape, so only successful ones are checked.
func checkResponse(spec map[string]any, method, template string, status int, contentType string, body []byte) []string {
	if status < 200 || status >= 300 {
		return nil
	}
	where := fmt.Sprintf("%s %s %d", method, template, status)
	op, _ := dig(spec, "paths", template, method).(map[string]any)
	if op == nil {
		return nil // TestOpenAPICoversEveryRoute reports it
	}
	responses, _ := op["responses"].(map[string]any)
	resp, _ := responses[strconv.Itoa(status)].(map[string]any)
	if resp == nil {
		return []string{where + ": status not documented"}
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	content, _ := resp["content"].(map[string]any)
	if content == nil {
		return []string{where + ": body not described"}
	}
	media, _, _ := mime.ParseMediaType(contentType)
	described, _ := content[media].(map[string]any)
	if described == nil {
		// A media range, such as image/* or */* for a file served with
		// its own type.
		kind, _, _ := strings.Cut(media, "/")
		if described, _ = content[kind+"/*"].(map[string]any); described == nil {
			described, _ = content["*/*"].(map[string]any)
		}
	}
	if described == nil {
		return []string{fmt.Sprintf("%s: content type %s not described", where, media)}
	}
	if media == "text/event-stream" {
		return checkStream(spec, described, body, where)
	}
	if media != "application/json" {
		return nil
	}
	schema, _ := described["schema"].(map[string]any)
	if schema == nil {
		return []string{where + ": no schema"}
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return []string{where + ": body isn't JSON"}
	}
	var out []string
	for _, d := range checkValue(spec, schema, value, "$", 0) {
		out = append(out, where+": "+d)
	}
	return out
}

// checkStream checks a stream of server-sent events against the media
// type's x-events: each event's name, and its data against that event's
// schema ("*" for any name). OpenAPI 3.0 has no way to describe the
// events, so the spec names them in this extension.
func checkStream(spec, described map[string]any, body []byte, where string) []string {
	events := asMap(described["x-events"])
	if events == nil {
		return []string{where + ": stream events not described (x-events)"}
	}
	var out []string
	seen := map[string]bool{}
	for _, e := range parseSSE(body) {
		schema := asMap(events[e.name])
		if schema == nil {
			schema = asMap(events["*"])
		}
		var drift []string
		if schema == nil {
			drift = []string{"event " + e.name + ": not described"}
		} else if resolve(spec, schema)["type"] != "string" {
			var value any
			if err := json.Unmarshal([]byte(e.data), &value); err != nil {
				drift = []string{"event " + e.name + ": data isn't JSON"}
			} else {
				for _, d := range checkValue(spec, schema, value, "$", 0) {
					drift = append(drift, "event "+e.name+": "+d)
				}
			}
		}
		for _, d := range drift {
			if !seen[d] {
				seen[d] = true
				out = append(out, where+": "+d)
			}
		}
	}
	return out
}

type sseEvent struct{ name, data string }

// parseSSE reads server-sent events: blocks of "event:" and "data:" lines,
// with comments (":") left out. An event without a name is "message".
func parseSSE(body []byte) []sseEvent {
	var out []sseEvent
	for _, block := range strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n\n") {
		e := sseEvent{name: "message"}
		var data []string
		for _, line := range strings.Split(block, "\n") {
			field, value, _ := strings.Cut(line, ":")
			value = strings.TrimPrefix(value, " ")
			switch field {
			case "event":
				e.name = value
			case "data":
				data = append(data, value)
			}
		}
		if data == nil {
			continue
		}
		e.data = strings.Join(data, "\n")
		out = append(out, e)
	}
	return out
}

// checkValue checks value against schema. Arrays are checked item by item
// but reported as [] so one missing field is one line, not one per item.
func checkValue(spec, schema map[string]any, value any, at string, depth int) []string {
	if depth > 32 {
		return nil
	}
	schema = resolve(spec, schema)
	if value == nil {
		if schema["nullable"] == true || len(schema) == 0 {
			return nil
		}
		if _, typed := schema["type"]; !typed {
			return nil
		}
		return []string{at + ": null, but not nullable"}
	}
	for _, key := range []string{"oneOf", "anyOf"} {
		if options, ok := schema[key].([]any); ok {
			for _, o := range options {
				if sub, ok := o.(map[string]any); ok && len(checkValue(spec, sub, value, at, depth+1)) == 0 {
					return nil
				}
			}
			return []string{at + ": matches none of " + key}
		}
	}
	if parts, ok := schema["allOf"].([]any); ok {
		merged := map[string]any{"type": "object", "properties": map[string]any{}}
		var required []any
		for _, p := range parts {
			sub, _ := p.(map[string]any)
			sub = resolve(spec, sub)
			for k, v := range asMap(sub["properties"]) {
				merged["properties"].(map[string]any)[k] = v
			}
			required = append(required, asSlice(sub["required"])...)
			if extra, ok := sub["additionalProperties"]; ok {
				merged["additionalProperties"] = extra
			}
		}
		merged["required"] = required
		schema = merged
	}

	switch schema["type"] {
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			return []string{fmt.Sprintf("%s: %s, but described as an object", at, kind(value))}
		}
		props := asMap(schema["properties"])
		extra, hasExtra := schema["additionalProperties"]
		if len(props) == 0 && !hasExtra {
			return []string{at + ": an object with no described fields"}
		}
		var out []string
		for _, r := range asSlice(schema["required"]) {
			if name, _ := r.(string); name != "" {
				if _, present := obj[name]; !present {
					out = append(out, fmt.Sprintf("%s.%s: required, but missing", at, name))
				}
			}
		}
		for name, v := range obj {
			if sub, described := props[name].(map[string]any); described {
				out = append(out, checkValue(spec, sub, v, at+"."+name, depth+1)...)
				continue
			}
			switch e := extra.(type) {
			case map[string]any:
				out = append(out, checkValue(spec, e, v, at+".*", depth+1)...)
			case bool:
				if !e {
					out = append(out, fmt.Sprintf("%s.%s: not allowed", at, name))
				}
			default:
				out = append(out, fmt.Sprintf("%s.%s: not described", at, name))
			}
		}
		return out
	case "array":
		list, ok := value.([]any)
		if !ok {
			return []string{fmt.Sprintf("%s: %s, but described as an array", at, kind(value))}
		}
		items, _ := schema["items"].(map[string]any)
		seen := map[string]bool{}
		var out []string
		for _, item := range list {
			for _, d := range checkValue(spec, items, item, at+"[]", depth+1) {
				if !seen[d] {
					seen[d] = true
					out = append(out, d)
				}
			}
		}
		return out
	case "string":
		if _, ok := value.(string); !ok {
			return []string{fmt.Sprintf("%s: %s, but described as a string", at, kind(value))}
		}
	case "integer":
		n, ok := value.(float64)
		if !ok || n != math.Trunc(n) {
			return []string{fmt.Sprintf("%s: %s, but described as an integer", at, kind(value))}
		}
	case "number":
		if _, ok := value.(float64); !ok {
			return []string{fmt.Sprintf("%s: %s, but described as a number", at, kind(value))}
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return []string{fmt.Sprintf("%s: %s, but described as a boolean", at, kind(value))}
		}
	}
	return nil
}

func resolve(spec, schema map[string]any) map[string]any {
	for i := 0; i < 8 && schema != nil; i++ {
		ref, _ := schema["$ref"].(string)
		if ref == "" {
			return schema
		}
		parts := strings.Split(strings.TrimPrefix(ref, "#/"), "/")
		path := make([]string, len(parts))
		copy(path, parts)
		next, _ := dig(spec, path...).(map[string]any)
		schema = next
	}
	if schema == nil {
		return map[string]any{}
	}
	return schema
}

func dig(v any, path ...string) any {
	for _, p := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[p]
	}
	return v
}

// normalize turns the map keys YAML decodes as numbers (status codes)
// into strings.
func normalize(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, inner := range x {
			x[k] = normalize(inner)
		}
		return x
	case map[any]any:
		out := make(map[string]any, len(x))
		for k, inner := range x {
			out[fmt.Sprint(k)] = normalize(inner)
		}
		return out
	case []any:
		for i := range x {
			x[i] = normalize(x[i])
		}
	}
	return v
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

func kind(v any) string {
	switch v.(type) {
	case map[string]any:
		return "an object"
	case []any:
		return "an array"
	case string:
		return "a string"
	case float64:
		return "a number"
	case bool:
		return "a boolean"
	}
	return "null"
}

// The stream check reads server-sent events as clients do, and holds each
// to the spec's x-events.
func TestOpenAPIStreamCheck(t *testing.T) {
	spec := specResponses.spec
	stream := "event: token\ndata: {\"content\":\"Hel\"}\n\n: ping\n\nevent: token\ndata: {\"content\":\"lo\"}\n\nevent: done\ndata: {}\n\n"
	if d := checkResponse(spec, "post", "/api/v1/chat", 200, "text/event-stream", []byte(stream)); len(d) > 0 {
		t.Errorf("a well-formed chat stream: %v", d)
	}
	bad := "event: token\ndata: {\"text\":\"Hi\"}\n\nevent: thinking\ndata: {}\n\n"
	got := checkResponse(spec, "post", "/api/v1/chat", 200, "text/event-stream", []byte(bad))
	want := []string{
		"post /api/v1/chat 200: event token: $.content: required, but missing",
		"post /api/v1/chat 200: event token: $.text: not described",
		"post /api/v1/chat 200: event thinking: not described",
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("a stream that doesn't match:\n got  %q\n want %q", got, want)
	}
	// Any event name on /events is an Event.
	events := "event: model.unloaded\ndata: {}\n\n"
	for _, d := range checkResponse(spec, "get", "/api/v1/events", 200, "text/event-stream", []byte(events)) {
		if strings.Contains(d, "not described") {
			t.Errorf("an event stream: %s", d)
		}
	}
}
