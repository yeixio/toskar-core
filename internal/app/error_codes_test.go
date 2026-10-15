package app

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Codes the web UI and apps make themselves, not sent by core.
var clientErrorCodes = []string{"SERVICE_UNREACHABLE", "TOOL_SOURCE_ENTRY_NOT_FOUND", "BAD_RESPONSE", "REPLY_INTERRUPTED"}

var codePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]+$`)

// sentErrorCodes reads every literal error code core sends: the code given
// to writeErr and writeErrFrom, and to contracts.NewError and Errorf. The
// OpenAI-compatible API keeps OpenAI's own errors for its clients.
func sentErrorCodes(t *testing.T, root string) map[string]string {
	t.Helper()
	codes := map[string]string{}
	fset := token.NewFileSet()
	for _, dir := range []string{"internal", "pkg"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && d.Name() == "openai" {
				return filepath.SkipDir
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				arg := -1
				switch fun := call.Fun.(type) {
				case *ast.Ident:
					if fun.Name == "writeErr" || fun.Name == "writeErrFrom" {
						arg = 2
					}
				case *ast.SelectorExpr:
					if pkg, ok := fun.X.(*ast.Ident); ok && pkg.Name == "contracts" && (fun.Sel.Name == "NewError" || fun.Sel.Name == "Errorf") {
						arg = 0
					}
				}
				if arg < 0 || len(call.Args) <= arg {
					return true
				}
				if lit, ok := call.Args[arg].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if code, err := strconv.Unquote(lit.Value); err == nil && codePattern.MatchString(code) {
						codes[code] = fset.Position(lit.Pos()).String()
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, rule := range chatErrorRules {
		codes[rule.code] = "chatErrorRules"
	}
	codes["MODEL_UNHEALTHY"] = "chatErrorCode"
	return codes
}

// Every code core sends has English text in the catalog, which clients show
// in the App language (multilingual spec §10), and the catalog has no codes
// nothing sends.
func TestErrorCodesHaveCatalogText(t *testing.T) {
	root := filepath.Join("..", "..")
	sent := sentErrorCodes(t, root)
	if len(sent) < 50 {
		t.Fatalf("found only %d error codes; is the scan reading the source?", len(sent))
	}
	raw, err := os.ReadFile(filepath.Join(root, "i18n", "locales", "en", "errors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalog map[string]string
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatalf("errors.json: %v", err)
	}
	var missing []string
	for code, where := range sent {
		if _, ok := catalog[code]; !ok {
			missing = append(missing, code+" ("+where+")")
		}
	}
	slices.Sort(missing)
	for _, m := range missing {
		t.Errorf("error code %s has no text in i18n/locales/en/errors.json", m)
	}
	for code := range catalog {
		if !codePattern.MatchString(code) {
			t.Errorf("errors.json key %q is not an error code (UPPER_SNAKE_CASE)", code)
		} else if _, ok := sent[code]; !ok && !slices.Contains(clientErrorCodes, code) {
			t.Errorf("errors.json has %s, which nothing sends", code)
		}
	}
}
