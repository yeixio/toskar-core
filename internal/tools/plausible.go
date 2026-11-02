package tools

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

// implausibleCall reports tool arguments that are not a real path or command.
// Those calls must not open the approval dialog.
func implausibleCall(toolID string, args map[string]any) error {
	switch toolID {
	case "internet.search":
		query, _ := stringArg(args, "query")
		if query == "" {
			return fmt.Errorf("query required")
		}
	case "internet.open":
		rawURL, _ := stringArg(args, "url")
		if rawURL == "" || !strings.Contains(rawURL, "://") {
			return fmt.Errorf("%q is not a web address", rawURL)
		}
	case "filesystem.search":
		query, _ := stringArg(args, "query")
		if query == "" {
			return fmt.Errorf("query required")
		}
	case "filesystem.read", "filesystem.write":
		path, _ := stringArg(args, "path")
		if path == "" {
			return fmt.Errorf("path required")
		}
		if !plausiblePath(path) {
			return fmt.Errorf("%q is not a file path; answer without reading a file", path)
		}
	case "git.diff", "git.add":
		path, _ := stringArg(args, "path")
		if path == "" || path == "." {
			return nil
		}
		if !plausiblePath(path) {
			return fmt.Errorf("%q is not a file path", path)
		}
	case "git.commit":
		message, _ := stringArg(args, "message")
		if message == "" {
			return fmt.Errorf("message required")
		}
	case "terminal":
		command, _ := stringArg(args, "command")
		if command == "" {
			return fmt.Errorf("command required")
		}
		if !plausibleCommand(command) {
			return fmt.Errorf("%q is not a shell command; answer without the terminal", command)
		}
	}
	return nil
}

func stringArg(args map[string]any, key string) (string, bool) {
	if args == nil {
		return "", false
	}
	value, ok := args[key].(string)
	return strings.TrimSpace(value), ok
}

func plausiblePath(path string) bool {
	if strings.ContainsAny(path, "\n\r?") {
		return false
	}
	if strings.ContainsAny(path, `/\`) || strings.HasPrefix(path, ".") {
		return true
	}
	ext := filepath.Ext(path)
	if ext != "" && ext != "." && !strings.ContainsAny(ext, " \t") && len(ext) <= 8 {
		return true
	}
	fields := strings.Fields(path)
	// Single names such as README, Makefile, and src stay approvable.
	// A phrase with no separator and no extension is not a path.
	return len(fields) <= 2
}

func plausibleCommand(command string) bool {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}
	first := strings.Trim(fields[0], `"'`)
	if _, skip := proseLead[strings.ToLower(first)]; skip {
		return false
	}
	if !commandToken(first) {
		return false
	}
	if len(fields) < 4 || hasShellSyntax(command, fields) {
		return true
	}
	for _, field := range fields[1:] {
		if _, prose := pathPrepositions[strings.ToLower(field)]; prose {
			return false
		}
	}
	return true
}

var proseLead = map[string]struct{}{
	"what": {}, "how": {}, "why": {}, "when": {}, "where": {}, "who": {}, "whom": {}, "which": {},
	"please": {}, "explain": {}, "tell": {}, "can": {}, "could": {}, "would": {}, "should": {},
}

var pathPrepositions = map[string]struct{}{
	"for": {}, "in": {}, "of": {}, "to": {}, "on": {}, "with": {},
}

func commandToken(token string) bool {
	if token == "" {
		return false
	}
	// A Windows path: C:\tools\run.exe, .\run.ps1.
	if len(token) > 2 && token[1] == ':' && token[2] == '\\' {
		token = token[2:]
	}
	for _, r := range token {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune(`._/~+-\`, r) {
			continue
		}
		return false
	}
	return true
}

func hasShellSyntax(command string, fields []string) bool {
	if strings.ContainsAny(command, `|;&><$`+"`") {
		return true
	}
	for _, field := range fields {
		if strings.HasPrefix(field, "-") {
			return true
		}
	}
	return false
}
