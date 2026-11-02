package tools

import (
	"regexp"
	"slices"
	"strings"
)

// CommandRisk is how far a terminal command can be trusted without asking.
type CommandRisk int

const (
	// CommandKnown is a command known to read, or to do ordinary project
	// work. It follows the profile and "always in this chat". Project
	// commands such as make run the project's own code, which is why the
	// tool still asks by default.
	CommandKnown CommandRisk = iota
	// CommandUnsure can't be said to be harmless, so it always asks.
	CommandUnsure
	// CommandDestructive deletes, overwrites, or changes the system. It
	// always asks, and says it can't be undone.
	CommandDestructive
)

// AlwaysAsks reports a command nothing may approve ahead of time (#1017):
// not a profile, not "always in this chat", and not an automation's grant.
func (r CommandRisk) AlwaysAsks() bool { return r != CommandKnown }

// destructiveWords find what deletes, overwrites, or changes the system,
// anywhere in a command it can't vouch for, so it's found inside sh -c, xargs,
// find -exec, or a script given inline.
var destructiveWords = regexp.MustCompile(`(?i)` + strings.Join([]string{
	`\b(rm|rmdir|unlink|shred|wipefs|mkfs(\.\w+)?|dd|fdisk|sfdisk|parted|gparted|diskutil|diskpart|format|truncate)\b`,
	`\b(sudo|doas|su|pkexec|runas)\b`,
	`\b(del|erase|rd|remove-item|clear-content|format-volume)\b`,
	`\b(systemctl|launchctl|service|sysctl|defaults write|reg (add|delete)|nvram|csrutil|setx|shutdown|reboot|halt|poweroff|kill|pkill|killall|crontab|iptables|ufw|mount|umount|useradd|userdel|passwd)\b`,
	`\b(chmod|chown|chattr|chflags)\b`,
	`-delete\b`,
	`\b(rmtree|removedirs|remove_all|removeall|rmsync|unlinksync|os\.remove|shutil\.move)\b`,
	`:\(\)\s*\{`,
	`>\s*/dev/(sd|hd|nvme|disk|rdisk|mapper)`,
}, "|"))

// gitDestructive finds what rewrites history or discards work. It's looked for
// first, since the commands around it are otherwise everyday git.
var gitDestructive = regexp.MustCompile(`(?i)` + strings.Join([]string{
	`\bgit\s+(\S+\s+)*push\s+(\S+\s+)*(--force\S*|-[a-z]*f[a-z]*|\+\S+|--delete|--mirror|-d)(\s|$)`,
	`\bgit\s+(\S+\s+)*(reset\s+(\S+\s+)*--(hard|merge|keep)|clean|filter-branch|filter-repo|rebase|reflog\s+(expire|delete)|gc\s+--prune|update-ref|replace|restore|stash\s+(drop|clear))\b`,
	`\bgit\s+(\S+\s+)*branch\s+(\S+\s+)*-[a-z]*[dD]\b`,
	`\bgit\s+(\S+\s+)*checkout\s+(\S+\s+)*(--|-f|--force)(\s|$)`,
}, "|"))

// commandShape finds what makes a command's effect impossible to read off it:
// substitutions, process substitution, inline or downloaded programs, here
// documents, and running what a variable holds.
var commandShape = regexp.MustCompile("\\$\\(|`|<\\(|>\\(|<<|\\$\\{|\\$[A-Za-z_]")

// harmlessRedirect is a redirection that writes nowhere that matters.
var harmlessRedirect = regexp.MustCompile(`\d?>>?\s*/dev/null|\d?>&\d`)

// readCommands only read, as long as no flag in their list is there.
var readCommands = map[string][]string{
	"ls": nil, "pwd": nil, "whoami": nil, "date": nil, "uname": nil, "hostname": nil, "id": nil, "df": nil, "du": nil,
	"wc": nil, "head": nil, "tail": {"-f", "--follow"}, "cat": nil, "echo": nil, "printf": nil, "which": nil, "file": nil, "stat": nil,
	"sort": {"-o", "--output", "--compress-program"}, "uniq": nil, "cut": nil, "tr": nil, "grep": nil, "egrep": nil, "fgrep": nil, "rg": {"--pre"}, "tree": {"-o"},
	"find": {"-delete", "-exec", "-execdir", "-ok", "-okdir", "-fprint", "-fprint0", "-fprintf", "-fls"},
}

// projectCommands are ordinary project work, by first and second word; an
// empty list is any second word.
var projectCommands = map[string][]string{
	"git":    {"status", "diff", "log", "show", "branch", "rev-parse", "ls-files", "blame", "describe", "add", "commit", "fetch", "pull", "switch", "stash"},
	"go":     {"build", "test", "vet", "fmt", "mod", "version", "env", "list"},
	"make":   {},
	"npm":    {"test", "run", "ci", "install", "ls", "version"},
	"pnpm":   {"test", "run", "install", "ls"},
	"yarn":   {"test", "run", "install"},
	"cargo":  {"build", "test", "check", "fmt", "clippy"},
	"pytest": {},
	"mkdir":  {},
	"touch":  {},
}

// ClassifyCommand says how far a terminal command can be trusted, and why it
// always asks when it does. Being unsure means asking: only commands it can
// read all of, and knows, are CommandKnown. Doing this exactly isn't possible
// for a shell, so it's built to fail toward asking.
func ClassifyCommand(command string) (CommandRisk, string) {
	// Quotes and backslashes can spell a word apart: r\m, 'r'm, "rm".
	plain := strings.NewReplacer(`\`+"\n", "", `\`, "", `'`, "", `"`, "").Replace(command)
	if m := gitDestructive.FindString(plain); m != "" {
		return CommandDestructive, "it can rewrite history or discard work: " + strings.TrimSpace(m)
	}
	why := ""
	segments, ok := splitCommands(command)
	switch {
	case commandShape.MatchString(command):
		why = "part of it is only known when it runs"
	case strings.ContainsAny(harmlessRedirect.ReplaceAllString(command, ""), "><"):
		why = "it writes to a file"
	case !ok || len(segments) == 0:
		why = "it couldn't be read all the way through"
	default:
		for _, words := range segments {
			if !knownCommand(words) {
				why = "it isn't a command known to be harmless: " + words[0]
				break
			}
		}
	}
	if why == "" {
		return CommandKnown, ""
	}
	// Only now do words count: "grep kill main.go" is just reading, but in
	// a command it can't vouch for, rm may be what runs.
	if m := destructiveWords.FindString(plain); m != "" {
		return CommandDestructive, "it can delete or change things outside this chat: " + strings.TrimSpace(m)
	}
	return CommandUnsure, why
}

func knownCommand(words []string) bool {
	name := words[0]
	if banned, ok := readCommands[name]; ok {
		for _, w := range words[1:] {
			for _, b := range banned {
				if w == b || strings.HasPrefix(w, b+"=") {
					return false
				}
			}
		}
		return true
	}
	seconds, ok := projectCommands[name]
	if !ok {
		return false
	}
	if len(seconds) == 0 {
		return true
	}
	if len(words) < 2 || !slices.Contains(seconds, words[1]) {
		return false
	}
	for _, w := range words[2:] {
		if strings.HasPrefix(w, "--output") || strings.HasPrefix(w, "--exec") || strings.HasPrefix(w, "--upload-pack") {
			return false
		}
	}
	return true
}

// splitCommands reads a command line into its simple commands, each as words,
// respecting quotes. ok is false for what it can't read: an open quote, or a
// command that sets variables first, which can change what runs (PATH).
func splitCommands(line string) (commands [][]string, ok bool) {
	var words []string
	var word strings.Builder
	inWord, quote := false, rune(0)
	endWord := func() {
		if inWord {
			words = append(words, word.String())
			word.Reset()
			inWord = false
		}
	}
	endCommand := func() {
		endWord()
		if len(words) > 0 {
			commands = append(commands, words)
			words = nil
		}
	}
	for _, r := range harmlessRedirect.ReplaceAllString(line, " ") {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == ' ' || r == '\t':
			endWord()
		case r == ';' || r == '&' || r == '|' || r == '\n' || r == '(' || r == ')':
			endCommand()
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, false
	}
	endCommand()
	for _, c := range commands {
		if strings.Contains(c[0], "=") {
			return nil, false
		}
	}
	return commands, true
}
