package tools

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Native tools are for a model whose calls llama.cpp reads only when the
// request offers its tools itself, as gpt-oss's harmony format (#1099).
// The tools come from the system prompt PromptFor wrote, so a request
// offers them exactly when the prompt does, and the call that comes back
// is turned into this package's own protocol, so the turn reads it as any
// other call.

// NativeFromPrompt is the tools a system prompt from PromptFor lists, in a
// chat completions request's form, or nil when it lists none.
func NativeFromPrompt(system string) []map[string]any {
	_, list, ok := strings.Cut(system, "\nAvailable tools:\n")
	if !ok {
		return nil
	}
	var out []map[string]any
	for _, line := range strings.Split(list, "\n") {
		rest, ok := strings.CutPrefix(line, "- ")
		if !ok {
			break
		}
		id, rest, ok := strings.Cut(rest, " ")
		if !ok || id == "" {
			continue
		}
		dec := json.NewDecoder(strings.NewReader(rest))
		var schema json.RawMessage
		if dec.Decode(&schema) != nil {
			continue
		}
		description := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest[dec.InputOffset():]), "—"))
		out = append(out, map[string]any{"type": "function", "function": map[string]any{
			"name": id, "description": description, "parameters": nativeParameters(schema),
		}})
	}
	return out
}

// nativeParameters is a tool's schema as JSON Schema: a full one as it is,
// and the short form the prompt uses, {"path":"string"}, as an object with
// those properties. A value that isn't a type, such as "owner/name", is a
// string shaped like it.
//
// The properties keep the order they're written in (#1112). llama.cpp holds
// a call to its schema's order, and with properties sorted, content before
// path, gpt-oss's write calls came back as {"path":"tally.go,"}.
func nativeParameters(schema json.RawMessage) json.RawMessage {
	var full struct {
		Properties json.RawMessage `json:"properties"`
	}
	if json.Unmarshal(schema, &full) == nil && full.Properties != nil {
		return json.RawMessage(bytes.TrimSpace(schema))
	}
	var out bytes.Buffer
	out.WriteString(`{"type":"object","properties":{`)
	dec := json.NewDecoder(bytes.NewReader(schema))
	if start, err := dec.Token(); err == nil && start == json.Delim('{') {
		for n := 0; dec.More(); n++ {
			key, err := dec.Token()
			if err != nil {
				break
			}
			var v any
			if dec.Decode(&v) != nil {
				break
			}
			kind, _ := v.(string)
			property := map[string]any{"type": "string", "description": kind}
			switch kind {
			case "string", "integer", "number", "boolean", "array", "object":
				property = map[string]any{"type": kind}
			}
			name, _ := json.Marshal(key)
			value, _ := json.Marshal(property)
			if n > 0 {
				out.WriteByte(',')
			}
			out.Write(name)
			out.WriteByte(':')
			out.Write(value)
		}
	}
	out.WriteString(`}}`)
	return json.RawMessage(out.Bytes())
}

// CallText is a native tool call, a name and its arguments as JSON, in the
// protocol ParseModelOutput reads.
func CallText(name, arguments string) string {
	args := json.RawMessage(bytes.TrimSpace([]byte(arguments)))
	var obj map[string]any
	if json.Unmarshal(args, &obj) != nil {
		args = json.RawMessage(`{}`)
	}
	b, _ := json.Marshal(map[string]any{"tool_call": map[string]any{"id": name, "args": args}})
	return string(b)
}
