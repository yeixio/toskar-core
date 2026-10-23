package api

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/yeixio/toskar-core/internal/auth"
)

// The named schemas in api/openapi.yaml that stand for a Go type, so the
// spec's fields follow the type's (toskar-core#666): same names, same JSON
// types, and required exactly when the field isn't omitempty. A schema not
// listed here is written by hand.
var specTypes = map[string]any{
	"Person":             auth.Person{},
	"Principal":          auth.Principal{},
	"Me":                 meView{},
	"Invite":             inviteView{},
	"InviteLink":         linkView{},
	"AddedPerson":        addedPerson{},
	"ActivityEntry":      auth.ActivityEntry{},
	"APIKey":             auth.APIKeyRecord{},
	"RoleCapabilities":   roleCapabilitiesView{},
	"PersonCapabilities": personCapabilitiesView{},
	"ProfilePins":        profilePinsView{},
	"OIDCInfo":           oidcInfo{},
	"Capabilities":       auth.Capabilities{},
}

// The Go types that encode themselves, with the schema of what they send.
var encodedAs = map[reflect.Type]map[string]any{
	reflect.TypeOf(time.Time{}):       {"type": "string", "format": "date-time"},
	reflect.TypeOf(time.Duration(0)):  {"type": "integer", "format": "int64"},
	reflect.TypeOf(json.RawMessage{}): {},
}

// schemaOf is the schema of what encoding/json makes of t. Types named in
// specTypes become references, except at the top.
func schemaOf(t reflect.Type, top bool) map[string]any {
	if s, ok := encodedAs[t]; ok {
		return s
	}
	if !top {
		for name, v := range specTypes {
			if reflect.TypeOf(v) == t {
				return map[string]any{"$ref": "#/components/schemas/" + name}
			}
		}
	}
	switch t.Kind() {
	case reflect.Pointer:
		s := map[string]any{}
		for k, v := range schemaOf(t.Elem(), false) {
			s[k] = v
		}
		if _, ref := s["$ref"]; ref {
			return map[string]any{"allOf": []any{s}, "nullable": true}
		}
		s["nullable"] = true
		return s
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return map[string]any{"type": "integer"}
	case reflect.Int64, reflect.Uint64:
		return map[string]any{"type": "integer", "format": "int64"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return map[string]any{"type": "string", "format": "byte"}
		}
		return map[string]any{"type": "array", "items": schemaOf(t.Elem(), false)}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": schemaOf(t.Elem(), false)}
	case reflect.Interface:
		return map[string]any{}
	case reflect.Struct:
		props := map[string]any{}
		var required []string
		addFields(t, props, &required)
		s := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			sort.Strings(required)
			s["required"] = required
		}
		return s
	}
	panic(fmt.Sprintf("schemaOf: no schema for %s", t))
}

// addFields adds a struct's JSON fields, with embedded structs' fields
// flattened in as encoding/json does.
func addFields(t reflect.Type, props map[string]any, required *[]string) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" || (!f.IsExported() && !f.Anonymous) {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" {
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				addFields(ft, props, required)
				continue
			}
		}
		if name == "" {
			name = f.Name
		}
		omitted := strings.Contains(opts, "omitempty") || strings.Contains(opts, "omitzero")
		if f.Type.Kind() == reflect.Pointer && omitted {
			// A nil pointer is left out, never sent as null.
			props[name] = schemaOf(f.Type.Elem(), false)
		} else {
			props[name] = schemaOf(f.Type, false)
		}
		if !omitted {
			*required = append(*required, name)
		}
	}
}

// The spec's schema for each Go type is the type's, apart from what only
// the spec says: descriptions, examples, enums, and formats on strings.
func TestOpenAPISchemasMatchGo(t *testing.T) {
	_, raw := loadOpenAPI(t)
	var spec map[string]any
	if err := yaml.Unmarshal([]byte(raw), &spec); err != nil {
		t.Fatal(err)
	}
	spec = normalize(spec).(map[string]any)
	schemas := asMap(dig(spec, "components", "schemas"))
	for name, v := range specTypes {
		got := asMap(schemas[name])
		if got == nil {
			t.Errorf("components/schemas/%s is missing; OPENAPI_PRINT=%s go test ./internal/api -run TestOpenAPIPrint prints it", name, name)
			continue
		}
		for _, d := range compareSchema(schemaOf(reflect.TypeOf(v), true), got, name) {
			t.Error(d)
		}
	}
}

// compareSchema reports where the spec's schema differs from the Go type's.
func compareSchema(want, got map[string]any, at string) []string {
	if ref, _ := want["$ref"].(string); ref != "" {
		if got["$ref"] != ref {
			return []string{fmt.Sprintf("%s: should be %s", at, ref)}
		}
		return nil
	}
	if all, ok := want["allOf"].([]any); ok {
		gotAll, _ := got["allOf"].([]any)
		if len(gotAll) != 1 || got["nullable"] != true {
			return []string{fmt.Sprintf("%s: should be allOf [%s] with nullable: true", at, asMap(all[0])["$ref"])}
		}
		return compareSchema(asMap(all[0]), asMap(gotAll[0]), at)
	}
	var out []string
	if want["type"] != got["type"] {
		out = append(out, fmt.Sprintf("%s: type %v, but the Go type is %v", at, got["type"], want["type"]))
	}
	if want["nullable"] == true && got["nullable"] != true {
		out = append(out, at+": should be nullable")
	}
	if f, ok := want["format"]; ok && got["format"] != f {
		out = append(out, fmt.Sprintf("%s: format %v, but the Go type is %v", at, got["format"], f))
	}
	switch want["type"] {
	case "array":
		out = append(out, compareSchema(asMap(want["items"]), asMap(got["items"]), at+"[]")...)
	case "object":
		if extra, ok := want["additionalProperties"]; ok {
			out = append(out, compareSchema(asMap(extra), asMap(got["additionalProperties"]), at+".*")...)
		} else if _, loose := got["additionalProperties"]; loose {
			out = append(out, at+": additionalProperties, but the Go type has only its fields")
		}
		wantProps, gotProps := asMap(want["properties"]), asMap(got["properties"])
		for name, w := range wantProps {
			g, ok := gotProps[name]
			if !ok {
				out = append(out, fmt.Sprintf("%s.%s: missing", at, name))
				continue
			}
			out = append(out, compareSchema(asMap(w), asMap(g), at+"."+name)...)
		}
		for name := range gotProps {
			if _, ok := wantProps[name]; !ok {
				out = append(out, fmt.Sprintf("%s.%s: the Go type has no such field", at, name))
			}
		}
		wantReq, gotReq := map[string]bool{}, map[string]bool{}
		for _, r := range asSlice(want["required"]) {
			wantReq[fmt.Sprint(r)] = true
		}
		for _, r := range asSlice(got["required"]) {
			gotReq[fmt.Sprint(r)] = true
		}
		if ws, ok := want["required"].([]string); ok {
			for _, r := range ws {
				wantReq[r] = true
			}
		}
		for r := range wantReq {
			if !gotReq[r] {
				out = append(out, fmt.Sprintf("%s.%s: always sent, so required", at, r))
			}
		}
		for r := range gotReq {
			if !wantReq[r] {
				out = append(out, fmt.Sprintf("%s.%s: omitted when empty, so not required", at, r))
			}
		}
	}
	sort.Strings(out)
	return out
}

// TestOpenAPIPrint prints the schemas named in OPENAPI_PRINT (comma
// separated) as YAML to paste into api/openapi.yaml, as a start.
func TestOpenAPIPrint(t *testing.T) {
	names := os.Getenv("OPENAPI_PRINT")
	if names == "" {
		t.Skip("set OPENAPI_PRINT")
	}
	for _, name := range strings.Split(names, ",") {
		v, ok := specTypes[name]
		if !ok {
			t.Fatalf("%s isn't in specTypes", name)
		}
		out, _ := yaml.Marshal(map[string]any{name: schemaOf(reflect.TypeOf(v), true)})
		fmt.Print(string(out))
	}
}
