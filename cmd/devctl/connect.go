package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

const connectUsage = `usage: toskarctl connect <agent> [flags]
  Prints the settings a coding agent needs to use a model on Toskar, or
  writes them with --write (#959). The agent drives the model directly, with
  its own tools (raw access, #843).

agents:
  claude-code   Claude Code, through Anthropic's Messages format
  codex         Codex CLI, through OpenAI's Responses format
  opencode      OpenCode, through Chat Completions
  aider         Aider, through Chat Completions

editors (#960), through Chat Completions; these print what to enter:
  continue      Continue, its config.yaml
  cline         Cline's OpenAI Compatible provider
  roo-code      Roo Code's OpenAI Compatible provider
  zed           Zed's settings.json
  jetbrains     JetBrains AI Assistant's third-party provider
  copilot       GitHub Copilot Chat in VS Code, its own-model option

flags:
  --model id    the model; by default, the best installed one for coding
                agents: it calls tools, is made for code, and fits well
  --write       write the settings instead of printing them:
                  claude-code  .claude/settings.local.json in this folder
                  opencode     opencode.json in this folder
                  codex        a toskar provider in ~/.codex/config.toml and a
                               toskar profile, ~/.codex/toskar.config.toml;
                               run codex --profile toskar
                Only the agent's Toskar settings are written; the rest stay.

The address is TOSKAR_URL, or this computer. On this computer no key is
needed; for another, set TOSKAR_API_KEY to a key with raw access (API Access,
"Drive models directly, for coding agents").`

// connectTarget is what an agent is pointed at.
type connectTarget struct {
	// base is Toskar's address, without /v1.
	base  string
	model string
	// key is the key an agent sends: TOSKAR_API_KEY, or, on this computer
	// without one, a stand-in, since none is needed there.
	key string
	// keyFromEnv says key came from TOSKAR_API_KEY, so settings can name
	// the variable rather than hold the secret.
	keyFromEnv bool
	// window is the context window the model starts with on Toskar, for an
	// editor that's told it (#962), or 8192 when it isn't known.
	window int
}

// connectAgent prints an agent's settings and, for most, writes them.
type connectAgent struct {
	name  string
	print func(t connectTarget) string
	write func(t connectTarget, dir, home string) (string, error)
}

var connectAgents = map[string]connectAgent{
	"claude-code": {name: "Claude Code", print: claudeCodePrint, write: claudeCodeWrite},
	"codex":       {name: "Codex CLI", print: codexPrint, write: codexWrite},
	"opencode":    {name: "OpenCode", print: openCodePrint, write: openCodeWrite},
	"aider":       {name: "Aider", print: aiderPrint},
	// Editors (#960): their agents and AI features take an OpenAI-compatible
	// endpoint, so the model is raw:<id>. Most keep it in their own settings
	// screens, so these print what to enter.
	"continue":  {name: "Continue", print: continuePrint},
	"cline":     {name: "Cline", print: func(t connectTarget) string { return compatibleSteps("Cline", t) }},
	"roo-code":  {name: "Roo Code", print: func(t connectTarget) string { return compatibleSteps("Roo Code", t) }},
	"zed":       {name: "Zed", print: zedPrint},
	"jetbrains": {name: "JetBrains AI Assistant", print: jetBrainsPrint},
	"copilot":   {name: "GitHub Copilot in VS Code", print: copilotPrint},
}

// localKey is the key agents send to Toskar on this computer, which needs
// none; agents won't start without one.
const localKey = "toskar-local"

func connectCommand(args []string, client daemonClient, out io.Writer, dir, home string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return &exitError{code: 2, msg: connectUsage}
	}
	agent, ok := connectAgents[args[0]]
	if !ok {
		return &exitError{code: 2, msg: fmt.Sprintf("toskarctl connect doesn't know %q\n\n%s", args[0], connectUsage)}
	}
	fs := flag.NewFlagSet("connect", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	model := fs.String("model", "", "")
	write := fs.Bool("write", false, "")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() > 0 {
		return &exitError{code: 2, msg: connectUsage}
	}
	t := connectTarget{base: client.base, model: strings.TrimPrefix(*model, "raw:"), key: config.Env("API_KEY")}
	t.keyFromEnv = t.key != ""
	if t.key == "" {
		if !onThisComputer(t.base) {
			return fmt.Errorf("%s is another computer, so the agent needs a key with raw access: create one in API Access, turn on \"Drive models directly, for coding agents\", and set TOSKAR_API_KEY to it", t.base)
		}
		t.key = localKey
	}
	models, err := listModels(client)
	if err != nil && t.model == "" {
		return err
	}
	if t.model == "" {
		if t.model = agentModel(models); t.model == "" {
			return fmt.Errorf("no chat model is installed for the agent to use; install one made for code, such as: toskarctl models install qwen2.5-coder-7b-q4")
		}
	}
	t.window = 8192
	for _, m := range models {
		if m.ID == t.model && m.ContextWindow > 0 {
			t.window = m.ContextWindow
		}
	}
	if !*write {
		_, err := io.WriteString(out, agent.print(t))
		return err
	}
	if agent.write == nil {
		if args[0] == "aider" {
			return fmt.Errorf("%s reads its settings from the environment, so there's no file to write: add these to your shell profile\n\n%s", agent.name, agent.print(t))
		}
		return fmt.Errorf("%s keeps these in its own settings, so toskarctl doesn't write them: enter them there\n\n%s", agent.name, agent.print(t))
	}
	path, err := agent.write(t, dir, home)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Wrote %s's Toskar settings to %s, with %s.\n", agent.name, path, t.model)
	if args[0] == "codex" {
		fmt.Fprintln(out, "Run: codex --profile toskar")
	}
	if !t.keyFromEnv {
		return nil
	}
	if args[0] == "claude-code" {
		fmt.Fprintln(out, "It holds your API key: Claude Code keeps settings.local.json out of git, so keep it that way.")
	}
	return nil
}

// onThisComputer reports a Toskar address on this computer.
func onThisComputer(base string) bool {
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

// agentModel is the installed chat model best for a coding agent: one that
// calls tools, then one made for code, then one that fits Excellent or
// Good, then the largest. Empty when no chat model is installed.
func agentModel(models []contracts.Model) string {
	var chat []contracts.Model
	for _, m := range models {
		if m.Installed && contracts.SupportRoleOf(m) == "" {
			chat = append(chat, m)
		}
	}
	fitsWell := func(m contracts.Model) bool {
		return m.Fit != nil && (m.Fit.Label == contracts.FitExcellent || m.Fit.Label == contracts.FitGood)
	}
	rank := func(m contracts.Model) [3]bool {
		return [3]bool{m.Capabilities.ToolCalling, m.Capabilities.Coding, fitsWell(m)}
	}
	sort.SliceStable(chat, func(i, j int) bool {
		a, b := rank(chat[i]), rank(chat[j])
		for k := range a {
			if a[k] != b[k] {
				return a[k]
			}
		}
		return chat[i].SizeBytes > chat[j].SizeBytes
	})
	if len(chat) == 0 {
		return ""
	}
	return chat[0].ID
}

// shellKey is the key as a shell line sets it: the variable, when it came
// from one, so the secret isn't printed.
func shellKey(t connectTarget) string {
	if t.keyFromEnv {
		return `"$TOSKAR_API_KEY"`
	}
	return t.key
}

// claudeEnv is what Claude Code reads to use Toskar: its address (Claude
// Code adds /v1/messages), the key, and the model for every role it might
// pick, so background and subagent work reaches Toskar too.
func claudeEnv(t connectTarget, key string) [][2]string {
	return [][2]string{
		{"ANTHROPIC_BASE_URL", t.base},
		{"ANTHROPIC_AUTH_TOKEN", key},
		{"ANTHROPIC_MODEL", t.model},
		{"ANTHROPIC_DEFAULT_OPUS_MODEL", t.model},
		{"ANTHROPIC_DEFAULT_SONNET_MODEL", t.model},
		{"ANTHROPIC_DEFAULT_HAIKU_MODEL", t.model},
	}
}

func claudeCodePrint(t connectTarget) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Claude Code with %s on Toskar, through Anthropic's Messages format.\n", t.model)
	b.WriteString("# Set these, then run claude; or write them for this folder: toskarctl connect claude-code --write\n")
	for _, kv := range claudeEnv(t, shellKey(t)) {
		fmt.Fprintf(&b, "export %s=%s\n", kv[0], kv[1])
	}
	return b.String()
}

func claudeCodeWrite(t connectTarget, dir, _ string) (string, error) {
	path := filepath.Join(dir, ".claude", "settings.local.json")
	settings, err := readJSONObject(path)
	if err != nil {
		return "", err
	}
	env, _ := settings["env"].(map[string]any)
	if env == nil {
		env = map[string]any{}
	}
	for _, kv := range claudeEnv(t, t.key) {
		env[kv[0]] = kv[1]
	}
	settings["env"] = env
	return path, writeJSONObject(path, settings)
}

// codexProvider is Codex's toskar provider, through the Responses format,
// for its config.toml, where Codex reads providers.
func codexProvider(t connectTarget) string {
	return fmt.Sprintf(`[model_providers.toskar]
name = "Toskar"
base_url = %q
env_key = "TOSKAR_API_KEY"
wire_api = "responses"
`, t.base+"/v1")
}

// codexProfile is the toskar profile, toskar.config.toml beside
// config.toml, which Codex 0.134 and later lays over it for codex --profile
// toskar, so the default stays as it was.
func codexProfile(t connectTarget) string {
	return fmt.Sprintf("model = %q\nmodel_provider = \"toskar\"\n", t.model)
}

func codexPrint(t connectTarget) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Codex CLI with %s on Toskar, through OpenAI's Responses format.\n", t.model)
	b.WriteString("# Write both with: toskarctl connect codex --write; then run codex --profile toskar\n")
	if !t.keyFromEnv {
		fmt.Fprintf(&b, "# Codex reads the key from TOSKAR_API_KEY; on this computer any value works: export TOSKAR_API_KEY=%s\n", t.key)
	}
	b.WriteString("\n# Add to ~/.codex/config.toml:\n")
	b.WriteString(codexProvider(t))
	b.WriteString("\n# ~/.codex/toskar.config.toml:\n")
	b.WriteString(codexProfile(t))
	return b.String()
}

func codexWrite(t connectTarget, _, home string) (string, error) {
	dir := codexHome(home)
	path := filepath.Join(dir, "config.toml")
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	// A toskar profile table from before 0.134 stops Codex from reading the
	// profile file, so it goes too.
	kept := strings.TrimRight(withoutTOMLTables(string(raw), "model_providers.toskar", "profiles.toskar"), "\n")
	if kept != "" {
		kept += "\n\n"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(kept+codexProvider(t)), 0o600); err != nil {
		return "", err
	}
	profile := filepath.Join(dir, "toskar.config.toml")
	return path + " and " + profile, os.WriteFile(profile, []byte(codexProfile(t)), 0o600)
}

// codexHome is where Codex keeps its settings: CODEX_HOME, else ~/.codex.
func codexHome(home string) string {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return dir
	}
	return filepath.Join(home, ".codex")
}

// withoutTOMLTables is a TOML file less the named tables: each from its
// header to the next table's header or the end. The rest is kept as it is.
func withoutTOMLTables(text string, names ...string) string {
	drop := map[string]bool{}
	for _, n := range names {
		drop["["+n+"]"] = true
	}
	var out []string
	skipping := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			skipping = drop[trimmed]
		}
		if !skipping {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// openCodeProvider is OpenCode's toskar provider, through Chat Completions,
// where the model is raw:<id> so it reaches the model and not the assistant.
func openCodeProvider(t connectTarget) map[string]any {
	key := t.key
	if t.keyFromEnv {
		key = "{env:TOSKAR_API_KEY}"
	}
	return map[string]any{
		"npm":     "@ai-sdk/openai-compatible",
		"name":    "Toskar",
		"options": map[string]any{"baseURL": t.base + "/v1", "apiKey": key},
		"models":  map[string]any{"raw:" + t.model: map[string]any{"name": t.model + " on Toskar"}},
	}
}

func openCodePrint(t connectTarget) string {
	cfg := map[string]any{
		"$schema":  "https://opencode.ai/config.json",
		"provider": map[string]any{"toskar": openCodeProvider(t)},
		"model":    "toskar/raw:" + t.model,
	}
	raw, _ := json.MarshalIndent(cfg, "", "  ")
	return fmt.Sprintf("# OpenCode with %s on Toskar, through Chat Completions.\n# Put in opencode.json in your project (or: toskarctl connect opencode --write)\n%s\n", t.model, raw)
}

func openCodeWrite(t connectTarget, dir, _ string) (string, error) {
	path := filepath.Join(dir, "opencode.json")
	cfg, err := readJSONObject(path)
	if err != nil {
		return "", err
	}
	if _, ok := cfg["$schema"]; !ok {
		cfg["$schema"] = "https://opencode.ai/config.json"
	}
	providers, _ := cfg["provider"].(map[string]any)
	if providers == nil {
		providers = map[string]any{}
	}
	providers["toskar"] = openCodeProvider(t)
	cfg["provider"] = providers
	cfg["model"] = "toskar/raw:" + t.model
	return path, writeJSONObject(path, cfg)
}

func aiderPrint(t connectTarget) string {
	return fmt.Sprintf(`# Aider with %s on Toskar, through Chat Completions.
export OPENAI_API_BASE=%s
export OPENAI_API_KEY=%s
aider --model openai/raw:%s
`, t.model, t.base+"/v1", shellKey(t), t.model)
}

// continuePrint is Continue's config.yaml model entry.
func continuePrint(t connectTarget) string {
	key := t.key
	if t.keyFromEnv {
		key = "<your TOSKAR_API_KEY>"
	}
	return fmt.Sprintf(`# Continue with %[1]s on Toskar, through Chat Completions.
# Add under models: in ~/.continue/config.yaml.
models:
  - name: %[1]s on Toskar
    provider: openai
    model: raw:%[1]s
    apiBase: %[2]s/v1
    apiKey: %[3]s
    roles:
      - chat
      - edit
      - apply
    capabilities:
      - tool_use
`, t.model, t.base, key)
}

// compatibleSteps are the steps for an editor agent whose settings screen
// takes an OpenAI-compatible provider, as Cline's and Roo Code's do.
func compatibleSteps(name string, t connectTarget) string {
	key := t.key
	if t.keyFromEnv {
		key = "your TOSKAR_API_KEY"
	}
	return fmt.Sprintf(`# %[1]s with %[2]s on Toskar, through Chat Completions.
# In %[1]s's settings, choose the API provider "OpenAI Compatible", then:
Base URL: %[3]s/v1
API key:  %[4]s
Model ID: raw:%[2]s
Context window: %[5]d
`, name, t.model, t.base, key, t.window)
}

// zedPrint is Zed's settings.json provider.
func zedPrint(t connectTarget) string {
	cfg := map[string]any{"language_models": map[string]any{"openai_compatible": map[string]any{"Toskar": map[string]any{
		"api_url": t.base + "/v1",
		"available_models": []map[string]any{{
			"name": "raw:" + t.model, "display_name": t.model + " on Toskar", "max_tokens": t.window,
			"capabilities": map[string]any{"tools": true, "images": false, "parallel_tool_calls": false, "prompt_cache_key": false},
		}},
	}}}}
	raw, _ := json.MarshalIndent(cfg, "", "  ")
	keyNote := "On this computer Toskar needs no key; if Zed asks for one, enter " + t.key + "."
	if t.keyFromEnv {
		keyNote = "Enter your TOSKAR_API_KEY as the Toskar provider's API key in Zed's agent settings."
	}
	return fmt.Sprintf("# Zed with %s on Toskar, through Chat Completions.\n# Merge into Zed's settings.json (zed: open settings). %s\n%s\n", t.model, keyNote, raw)
}

func jetBrainsPrint(t connectTarget) string {
	key := t.key
	if t.keyFromEnv {
		key = "your TOSKAR_API_KEY"
	}
	return fmt.Sprintf(`# JetBrains AI Assistant with %[1]s on Toskar, through Chat Completions.
# Settings > Tools > AI Assistant > Models: add a third-party provider,
# "OpenAI-compatible", then:
URL:     %[2]s/v1
API key: %[3]s
Model:   raw:%[1]s
`, t.model, t.base, key)
}

func copilotPrint(t connectTarget) string {
	key := t.key
	if t.keyFromEnv {
		key = "your TOSKAR_API_KEY"
	}
	return fmt.Sprintf(`# GitHub Copilot Chat in VS Code with %[1]s on Toskar, through Chat Completions.
# Run "Chat: Manage Language Models", add the "OpenAI Compatible" provider, then:
Base URL: %[2]s/v1
API key:  %[3]s
# Copilot lists the models from %[2]s/v1/models; pick raw:%[1]s in the chat's model picker.
`, t.model, t.base, key)
}

// readJSONObject reads a JSON settings file, or an empty one when there's
// none yet.
func readJSONObject(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]any{}, nil
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%s isn't JSON Toskar can add to: %v", path, err)
	}
	return m, nil
}

func writeJSONObject(path string, m map[string]any) error {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o600)
}
