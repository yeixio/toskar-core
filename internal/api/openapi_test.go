package api

import (
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"gopkg.in/yaml.v3"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

type openAPIDoc struct {
	Paths      map[string]map[string]openAPIOperation `yaml:"paths"`
	Components struct {
		Schemas map[string]any `yaml:"schemas"`
	} `yaml:"components"`
}

type openAPIOperation struct {
	OperationID string `yaml:"operationId"`
	Parameters  []struct {
		Name string `yaml:"name"`
		In   string `yaml:"in"`
	} `yaml:"parameters"`
}

var openAPIMethods = map[string]bool{"get": true, "post": true, "put": true, "patch": true, "delete": true}

func loadOpenAPI(t *testing.T) (openAPIDoc, string) {
	t.Helper()
	raw, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc openAPIDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("api/openapi.yaml: %v", err)
	}
	return doc, string(raw)
}

// api/openapi.yaml describes every route the daemon serves, and nothing it
// does not, so the spec cannot fall behind the router again.
func TestOpenAPICoversEveryRoute(t *testing.T) {
	doc, _ := loadOpenAPI(t)
	srv := NewServer(Dependencies{})

	served := map[string]bool{}
	err := srv.router.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		path, err := route.GetPathTemplate()
		if err != nil {
			return nil
		}
		methods, err := route.GetMethods()
		if err != nil {
			return nil // a prefix with no methods of its own
		}
		for _, m := range methods {
			if m != http.MethodOptions {
				served[strings.ToLower(m)+" "+path] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	described := map[string]bool{}
	for path, ops := range doc.Paths {
		for m := range ops {
			if openAPIMethods[m] {
				described[m+" "+path] = true
			}
		}
	}

	var missing, extra []string
	for k := range served {
		if !described[k] {
			missing = append(missing, k)
		}
	}
	for k := range described {
		if !served[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("routes missing from api/openapi.yaml:\n  %s", strings.Join(missing, "\n  "))
	}
	if len(extra) > 0 {
		t.Errorf("api/openapi.yaml describes routes the daemon does not serve:\n  %s", strings.Join(extra, "\n  "))
	}
}

// Every operation has a unique id, declares its path parameters, and refers
// only to schemas that exist.
func TestOpenAPIIsConsistent(t *testing.T) {
	doc, raw := loadOpenAPI(t)
	ids := map[string]string{}
	param := regexp.MustCompile(`\{([^}]+)\}`)
	for path, ops := range doc.Paths {
		for m, op := range ops {
			if !openAPIMethods[m] {
				continue
			}
			where := m + " " + path
			if op.OperationID == "" {
				t.Errorf("%s has no operationId", where)
			} else if prev, dup := ids[op.OperationID]; dup {
				t.Errorf("operationId %q is used by %s and %s", op.OperationID, prev, where)
			}
			ids[op.OperationID] = where
			declared := map[string]bool{}
			for _, p := range op.Parameters {
				if p.In == "path" {
					declared[p.Name] = true
				}
			}
			for _, match := range param.FindAllStringSubmatch(path, -1) {
				if !declared[match[1]] {
					t.Errorf("%s does not declare path parameter %q", where, match[1])
				}
			}
		}
	}
	for _, match := range regexp.MustCompile(`#/components/schemas/([A-Za-z0-9_]+)`).FindAllStringSubmatch(raw, -1) {
		if _, ok := doc.Components.Schemas[match[1]]; !ok {
			t.Errorf("schema %q is referenced but not defined", match[1])
		}
	}
}

// The spec's version is the client contract's, so a generated SDK says
// which contract it was built for (#666).
func TestOpenAPIVersionIsTheContract(t *testing.T) {
	_, raw := loadOpenAPI(t)
	var head struct {
		Info struct {
			Version string `yaml:"version"`
		} `yaml:"info"`
	}
	if err := yaml.Unmarshal([]byte(raw), &head); err != nil {
		t.Fatal(err)
	}
	if head.Info.Version != contracts.ContractVersion {
		t.Errorf("api/openapi.yaml info.version is %q; it follows the contract, %q (pkg/contracts/contract.go)", head.Info.Version, contracts.ContractVersion)
	}
}

// Generated SDKs name their types after the spec's schemas, so a JSON
// response is a named schema (or a list of one), not an object described
// in place (#666). The operations that aren't yet are listed in
// testdata/openapi_unnamed.txt; the list may only shrink.
func TestOpenAPIResponsesAreNamed(t *testing.T) {
	_, raw := loadOpenAPI(t)
	var spec map[string]any
	if err := yaml.Unmarshal([]byte(raw), &spec); err != nil {
		t.Fatal(err)
	}
	spec = normalize(spec).(map[string]any)
	var unnamed []string
	for path, ops := range asMap(spec["paths"]) {
		for m, op := range asMap(ops) {
			if !openAPIMethods[m] {
				continue
			}
			for code, resp := range asMap(asMap(op)["responses"]) {
				if !strings.HasPrefix(code, "2") {
					continue
				}
				schema := asMap(asMap(asMap(asMap(resp)["content"])["application/json"])["schema"])
				if schema == nil || schema["$ref"] != nil || asMap(schema["items"])["$ref"] != nil {
					continue
				}
				unnamed = append(unnamed, m+" "+path+" "+code)
			}
		}
	}
	sort.Strings(unnamed)
	const file = "testdata/openapi_unnamed.txt"
	if os.Getenv("UPDATE_OPENAPI_DRIFT") != "" {
		if err := os.WriteFile(file, []byte(strings.Join(unnamed, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	raw2, _ := os.ReadFile(file)
	listed := map[string]bool{}
	for _, line := range strings.Split(string(raw2), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			listed[line] = true
		}
	}
	for _, u := range unnamed {
		if !listed[u] {
			t.Errorf("%s describes its JSON in place; give it a named schema in components/schemas", u)
		}
		delete(listed, u)
	}
	for done := range listed {
		t.Errorf("%s has a named schema now; remove it from %s", done, file)
	}
}
