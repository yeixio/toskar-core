package terminal

import (
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Network is what a command reaches off this computer, as well as can be
// told from its words: the hosts in its addresses, or the program, such as
// npm for its registry (#1426). The shell runs it, so Toskar can't see what
// it really sends; this is for What left this computer.
type Network struct {
	// Destinations are hosts, or a program when no address is given.
	Destinations []string
	// Command is the command, with user names, passwords, and tokens taken
	// out of it.
	Command string
}

// Programs that reach the network whatever they're given.
var networkPrograms = map[string]bool{
	"curl": true, "wget": true, "ssh": true, "scp": true, "sftp": true, "rsync": true, "ftp": true,
	"nc": true, "ncat": true, "telnet": true, "http": true, "https": true, "xh": true, "aria2c": true,
	"dig": true, "nslookup": true, "host": true, "ping": true, "traceroute": true, "whois": true,
	"gh": true, "aws": true, "gcloud": true, "az": true, "npx": true, "bunx": true, "uvx": true, "pnpx": true,
}

// Programs that reach the network for some of what they do, by that word.
var networkVerbs = map[string]map[string]bool{
	"git":     {"clone": true, "fetch": true, "pull": true, "push": true, "ls-remote": true, "submodule": true},
	"pip":     {"install": true, "download": true},
	"pip3":    {"install": true, "download": true},
	"uv":      {"pip": true, "add": true, "sync": true, "tool": true},
	"pipx":    {"install": true, "run": true},
	"npm":     {"install": true, "i": true, "ci": true, "add": true, "update": true, "publish": true, "view": true},
	"pnpm":    {"install": true, "i": true, "add": true, "update": true, "dlx": true, "publish": true},
	"yarn":    {"install": true, "add": true, "upgrade": true, "dlx": true, "publish": true},
	"bun":     {"install": true, "i": true, "add": true, "update": true, "x": true},
	"brew":    {"install": true, "upgrade": true, "update": true, "tap": true, "fetch": true},
	"apt":     {"install": true, "update": true, "upgrade": true},
	"apt-get": {"install": true, "update": true, "upgrade": true},
	"dnf":     {"install": true, "update": true, "upgrade": true},
	"yum":     {"install": true, "update": true},
	"go":      {"get": true, "install": true, "mod": true},
	"cargo":   {"install": true, "add": true, "fetch": true, "update": true, "publish": true},
	"gem":     {"install": true, "update": true},
	"docker":  {"pull": true, "push": true, "login": true},
	"podman":  {"pull": true, "push": true, "login": true},
	"ollama":  {"pull": true, "push": true},
}

// Words before a program that don't change what it is.
var prefixes = map[string]bool{"sudo": true, "env": true, "time": true, "nohup": true, "exec": true, "command": true}

var (
	separators = regexp.MustCompile(`&&|\|\||[;|&\n()` + "`" + `]|\$\(`)
	addresses  = regexp.MustCompile(`\b[a-zA-Z][a-zA-Z0-9+.-]*://[^\s'"<>()` + "`" + `]+`)
	scpLike    = regexp.MustCompile(`\b[\w.-]+@([a-zA-Z0-9.-]+\.[a-zA-Z]{2,}):`)
	headers    = regexp.MustCompile(`(?i)\b((?:proxy-)?authorization:\s*|x-api-key:\s*|cookie:\s*)[^'"\n]+`)
	secrets    = regexp.MustCompile(`(?i)(bearer\s+|token[=:]\s*|password[=:]\s*|--password[= ]\s*|-u\s+)[^\s'"]+`)
)

// Reaches is what command reaches, or false when nothing in it says it goes
// online.
func Reaches(command string) (Network, bool) {
	var programs []string
	for _, segment := range separators.Split(command, -1) {
		words := strings.Fields(segment)
		for len(words) > 0 && (prefixes[words[0]] || strings.Contains(words[0], "=") && !strings.HasPrefix(words[0], "-")) {
			words = words[1:]
		}
		if len(words) == 0 {
			continue
		}
		name := strings.ToLower(filepath.Base(words[0]))
		name = strings.TrimSuffix(name, ".exe")
		// python -m pip install
		if strings.HasPrefix(name, "python") && len(words) > 2 && words[1] == "-m" {
			name, words = words[2], words[2:]
		}
		if networkPrograms[name] {
			programs = append(programs, name)
			continue
		}
		if verbs, ok := networkVerbs[name]; ok {
			for _, w := range words[1:] {
				if strings.HasPrefix(w, "-") {
					continue
				}
				if verbs[w] {
					programs = append(programs, name)
				}
				break
			}
		}
	}
	if len(programs) == 0 {
		return Network{}, false
	}
	seen := map[string]bool{}
	var hosts []string
	add := func(h string) {
		h = strings.ToLower(strings.TrimSuffix(h, "."))
		if h != "" && !seen[h] {
			seen[h] = true
			hosts = append(hosts, h)
		}
	}
	for _, raw := range addresses.FindAllString(command, -1) {
		if u, err := url.Parse(raw); err == nil {
			add(u.Hostname())
		}
	}
	for _, m := range scpLike.FindAllStringSubmatch(command, -1) {
		add(m[1])
	}
	if len(hosts) == 0 {
		for _, p := range programs {
			add(p)
		}
	}
	sort.Strings(hosts)
	return Network{Destinations: hosts, Command: scrubCommand(command)}, true
}

// scrubCommand takes user names, passwords, and tokens out of a command.
func scrubCommand(command string) string {
	out := addresses.ReplaceAllStringFunc(command, func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil || u.User == nil {
			return raw
		}
		u.User = nil
		return u.String()
	})
	out = headers.ReplaceAllString(out, "${1}…")
	return secrets.ReplaceAllString(out, "${1}…")
}
