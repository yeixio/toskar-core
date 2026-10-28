package api

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/cache"
	"github.com/yeixio/toskar-core/internal/connectors"
	"github.com/yeixio/toskar-core/internal/diagnostics"
	"github.com/yeixio/toskar-core/internal/egress"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/gjallarhorn"
	"github.com/yeixio/toskar-core/internal/inventory"
	"github.com/yeixio/toskar-core/internal/logs"
	"github.com/yeixio/toskar-core/internal/mcp"
	"github.com/yeixio/toskar-core/internal/mimir"
	"github.com/yeixio/toskar-core/internal/models"
	"github.com/yeixio/toskar-core/internal/muninn"
	"github.com/yeixio/toskar-core/internal/news"
	"github.com/yeixio/toskar-core/internal/portals"
	"github.com/yeixio/toskar-core/internal/ratings"
	"github.com/yeixio/toskar-core/internal/remotetools"
	"github.com/yeixio/toskar-core/internal/runlog"
	"github.com/yeixio/toskar-core/internal/runtimes"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/internal/training"
	"github.com/yeixio/toskar-core/internal/updates"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// The named schemas in api/openapi.yaml that stand for a Go type, so the
// spec's fields follow the type's (toskar-core#666): same names, same JSON
// types, and required exactly when the field isn't omitempty. A schema not
// listed here is written by hand.
var specTypes = map[string]any{
	"Person":              auth.Person{},
	"Principal":           auth.Principal{},
	"Me":                  meView{},
	"Invite":              inviteView{},
	"InviteLink":          linkView{},
	"AddedPerson":         addedPerson{},
	"ActivityEntry":       auth.ActivityEntry{},
	"APIKey":              auth.APIKeyRecord{},
	"RoleCapabilities":    roleCapabilitiesView{},
	"PersonCapabilities":  personCapabilitiesView{},
	"ProfilePins":         profilePinsView{},
	"OIDCInfo":            oidcInfo{},
	"Capabilities":        auth.Capabilities{},
	"DevicePairing":       pairingView{},
	"PairedDevice":        pairedView{},
	"DeviceRoute":         routeView{},
	"MyDevice":            myDevice{},
	"ConversationSummary": muninn.Summary{},
	"ChatStopped":         chatStopped{},
	"Error":               contracts.APIError{},

	// The client contract's types (tests/contract), by the same names.
	"Event":                events.Event{},
	"Run":                  runlog.Run{},
	"ModelUse":             runlog.ModelUse{},
	"ToolUse":              runlog.ToolUse{},
	"FirstAnswers":         runlog.FirstAnswers{},
	"FirstAnswersGroup":    runlog.FirstAnswersGroup{},
	"PromptCache":          runlog.PromptCache{},
	"PromptCacheModel":     runlog.PromptCacheModel{},
	"Message":              contracts.Message{},
	"MessageMeta":          contracts.MessageMeta{},
	"Citation":             contracts.Citation{},
	"ActivityStep":         contracts.ActivityStep{},
	"FileRef":              contracts.FileRef{},
	"SetupOffer":           contracts.SetupOffer{},
	"AutomationDraft":      contracts.AutomationDraft{},
	"AutomationRun":        contracts.AutomationRunRef{},
	"ContextUsage":         contracts.ContextUsage{},
	"Conversation":         contracts.Conversation{},
	"ConversationsDeleted": contracts.ConversationsDeleted{},
	"ConversationSkipped":  contracts.ConversationSkipped{},
	"ModelWarmRequest":     contracts.ModelWarmRequest{},
	"ModelWarmResponse":    contracts.ModelWarmResponse{},
	"Artifact":             artifacts.Artifact{},
	"Notification":         gjallarhorn.Notification{},
	"EgressRecord":         egress.Record{},
	"ContractInfo":         contracts.ContractInfo{},
	"VersionResponse":      contracts.VersionResponse{},
	"ErrorBody":            contracts.ErrorBody{},
	"ChatAnswer":           contracts.ChatAnswer{},
	"ChatToken":            contracts.ChatToken{},
	"ChatDone":             contracts.ChatDone{},
	"GenerationMetrics":    contracts.GenerationMetrics{},

	// Models and runtimes.
	"Model":              contracts.Model{},
	"Recommendation":     contracts.Recommendation{},
	"ModelsFit":          contracts.ModelsFitResponse{},
	"ModelFit":           contracts.ModelFit{},
	"CategoryWinner":     contracts.CategoryWinner{},
	"BrowseModel":        contracts.BrowseModel{},
	"RunningModelView":   contracts.RunningModelView{},
	"URLPreview":         models.URLPreview{},
	"FoundModel":         models.FoundModel{},
	"FoundModels":        foundModels{},
	"ImportedModel":      importedModel{},
	"ModelFileDetails":   modelFileDetails{},
	"ModelInstall":       modelInstall{},
	"AddedModel":         addedModelView{},
	"ModelVision":        modelVision{},
	"ModelPlacement":     modelPlacement{},
	"ModelRating":        ratings.View{},
	"RatingStats":        ratings.Stats{},
	"RatingObservations": ratings.Observations{},
	"CommunityRatings":   ratings.Community{},
	"RuntimeInfo":        runtimes.RuntimeInfo{},
	"RuntimeInstall":     runtimeInstall{},
	"OK":                 okView{},

	// Automations, tasks, knowledge, and training.
	"Automation":             automations.Automation{},
	"AutomationInput":        automations.CreateInput{},
	"Schedule":               automations.Schedule{},
	"AutomationNotification": automations.Notification{},
	"AutomationDetail":       automations.Detail{},
	"AutomationRunsPage":     automations.RunsPage{},
	"AutomationRequest":      automations.ParsedRequest{},
	"AutomationPreview":      automations.Preview{},
	"AutomationRunInfo":      automations.Run{},
	"Task":                   contracts.Task{},
	"KnowledgeSource":        mimir.Source{},
	"KnowledgeHit":           mimir.Hit{},
	"TrainingBaseModel":      training.BaseChoice{},
	"TrainingSample":         training.SampleFile{},
	"TrainingMaterial":       training.Material{},
	"TrainingPlan":           training.Plan{},
	"EvalPrompt":             training.EvalPrompt{},

	// Computers, portals, and the rest.
	"NetworkStatus":      NetworkStatus{},
	"JoinResult":         JoinResult{},
	"LeaveResult":        LeaveResult{},
	"JoinTokenCreated":   JoinTokenCreated{},
	"PairingSession":     auth.PairingSession{},
	"TLSInfo":            APITLS{},
	"Portal":             portals.Portal{},
	"PortalConversation": portals.Conversation{},
	"PortalMessage":      portals.Message{},
	"PortalUsage":        portals.Usage{},
	"PortalPage":         portalPage{},
	"EnteredPortal":      enteredGuest{},
	"VisitorLink":        visitorLink{},
	"CacheInfo":          cache.Info{},
	"ConnectorStatus":    connectors.Status{},
	"DiagnosticsReport":  diagnostics.Report{},
	"RuntimeHistory":     diagnostics.RuntimeHistory{},
	"LogFile":            logs.Entry{},
	"LogContent":         logs.Content{},
	"MCPServer":          mcp.View{},
	"News":               news.View{},
	"UpdateStatus":       updates.Status{},

	// Answers that were maps.
	"CapabilityInventory":          inventory.Snapshot{},
	"CapabilityAnswer":             capabilityAnswer{},
	"SourceText":                   textView{},
	"Memory":                       muninn.Memory{},
	"MemoryList":                   memoryList{},
	"NotificationList":             notificationList{},
	"NotificationDestination":      gjallarhorn.Destination{},
	"NotificationDestinationInput": gjallarhorn.DestinationInput{},
	"NotificationDigest":           gjallarhorn.Digest{},
	"QuietHours":                   gjallarhorn.QuietHours{},
	"CreatedDestination":           createdDestination{},
	"DestinationSecret":            destinationSecret{},
	"NewAPIKey":                    newAPIKey{},
	"Status":                       statusView{},
	"HookLink":                     hookLink{},
	"ContinuedRun":                 continuedRun{},
	"HookRun":                      hookRun{},
	"InvitedVisitor":               invitedVisitor{},
	"SpokenText":                   spokenText{},
	"ToolEnabled":                  toolEnabled{},
	"TrainingExamples":             trainingExamples{},
	"Evaluation":                   evaluation{},
	"ClassifiedMaterial":           classifiedMaterial{},
	"TrainingBackend":              training.Backend{},
	"DiagnosticsBundle":            diagnosticsBundle{},
	"Transcription":                Transcription{},
	"ToolActivity":                 tools.Activity{},
	"NodeToolProviders":            remotetools.NodeProviders{},
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
		var required, order []string
		addFieldsOf(t, props, &required, &order, false)
		s := map[string]any{"type": "object", "properties": props, "x-order": order}
		if doc := typeDoc(t); doc != "" {
			s["description"] = doc
		}
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
// addFieldsOf adds t's fields, in order; optional ones (from an embedded
// pointer, which may be nil) are never required.
func addFieldsOf(t reflect.Type, props map[string]any, required, order *[]string, optional bool) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" || (!f.IsExported() && !f.Anonymous) {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" {
			ft, viaPointer := f.Type, false
			if ft.Kind() == reflect.Pointer {
				ft, viaPointer = ft.Elem(), true
			}
			if ft.Kind() == reflect.Struct {
				addFieldsOf(ft, props, required, order, optional || viaPointer)
				continue
			}
		}
		if name == "" {
			name = f.Name
		}
		omitted := strings.Contains(opts, "omitempty") || strings.Contains(opts, "omitzero")
		var field map[string]any
		if f.Type.Kind() == reflect.Pointer && omitted {
			// A nil pointer is left out, never sent as null.
			field = schemaOf(f.Type.Elem(), false)
		} else {
			field = schemaOf(f.Type, false)
		}
		if doc := fieldDoc(t, f.Name, name); doc != "" && field["$ref"] == nil {
			field = copyMap(field)
			field["description"] = doc
		}
		props[name] = field
		*order = append(*order, name)
		if !omitted && !optional {
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
		for _, d := range compareSchema(schemaOf(reflect.TypeOf(v), true), flattenAllOf(spec, got), name) {
			t.Error(d)
		}
	}
}

// flattenAllOf merges a schema written as allOf (another schema and more
// fields) into one, as the Go type is: an embedded struct's fields are its
// own.
func flattenAllOf(spec, schema map[string]any) map[string]any {
	parts, ok := schema["allOf"].([]any)
	if !ok || schema["nullable"] == true {
		return schema
	}
	props := map[string]any{}
	var required []any
	for _, p := range parts {
		part := flattenAllOf(spec, resolve(spec, asMap(p)))
		for k, v := range asMap(part["properties"]) {
			props[k] = v
		}
		required = append(required, asSlice(part["required"])...)
	}
	return map[string]any{"type": "object", "properties": props, "required": required}
}

// Every type in the client contract is a schema of the same name, so what
// tests/contract keeps compatible is what the spec, and the SDKs made from
// it, describe (#666).
func TestOpenAPICoversTheContract(t *testing.T) {
	raw, err := os.ReadFile("../../tests/contract/testdata/contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var snap struct {
		Types map[string][]string `json:"types"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	for name := range snap.Types {
		if _, ok := specTypes[name]; !ok {
			t.Errorf("the contract's %s isn't in specTypes; add it, with its schema in api/openapi.yaml", name)
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
// separated) as YAML for api/openapi.yaml: the Go type's fields in order,
// described by their doc comments, keeping what the spec's schema of that
// name already says (descriptions, enums, examples).
func TestOpenAPIPrint(t *testing.T) {
	names := os.Getenv("OPENAPI_PRINT")
	if names == "" {
		t.Skip("set OPENAPI_PRINT")
	}
	_, raw := loadOpenAPI(t)
	var spec map[string]any
	if err := yaml.Unmarshal([]byte(raw), &spec); err != nil {
		t.Fatal(err)
	}
	spec = normalize(spec).(map[string]any)
	for _, name := range strings.Split(names, ",") {
		v, ok := specTypes[name]
		if !ok {
			t.Fatalf("%s isn't in specTypes", name)
		}
		schema := schemaOf(reflect.TypeOf(v), true)
		keepWritten(schema, asMap(dig(spec, "components", "schemas", name)))
		doc := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: name}, schemaNode(schema),
		}}
		var b strings.Builder
		enc := yaml.NewEncoder(&b)
		enc.SetIndent(2)
		if err := enc.Encode(doc); err != nil {
			t.Fatal(err)
		}
		fmt.Print(b.String())
	}
}

// keepWritten copies what a person wrote in the spec's schema into the
// generated one: descriptions, enums, and examples.
func keepWritten(gen, written map[string]any) {
	if written == nil || gen == nil || gen["$ref"] != nil {
		return
	}
	for _, k := range []string{"description", "enum", "example"} {
		if v, ok := written[k]; ok {
			gen[k] = v
		}
	}
	if gen["type"] == "string" && written["format"] != nil && gen["format"] == nil {
		gen["format"] = written["format"]
	}
	props := asMap(written["properties"])
	for name, g := range asMap(gen["properties"]) {
		if w := asMap(props[name]); w != nil {
			gm := copyMap(asMap(g))
			keepWritten(gm, w)
			asMap(gen["properties"])[name] = gm
		}
	}
	if items := asMap(gen["items"]); items != nil {
		gm := copyMap(items)
		keepWritten(gm, asMap(written["items"]))
		gen["items"] = gm
	}
}

// schemaNode is a schema as YAML in the spec's style: keys in a fixed
// order, properties in the Go type's order, and short schemas on one line.
func schemaNode(s map[string]any) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode}
	simple := true
	add := func(key string, v *yaml.Node) {
		n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, v)
		if v.Kind != yaml.ScalarNode && (v.Kind != yaml.SequenceNode || key != "enum") {
			simple = false
		}
	}
	for _, key := range []string{"$ref", "type", "format", "nullable", "description", "enum", "example", "required", "properties", "items", "additionalProperties", "allOf"} {
		v, ok := s[key]
		if !ok {
			continue
		}
		switch key {
		case "properties":
			props := &yaml.Node{Kind: yaml.MappingNode}
			order, _ := s["x-order"].([]string)
			if len(order) == 0 {
				for name := range asMap(v) {
					order = append(order, name)
				}
				sort.Strings(order)
			}
			for _, name := range order {
				props.Content = append(props.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: name}, schemaNode(asMap(asMap(v)[name])))
			}
			add(key, props)
		case "items", "additionalProperties":
			if b, isBool := v.(bool); isBool {
				add(key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: fmt.Sprint(b)})
			} else {
				add(key, schemaNode(asMap(v)))
			}
		case "allOf":
			seq := &yaml.Node{Kind: yaml.SequenceNode}
			for _, p := range asSlice(v) {
				seq.Content = append(seq.Content, schemaNode(asMap(p)))
			}
			add(key, seq)
		default:
			var node yaml.Node
			if err := node.Encode(v); err != nil {
				panic(err)
			}
			if node.Kind == yaml.SequenceNode {
				node.Style = yaml.FlowStyle
			}
			add(key, &node)
		}
	}
	if simple {
		n.Style = yaml.FlowStyle
	}
	return n
}

func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// Doc comments, read from the Go source: a type's, and its fields'.
var (
	docsOnce sync.Map // package dir -> map["Type" or "Type.Field"]string
)

func docsOf(t reflect.Type) map[string]string {
	const module = "github.com/yeixio/toskar-core"
	if !strings.HasPrefix(t.PkgPath(), module) {
		return nil
	}
	dir := filepath.Join("..", "..", strings.TrimPrefix(t.PkgPath(), module))
	if got, ok := docsOnce.Load(dir); ok {
		return got.(map[string]string)
	}
	docs := map[string]string{}
	paths, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	fset := token.NewFileSet()
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			decl, ok := node.(*ast.GenDecl)
			if !ok || decl.Tok != token.TYPE {
				return true
			}
			for _, spec := range decl.Specs {
				ts := spec.(*ast.TypeSpec)
				doc := ts.Doc
				if doc == nil && len(decl.Specs) == 1 {
					doc = decl.Doc
				}
				docs[ts.Name.Name] = commentText(doc, ts.Name.Name, "")
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				for _, f := range st.Fields.List {
					c := f.Doc
					if c == nil {
						c = f.Comment
					}
					for _, name := range f.Names {
						docs[ts.Name.Name+"."+name.Name] = commentText(c, name.Name, "")
					}
				}
			}
			return false
		})
	}
	docsOnce.Store(dir, docs)
	return docs
}

// typeDoc is a type's doc comment without its name: "Summary covers a
// conversation" becomes "Covers a conversation".
func typeDoc(t reflect.Type) string {
	text := docsOf(t)[t.Name()]
	if rest, ok := strings.CutPrefix(text, "`"+t.Name()+"` "); ok && rest != "" {
		return strings.ToUpper(rest[:1]) + rest[1:]
	}
	return text
}

func fieldDoc(t reflect.Type, goName, jsonName string) string {
	text := docsOf(t)[t.Name()+"."+goName]
	if text == "" {
		return ""
	}
	return strings.Replace(text, "`"+goName+"`", "`"+jsonName+"`", 1)
}

// commentText is a doc comment as one paragraph for the spec. "Code is the
// 6 digits" becomes "The 6 digits"; a comment that names the field
// otherwise names it in backquotes.
func commentText(c *ast.CommentGroup, goName, _ string) string {
	if c == nil {
		return ""
	}
	text := strings.Join(strings.Fields(c.Text()), " ")
	for _, verb := range []string{" is ", " are "} {
		if rest, ok := strings.CutPrefix(text, goName+verb); ok && rest != "" {
			// A value such as this_computer keeps its case.
			if first, _, _ := strings.Cut(rest, " "); strings.ContainsAny(first, "_.\"`") {
				return rest
			}
			return strings.ToUpper(rest[:1]) + rest[1:]
		}
	}
	if rest, ok := strings.CutPrefix(text, goName+" "); ok {
		return "`" + goName + "` " + rest
	}
	return text
}
