package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// connectDaemon is a daemon on this computer with three chat models and an
// embedding one installed; the coder calls tools, is made for code, and
// fits well, so it's the agent's.
func connectDaemon(t *testing.T) daemonClient {
	t.Helper()
	models := []contracts.Model{
		{ID: "big-chat", SizeBytes: 9e9, Installed: true, Capabilities: contracts.ModelCapabilities{ToolCalling: true}, Fit: &contracts.ModelFit{Label: contracts.FitTight}},
		{ID: "qwen2.5-coder-7b-q4", SizeBytes: 4e9, Installed: true, Capabilities: contracts.ModelCapabilities{ToolCalling: true, Coding: true}, Fit: &contracts.ModelFit{Label: contracts.FitExcellent}},
		{ID: "tiny-no-tools", SizeBytes: 1e9, Installed: true, Fit: &contracts.ModelFit{Label: contracts.FitExcellent}},
		{ID: "not-installed-coder", SizeBytes: 3e9, Capabilities: contracts.ModelCapabilities{ToolCalling: true, Coding: true}},
		{ID: "nomic-embed", SupportRole: "embedding", Installed: true},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/models":
			_ = json.NewEncoder(w).Encode(models)
		case "/api/v1/models/fit":
			_ = json.NewEncoder(w).Encode([]contracts.ModelsFitResponse{})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	// The address agents get is this computer's, as the daemon's would be.
	return daemonClient{base: strings.Replace(srv.URL, "127.0.0.1", "localhost", 1), client: srv.Client()}
}

// Each agent's printed settings point it at Toskar in the format it speaks,
// with the best model for it (#959).
func TestConnectPrints(t *testing.T) {
	t.Setenv("TOSKAR_API_KEY", "")
	client := connectDaemon(t)
	for agent, want := range map[string][]string{
		"claude-code": {"export ANTHROPIC_BASE_URL=" + client.base + "\n", "ANTHROPIC_AUTH_TOKEN=toskar-local", "ANTHROPIC_MODEL=qwen2.5-coder-7b-q4", "ANTHROPIC_DEFAULT_HAIKU_MODEL=qwen2.5-coder-7b-q4"},
		"codex":       {"[model_providers.toskar]", `base_url = "` + client.base + `/v1"`, `wire_api = "responses"`, `env_key = "TOSKAR_API_KEY"`, "toskar.config.toml", `model = "qwen2.5-coder-7b-q4"`, `model_provider = "toskar"`, "codex --profile toskar", "export TOSKAR_API_KEY=toskar-local"},
		"opencode":    {`"npm": "@ai-sdk/openai-compatible"`, `"baseURL": "` + client.base + `/v1"`, `"raw:qwen2.5-coder-7b-q4"`, `"model": "toskar/raw:qwen2.5-coder-7b-q4"`},
		"aider":       {"OPENAI_API_BASE=" + client.base + "/v1", "aider --model openai/raw:qwen2.5-coder-7b-q4"},
	} {
		var out bytes.Buffer
		if err := connectCommand([]string{agent}, client, &out, t.TempDir(), t.TempDir()); err != nil {
			t.Fatalf("%s: %v", agent, err)
		}
		for _, w := range want {
			if !strings.Contains(out.String(), w) {
				t.Errorf("%s: no %q in\n%s", agent, w, out.String())
			}
		}
	}

	// A key from TOSKAR_API_KEY is named, not printed.
	t.Setenv("TOSKAR_API_KEY", "ygg_secret")
	var out bytes.Buffer
	if err := connectCommand([]string{"claude-code", "--model", "raw:big-chat"}, client, &out, t.TempDir(), t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "ygg_secret") || !strings.Contains(out.String(), `ANTHROPIC_AUTH_TOKEN="$TOSKAR_API_KEY"`) || !strings.Contains(out.String(), "ANTHROPIC_MODEL=big-chat") {
		t.Errorf("with a key:\n%s", out.String())
	}
}

// --write adds only the agent's Toskar settings, keeping the rest, and
// writing again replaces them rather than adding twice (#959).
func TestConnectWrites(t *testing.T) {
	t.Setenv("TOSKAR_API_KEY", "")
	client := connectDaemon(t)
	dir, home := t.TempDir(), t.TempDir()
	write := func(path, text string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	read := func(path string) string {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	write(filepath.Join(dir, ".claude", "settings.local.json"), `{"permissions":{"allow":["Bash(go test:*)"]},"env":{"KEEP":"me"}}`)
	write(filepath.Join(dir, "opencode.json"), `{"theme":"dark","provider":{"other":{"name":"Other"}}}`)
	t.Setenv("CODEX_HOME", "")
	write(filepath.Join(home, ".codex", "config.toml"), "model = \"gpt-5\"\n\n[model_providers.toskar]\nname = \"old\"\n\n[profiles.toskar]\nmodel = \"legacy\"\n\n[mcp_servers.docs]\ncommand = \"docs\"\n")

	for range 2 {
		for _, agent := range []string{"claude-code", "opencode", "codex"} {
			var out bytes.Buffer
			if err := connectCommand([]string{agent, "--write"}, client, &out, dir, home); err != nil {
				t.Fatalf("%s: %v", agent, err)
			}
		}
	}

	var claude map[string]any
	_ = json.Unmarshal([]byte(read(filepath.Join(dir, ".claude", "settings.local.json"))), &claude)
	env, _ := claude["env"].(map[string]any)
	if claude["permissions"] == nil || env["KEEP"] != "me" || env["ANTHROPIC_MODEL"] != "qwen2.5-coder-7b-q4" || env["ANTHROPIC_AUTH_TOKEN"] != localKey {
		t.Errorf("claude-code settings: %v", claude)
	}

	var oc map[string]any
	_ = json.Unmarshal([]byte(read(filepath.Join(dir, "opencode.json"))), &oc)
	providers, _ := oc["provider"].(map[string]any)
	if oc["theme"] != "dark" || providers["other"] == nil || providers["toskar"] == nil || oc["model"] != "toskar/raw:qwen2.5-coder-7b-q4" {
		t.Errorf("opencode.json: %v", oc)
	}

	codex := read(filepath.Join(home, ".codex", "config.toml"))
	if !strings.HasPrefix(codex, "model = \"gpt-5\"") || !strings.Contains(codex, "[mcp_servers.docs]") || strings.Contains(codex, `name = "old"`) ||
		strings.Count(codex, "[model_providers.toskar]") != 1 || strings.Contains(codex, "[profiles.toskar]") {
		t.Errorf("config.toml:\n%s", codex)
	}
	if profile := read(filepath.Join(home, ".codex", "toskar.config.toml")); profile != "model = \"qwen2.5-coder-7b-q4\"\nmodel_provider = \"toskar\"\n" {
		t.Errorf("toskar.config.toml:\n%s", profile)
	}

	var out bytes.Buffer
	if err := connectCommand([]string{"aider", "--write"}, client, &out, dir, home); err == nil || !strings.Contains(err.Error(), "OPENAI_API_BASE") {
		t.Errorf("aider --write: %v", err)
	}
}

// Another computer needs a key with raw access; no chat model, an unknown
// agent, and a stray argument are refused (#959).
func TestConnectRefuses(t *testing.T) {
	t.Setenv("TOSKAR_API_KEY", "")
	remote := daemonClient{base: "http://192.168.1.20:7331"}
	if err := connectCommand([]string{"codex", "--model", "m"}, remote, new(bytes.Buffer), t.TempDir(), t.TempDir()); err == nil || !strings.Contains(err.Error(), "TOSKAR_API_KEY") {
		t.Errorf("remote without a key: %v", err)
	}
	if got := agentModel([]contracts.Model{{ID: "e", SupportRole: "embedding", Installed: true}, {ID: "x"}}); got != "" {
		t.Errorf("no chat model: %q", got)
	}
	for _, args := range [][]string{{}, {"warp"}, {"codex", "extra"}, {"--write"}} {
		var e *exitError
		err := connectCommand(args, connectDaemon(t), new(bytes.Buffer), t.TempDir(), t.TempDir())
		if !errors.As(err, &e) || e.code != 2 {
			t.Errorf("%v: %v", args, err)
		}
	}
}
