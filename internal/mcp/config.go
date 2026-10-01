package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Spec is how to reach one server: a command to run on this computer, or a
// web address.
type Spec struct {
	Name    string            `json:"name"`
	Preset  string            `json:"preset,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Dir     string            `json:"dir,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	// ClientID and ClientSecret are for services that do not let apps
	// register themselves for sign-in.
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	// Keywords are words that show a message is about this server.
	Keywords []string `json:"keywords,omitempty"`
}

// Remote reports whether the server is reached by web address.
func (s Spec) Remote() bool { return s.URL != "" }

// Validate checks the spec can be tried.
func (s Spec) Validate() error {
	switch {
	case strings.TrimSpace(s.Name) == "":
		return errors.New("give the tool source a name")
	case s.Command == "" && s.URL == "":
		return errors.New("give a command to run or a web address")
	case s.Command != "" && s.URL != "":
		return errors.New("give a command or a web address, not both")
	}
	if s.URL != "" {
		u, err := url.Parse(s.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return fmt.Errorf("%q is not a web address", s.URL)
		}
	}
	return nil
}

// Need is a value a spec still needs from the person, such as a token left
// as a placeholder in a pasted snippet.
type Need struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Secret bool   `json:"secret"`
}

// Needs lists placeholders and blank values in the spec.
func (s Spec) Needs() []Need {
	var out []Need
	for _, k := range sortedKeys(s.Env) {
		if placeholder(s.Env[k]) {
			out = append(out, Need{Key: "env:" + k, Label: k, Secret: secretKey(k)})
		}
	}
	for _, k := range sortedKeys(s.Headers) {
		if v := s.Headers[k]; placeholder(strings.TrimPrefix(strings.TrimPrefix(v, "Bearer "), "bearer ")) {
			out = append(out, Need{Key: "header:" + k, Label: k, Secret: true})
		}
	}
	for i, a := range s.Args {
		if placeholderArg(a) {
			out = append(out, Need{Key: fmt.Sprintf("arg:%d", i), Label: strings.Trim(a, "<>{}$"), Secret: secretKey(a)})
		}
	}
	return out
}

// Fill puts values from the person into the places Needs named.
func (s Spec) Fill(values map[string]string) Spec {
	s = s.clone()
	for key, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		kind, name, _ := strings.Cut(key, ":")
		switch kind {
		case "env":
			s.Env[name] = v
		case "header":
			if old := s.Headers[name]; strings.HasPrefix(strings.ToLower(old), "bearer ") && !strings.HasPrefix(strings.ToLower(v), "bearer ") {
				v = "Bearer " + v
			}
			s.Headers[name] = v
		case "arg":
			var i int
			if _, err := fmt.Sscan(name, &i); err == nil && i >= 0 && i < len(s.Args) {
				s.Args[i] = v
			}
		}
	}
	return s
}

func (s Spec) clone() Spec {
	c := s
	c.Args = append([]string(nil), s.Args...)
	c.Env = map[string]string{}
	for k, v := range s.Env {
		c.Env[k] = v
	}
	c.Headers = map[string]string{}
	for k, v := range s.Headers {
		c.Headers[k] = v
	}
	c.Keywords = append([]string(nil), s.Keywords...)
	return c
}

// Secrets are the values of a spec kept in the secrets directory.
type Secrets struct {
	Env          map[string]string `json:"env,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	Args         map[int]string    `json:"args,omitempty"`
	ClientSecret string            `json:"client_secret,omitempty"`
	OAuth        *OAuth            `json:"oauth,omitempty"`
}

// split moves secret values out of the spec. The spec keeps the key with
// an empty value, so the shape stays visible.
func split(s Spec) (Spec, Secrets) {
	s = s.clone()
	sec := Secrets{Env: map[string]string{}, Headers: map[string]string{}, Args: map[int]string{}}
	for k, v := range s.Env {
		if v != "" && (secretKey(k) || secretValue(v)) {
			sec.Env[k], s.Env[k] = v, ""
		}
	}
	// Every header is a secret: they are how remote servers check who
	// is calling.
	for k, v := range s.Headers {
		if v != "" {
			sec.Headers[k], s.Headers[k] = v, ""
		}
	}
	for i, a := range s.Args {
		if secretValue(a) {
			sec.Args[i], s.Args[i] = a, ""
		}
	}
	sec.ClientSecret, s.ClientSecret = s.ClientSecret, ""
	return s, sec
}

// join puts secret values back into a spec for running it.
func join(s Spec, sec Secrets) Spec {
	s = s.clone()
	for k, v := range sec.Env {
		s.Env[k] = v
	}
	for k, v := range sec.Headers {
		s.Headers[k] = v
	}
	for i, v := range sec.Args {
		if i >= 0 && i < len(s.Args) {
			s.Args[i] = v
		}
	}
	s.ClientSecret = sec.ClientSecret
	return s
}

// values lists every secret value, so results can be scrubbed of them.
func (sec Secrets) values() []string {
	var out []string
	add := func(v string) {
		v = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(v, "Bearer "), "bearer "))
		if len(v) >= 6 {
			out = append(out, v)
		}
	}
	for _, v := range sec.Env {
		add(v)
	}
	for _, v := range sec.Headers {
		add(v)
	}
	for _, v := range sec.Args {
		add(v)
		// A password inside a connection string.
		if u, err := url.Parse(v); err == nil && u.User != nil {
			if p, ok := u.User.Password(); ok {
				add(p)
			}
		}
	}
	add(sec.ClientSecret)
	if sec.OAuth != nil {
		add(sec.OAuth.AccessToken)
		add(sec.OAuth.RefreshToken)
		add(sec.OAuth.ClientSecret)
	}
	return out
}

var secretKeyRe = regexp.MustCompile(`(?i)(key|token|secret|passw|pwd|credential|auth|private|cookie|session|pat\b|_pat|dsn|database_ur[il]|connection)`)

func secretKey(k string) bool { return secretKeyRe.MatchString(k) }

// secretValue reports values that are secrets whatever their name: a
// connection string with a password, or a well-known token shape.
func secretValue(v string) bool {
	if u, err := url.Parse(v); err == nil && u.User != nil {
		if _, ok := u.User.Password(); ok {
			return true
		}
	}
	for _, prefix := range []string{"sk-", "ghp_", "github_pat_", "xoxb-", "xoxp-", "glpat-", "ntn_", "secret_", "lin_api_", "AKIA"} {
		if strings.HasPrefix(v, prefix) && len(v) > 12 {
			return true
		}
	}
	return false
}

var placeholderRe = regexp.MustCompile(`(?i)^(<[^>]*>|\$\{[^}]*\}|\{\{[^}]*\}\}|your[-_ ].*|.*[-_]here|xxx+|\.\.\.|…|replace[-_ ]?me|changeme|todo|<?api[-_ ]?key>?|\*+|(sk|ghp|github_pat|ntn|lin_api)[-_]?(x+|\.\.\.|…|\*+))$`)

func placeholder(v string) bool {
	v = strings.TrimSpace(v)
	return v == "" || placeholderRe.MatchString(v)
}

func placeholderArg(a string) bool {
	a = strings.TrimSpace(a)
	return a != "" && placeholderRe.MatchString(a) && !strings.HasPrefix(a, "-")
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Parse reads servers from text a person pasted: a JSON config from any
// app's instructions (Claude Desktop, Cursor, VS Code, Windsurf, Gemini,
// LM Studio), one server's JSON, a web address, or a command line such as
// "npx -y @modelcontextprotocol/server-filesystem ~/Documents".
func Parse(text string) ([]Spec, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("paste a server's settings, its web address, or the command that runs it")
	}
	if strings.HasPrefix(text, "{") || strings.HasPrefix(text, `"`) || strings.HasPrefix(text, "[") {
		specs, err := parseJSON(text)
		if err == nil && len(specs) > 0 {
			return specs, nil
		}
		if err != nil {
			return nil, err
		}
	}
	if u, err := url.Parse(text); err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && !strings.ContainsAny(text, " \n") {
		return []Spec{withPreset(Spec{Name: nameFromURL(u), URL: text})}, nil
	}
	return parseCommandLine(text)
}

func parseJSON(text string) ([]Spec, error) {
	clean := cleanJSON(text)
	var root map[string]json.RawMessage
	if err := json.Unmarshal([]byte(clean), &root); err != nil {
		// A fragment such as `"notion": { ... }` copied out of a bigger file.
		if err2 := json.Unmarshal([]byte("{"+clean+"}"), &root); err2 != nil {
			return nil, fmt.Errorf("that does not look like valid JSON: %v", err)
		}
	}
	return specsFrom(root)
}

// specsFrom finds servers in a decoded config, wherever the app keeps them.
func specsFrom(root map[string]json.RawMessage) ([]Spec, error) {
	for _, key := range []string{"mcpServers", "servers", "mcp_servers", "context_servers"} {
		if raw, ok := root[key]; ok {
			return serverMap(raw)
		}
	}
	if raw, ok := root["mcp"]; ok {
		var inner map[string]json.RawMessage
		if json.Unmarshal(raw, &inner) == nil {
			return specsFrom(inner)
		}
	}
	if isServer(root) {
		s, err := serverSpec("", root)
		if err != nil {
			return nil, err
		}
		return []Spec{s}, nil
	}
	raw, _ := json.Marshal(root)
	specs, err := serverMap(raw)
	if err == nil && len(specs) == 0 {
		return nil, errors.New("no servers were found in that text")
	}
	return specs, err
}

func serverMap(raw json.RawMessage) ([]Spec, error) {
	var m map[string]map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, errors.New("no servers were found in that text")
	}
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []Spec
	for _, name := range names {
		if !isServer(m[name]) {
			continue
		}
		s, err := serverSpec(name, m[name])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out = append(out, s)
	}
	return out, nil
}

func isServer(m map[string]json.RawMessage) bool {
	for _, k := range []string{"command", "url", "serverUrl", "httpUrl"} {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}

func serverSpec(name string, m map[string]json.RawMessage) (Spec, error) {
	str := func(k string) string {
		var s string
		_ = json.Unmarshal(m[k], &s)
		return strings.TrimSpace(s)
	}
	s := Spec{Name: name, Env: map[string]string{}, Headers: map[string]string{}}
	s.URL = firstNonEmpty(str("url"), str("serverUrl"), str("httpUrl"))
	if s.URL == "" {
		var cmd struct {
			Path string   `json:"path"`
			Args []string `json:"args"`
		}
		if json.Unmarshal(m["command"], &cmd) == nil && cmd.Path != "" {
			// Zed nests the command.
			s.Command, s.Args = cmd.Path, cmd.Args
		} else {
			s.Command = str("command")
			_ = json.Unmarshal(m["args"], &s.Args)
		}
		if !strings.Contains(s.Command, "/") && strings.Contains(s.Command, " ") && len(s.Args) == 0 {
			// "npx -y pkg" written as one string.
			fields := splitCommand(s.Command)
			s.Command, s.Args = fields[0], fields[1:]
		}
	}
	for k, v := range stringMap(m["env"]) {
		s.Env[k] = v
	}
	for k, v := range stringMap(m["headers"]) {
		s.Headers[k] = v
	}
	s.Dir = str("cwd")
	if s.Name == "" {
		if s.URL != "" {
			if u, err := url.Parse(s.URL); err == nil {
				s.Name = nameFromURL(u)
			}
		} else {
			s.Name = nameFromCommand(s.Command, s.Args)
		}
	} else {
		s.Name = titleCase(s.Name)
	}
	s = unwrapRemote(s)
	if err := s.Validate(); err != nil {
		return Spec{}, err
	}
	return withPreset(s), nil
}

// unwrapRemote turns "npx mcp-remote <address>", which apps without remote
// support use to reach a web server, into a direct connection, so
// Yggdrasil's own sign-in is used and no Node.js process is needed.
func unwrapRemote(s Spec) Spec {
	if s.Command == "" || len(s.Args) == 0 {
		return s
	}
	base := baseCommand(s.Command)
	if base != "npx" && base != "bunx" && base != "pnpm" && base != "mcp-remote" {
		return s
	}
	i := 0
	if base != "mcp-remote" {
		for i < len(s.Args) && strings.HasPrefix(s.Args[i], "-") || i < len(s.Args) && s.Args[i] == "dlx" {
			i++
		}
		if i >= len(s.Args) || packageOf(s.Args[i:i+1]) != "mcp-remote" {
			return s
		}
		i++
	}
	out := s.clone()
	out.Command, out.Args = "", nil
	for ; i < len(s.Args); i++ {
		a := s.Args[i]
		switch {
		case a == "--header" && i+1 < len(s.Args):
			k, v, _ := strings.Cut(s.Args[i+1], ":")
			// mcp-remote reads ${VAR} from env; keep the value itself.
			v = os.Expand(strings.TrimSpace(v), func(name string) string { return s.Env[name] })
			out.Headers[strings.TrimSpace(k)] = v
			i++
		case strings.HasPrefix(a, "http://") || strings.HasPrefix(a, "https://"):
			if out.URL == "" {
				out.URL = a
			}
		case strings.HasPrefix(a, "-"):
			// Ports, transports, and debug flags only matter to mcp-remote.
			if i+1 < len(s.Args) && !strings.HasPrefix(s.Args[i+1], "-") && !strings.HasPrefix(s.Args[i+1], "http") {
				i++
			}
		}
	}
	if out.URL == "" {
		return s
	}
	out.Env = map[string]string{}
	if out.Name == "" || out.Name == nameFromCommand(s.Command, s.Args) {
		if u, err := url.Parse(out.URL); err == nil {
			out.Name = nameFromURL(u)
		}
	}
	return out
}

func stringMap(raw json.RawMessage) map[string]string {
	out := map[string]string{}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return out
	}
	for k, v := range m {
		switch x := v.(type) {
		case string:
			out[k] = x
		case float64, bool:
			out[k] = fmt.Sprint(x)
		}
	}
	return out
}

// cleanJSON removes // and /* */ comments and trailing commas, which VS
// Code and Zed settings files allow.
func cleanJSON(s string) string {
	var b strings.Builder
	inString, escaped := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			b.WriteByte(c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch {
		case c == '"':
			inString = true
			b.WriteByte(c)
		case c == '/' && i+1 < len(s) && s[i+1] == '/':
			for i < len(s) && s[i] != '\n' {
				i++
			}
			b.WriteByte('\n')
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				i = len(s)
			} else {
				i += end + 3
			}
		default:
			b.WriteByte(c)
		}
	}
	return trailingCommaRe.ReplaceAllString(b.String(), "$1")
}

var trailingCommaRe = regexp.MustCompile(`,(\s*[}\]])`)

// parseCommandLine reads a command a README says to run, including
// "claude mcp add" lines and leading NAME=value variables.
func parseCommandLine(text string) ([]Spec, error) {
	text = strings.ReplaceAll(text, "\\\n", " ")
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i]
	}
	fields := splitCommand(text)
	if len(fields) == 0 {
		return nil, errors.New("paste a server's settings, its web address, or the command that runs it")
	}
	s := Spec{Env: map[string]string{}, Headers: map[string]string{}}
	// claude mcp add [--transport http] [-e K=V] [-H "K: V"] name [--] command|url args...
	if len(fields) > 3 && fields[0] == "claude" && fields[1] == "mcp" && fields[2] == "add" {
		rest := fields[3:]
		var positional []string
		for i := 0; i < len(rest); i++ {
			f := rest[i]
			switch {
			case f == "--":
				positional = append(positional, rest[i+1:]...)
				i = len(rest)
			case (f == "-e" || f == "--env") && i+1 < len(rest):
				k, v, _ := strings.Cut(rest[i+1], "=")
				s.Env[k] = v
				i++
			case (f == "-H" || f == "--header") && i+1 < len(rest):
				k, v, _ := strings.Cut(rest[i+1], ":")
				s.Headers[strings.TrimSpace(k)] = strings.TrimSpace(v)
				i++
			case (f == "-t" || f == "--transport" || f == "-s" || f == "--scope") && i+1 < len(rest):
				i++
			case strings.HasPrefix(f, "--transport=") || strings.HasPrefix(f, "--scope="):
			default:
				positional = append(positional, f)
			}
		}
		if len(positional) < 2 {
			return nil, errors.New("that claude mcp add line is missing the command or address")
		}
		s.Name = titleCase(positional[0])
		fields = positional[1:]
	}
	for len(fields) > 0 && envAssign.MatchString(fields[0]) {
		k, v, _ := strings.Cut(fields[0], "=")
		s.Env[k] = v
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return nil, errors.New("that line has no command in it")
	}
	if u, err := url.Parse(fields[0]); err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" {
		s.URL = fields[0]
		if s.Name == "" {
			s.Name = nameFromURL(u)
		}
	} else {
		s.Command, s.Args = fields[0], fields[1:]
		if s.Name == "" {
			s.Name = nameFromCommand(s.Command, s.Args)
		}
		s = unwrapRemote(s)
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return []Spec{withPreset(s)}, nil
}

var envAssign = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// splitCommand splits a command line on spaces, keeping quoted parts.
func splitCommand(s string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	has := false
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote, has = r, true
		case unicode.IsSpace(r):
			if cur.Len() > 0 || has {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 || has {
		out = append(out, cur.String())
	}
	return out
}

// nameFromURL names a remote server by its host: mcp.notion.com is Notion.
func nameFromURL(u *url.URL) string {
	host := strings.TrimPrefix(u.Hostname(), "www.")
	parts := strings.Split(host, ".")
	for _, p := range parts {
		if p != "mcp" && p != "api" && p != "app" && len(p) > 1 {
			return titleCase(p)
		}
	}
	return titleCase(host)
}

// nameFromCommand names a server by the package it runs:
// @modelcontextprotocol/server-filesystem is Filesystem.
func nameFromCommand(command string, args []string) string {
	pkg := ""
	for _, a := range args {
		if strings.HasPrefix(a, "-") || a == "run" || a == "x" || a == "exec" || a == "dlx" || a == "tool" {
			continue
		}
		pkg = a
		break
	}
	if pkg == "" {
		pkg = filepath.Base(command)
	}
	if i := strings.LastIndex(pkg, "@"); i > 0 {
		pkg = pkg[:i]
	}
	scope := ""
	if strings.HasPrefix(pkg, "@") {
		scope, pkg, _ = strings.Cut(strings.TrimPrefix(pkg, "@"), "/")
	}
	pkg = filepath.Base(pkg)
	for _, affix := range []string{"mcp-server-", "server-", "mcp-", "-mcp-server", "-mcp", "_mcp"} {
		if strings.HasPrefix(pkg, affix) {
			pkg = strings.TrimPrefix(pkg, affix)
		} else {
			pkg = strings.TrimSuffix(pkg, affix)
		}
	}
	if (pkg == "" || pkg == "mcp" || pkg == "server") && scope != "" {
		pkg = scope
	}
	if pkg == "" {
		pkg = "Tool source"
	}
	return titleCase(pkg)
}

func titleCase(s string) string {
	words := strings.FieldsFunc(s, func(r rune) bool { return r == '-' || r == '_' || r == ' ' || r == '.' })
	for i, w := range words {
		if w == strings.ToLower(w) {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// expandHome replaces a leading ~ with the home folder.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}
