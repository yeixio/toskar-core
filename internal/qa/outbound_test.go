package qa

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// outboundHosts is every web address Toskar's own code names, and what in
// docs/privacy.md says it's reached (#1415). A host that's only shown to
// people, such as a link to install Node.js, or that isn't an address at
// all, such as an XML namespace, has "" and why beside it.
//
// A new host fails TestEveryOutboundHostIsDocumented until it's added here:
// a call Toskar makes needs a line in docs/privacy.md that names it, and a
// record in What left this computer (internal/egress).
var outboundHosts = map[string]string{
	"aerodatabox.p.rapidapi.com":  "AeroDataBox",
	"prod.api.market":             "AeroDataBox",
	"api.github.com":              "api.github.com",
	"github.com":                  "GitHub",
	"raw.githubusercontent.com":   "raw.githubusercontent.com",
	"api.openai.com":              "External server",
	"html.duckduckgo.com":         "DuckDuckGo",
	"wttr.in":                     "wttr.in",
	"fonts.gstatic.com":           "fonts.gstatic.com",
	"huggingface.co":              "Hugging Face",
	"nominatim.openstreetmap.org": "Nominatim",
	"overpass-api.de":             "Overpass",
	"routing.openstreetmap.de":    "router",
	"ntfy.sh":                     "ntfy",
	"ratings.toskar.ai":           "ratings service",
	"relay.toskar.ai":             "relay.toskar.ai",
	"toskar.ai":                   "toskar.ai",
	// MCP gallery entries: the server is reached only once someone adds
	// it, as any MCP tool source is.
	"api.githubcopilot.com": "MCP tool sources",
	"mcp.atlassian.com":     "MCP tool sources",
	"mcp.context7.com":      "MCP tool sources",
	"mcp.deepwiki.com":      "MCP tool sources",
	"mcp.linear.app":        "MCP tool sources",
	"mcp.notion.com":        "MCP tool sources",
	"mcp.sentry.dev":        "MCP tool sources",
	// Calendar presets, reached only once connected.
	"caldav.fastmail.com": "CalDAV",
	"caldav.icloud.com":   "CalDAV",
	// A placeholder for the person's own Home Assistant.
	"homeassistant.local": "Home Assistant",

	// Shown to people as links, never fetched.
	"brave.com":             "",
	"bun.sh":                "",
	"context7.com":          "",
	"deepwiki.com":          "",
	"developers.notion.com": "",
	"docs.astral.sh":        "",
	"docs.sentry.io":        "",
	"learn.microsoft.com":   "",
	"linear.app":            "",
	"nodejs.org":            "",
	"opencode.ai":           "",
	"pypi.org":              "",
	"rpmfusion.org":         "",
	"wiki.debian.org":       "",
	"www.amd.com":           "",
	"www.atlassian.com":     "",
	"www.docker.com":        "",
	"www.intel.com":         "",
	"www.nvidia.com":        "",
	"www.openstreetmap.org": "",
	"www.python.org":        "",
	"yeix.io":               "",
	// Examples and fixtures.
	"example.com":      "",
	"shop.example.com": "",
	"www.example.org":  "",
	// XML namespaces and local names, not addresses.
	"schemas.openxmlformats.org": "",
	"schemas.xmlsoap.org":        "",
	"wails.localhost":            "",
}

var hostRe = regexp.MustCompile(`https?://([a-zA-Z0-9-]+(?:\.[a-zA-Z0-9-]+)*\.[a-zA-Z]{2,})`)

func TestEveryOutboundHostIsDocumented(t *testing.T) {
	root := filepath.Join("..", "..")
	privacy, err := os.ReadFile(filepath.Join(root, "docs", "privacy.md"))
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]string{}
	for _, dir := range []string{"internal", "cmd", "pkg"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				// The quality runner and the screenshot server are tools
				// for making Toskar, not part of what it runs.
				if d.Name() == "testdata" || d.Name() == "qualityrun" || d.Name() == "screenshot" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil {
					return true
				}
				for _, m := range hostRe.FindAllStringSubmatch(s, -1) {
					host := strings.ToLower(m[1])
					if _, ok := found[host]; !ok {
						found[host] = path
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
	var hosts []string
	for h := range found {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	for _, host := range hosts {
		doc, known := outboundHosts[host]
		if !known {
			t.Errorf("%s names %s: add it to outboundHosts, with what in docs/privacy.md says it's reached, and record the call in What left this computer", found[host], host)
			continue
		}
		if doc != "" && !strings.Contains(string(privacy), doc) {
			t.Errorf("%s is reached, but docs/privacy.md doesn't say %q", host, doc)
		}
	}
}
