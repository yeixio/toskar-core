package main

import (
	"fmt"
	"strings"
)

const docsURL = "https://toskar.ai/docs"

// helpFlag is one flag in a command's help.
type helpFlag struct{ name, about string }

// helpEntry is a command or subcommand's help. Every example is run against a
// real Core in CI (examples/test.py), so each has to work on a new install.
type helpEntry struct {
	name, summary string
	about         string
	usage         []string
	flags         []helpFlag
	examples      []string
	subs          []helpEntry
}

type helpGroup struct {
	title    string
	commands []helpEntry
}

const outputFlagAbout = "print JSON: json is the only value"

var helpGroups = []helpGroup{
	{"Chat with Toskar", []helpEntry{
		{
			name: "ask", summary: "Send one prompt and print the answer, for scripts and CI jobs",
			about: "Sends one prompt and prints the answer. Text piped in, or --file, is sent as data to work on, apart from the prompt. Memory, knowledge, and tools are off unless asked for.\n\nExit status: 0 an answer, 1 a model or connection error, 2 usage, 3 an answer that doesn't fit --json-schema.",
			usage: []string{"toskarctl ask [flags] [prompt]"},
			flags: []helpFlag{
				{"--model id", "a model, auto, or sai:<slug> (default auto)"},
				{"--profile id", "answer as this profile instead"},
				{"--system text", "a system prompt"},
				{"--file path", "data to work on, from a file (- for standard input)"},
				{"--json-schema path", "answer with JSON that fits this JSON Schema"},
				{"--max-tokens n", "the longest answer, in tokens"},
				{"--temperature t", "sampling temperature, such as 0 for the steadiest answers"},
				{"--no-thinking", "a reasoning model answers without thinking first: faster"},
				{"--timeout d", "how long to wait for the answer (default 5m)"},
				{"--memory", "let the answer use and add to memory"},
				{"--knowledge", "let the answer use connected knowledge"},
			},
			examples: []string{
				`toskarctl ask "Name three rivers in Alaska"`,
				`echo "line 12: missing semicolon" | toskarctl ask "What failed?"`,
			},
		},
		{
			name: "chat", summary: "Hold a conversation in the terminal, in the same chats as the apps",
			about: "Chats with Toskar in the terminal: one begun here continues on your phone and back. A tool that asks first, such as running code, shows what it would do and waits for y, n, or a (always, in this chat). Piped in, it answers once, with what's piped as data to work on. If Toskar isn't running on this computer, it starts it, and Toskar stays running after the chat. With --project, it starts or continues in one of your projects, and still works in the folder you're in. With --swarm, several agents work on each message at once, each shown as a line updated in place, then the answer comes.\n\nWhile chatting: /new, /model id, /profile id, /attach path, /project name (none to take the chat out; alone, it says which), /swarm [split|race|off] (a swarm for the next message), /agents (the last swarm's agents), /open N (continue agent N's chat), /back, /log, /help, and /exit (or Ctrl-D).",
			usage: []string{"toskarctl chat [flags] [message]"},
			flags: []helpFlag{
				{"--resume [id]", "continue a chat: its id, or pick from recent ones"},
				{"--model id", "a model, or auto (default auto)"},
				{"--profile id", "answer as this profile"},
				{"--file path", "data to work on with the first message"},
				{"--project name", "start or continue in a project: its name or id"},
				{"--swarm [split|race]", "send each message to a swarm of agents (default split)"},
			},
			examples: []string{`echo "Build failed on line 12" | toskarctl chat "Why did this fail?"`},
		},
		{
			name: "projects", summary: "List your projects, to chat in one with chat --project",
			about:    "Lists your projects, most recently used first: each one's id, name, and how many chats are in it. toskarctl chat --project name starts or continues a chat in one. Make and change projects in the web or desktop app.",
			usage:    []string{"toskarctl projects [list] [--output json]"},
			flags:    []helpFlag{{"--output json", outputFlagAbout}},
			examples: []string{"toskarctl projects"},
		},
	}},
	{"Models", []helpEntry{
		{
			name: "models", summary: "List the models Toskar offers, and install them",
			about: "Lists the models Toskar offers, and installs them.",
			usage: []string{"toskarctl models <command>"},
			subs: []helpEntry{
				{
					name: "list", summary: "The models, smallest first, with their size and how well each fits this computer",
					about:    "Lists the models Toskar offers, smallest first, with their download size and how well each fits this computer.",
					usage:    []string{"toskarctl models list [flags]"},
					flags:    []helpFlag{{"--installed", "only the installed ones"}, {"--json", "print JSON"}},
					examples: []string{"toskarctl models list", "toskarctl models list --installed --json"},
				},
				{
					name: "install", summary: "Download a model and wait until it's installed",
					about: "Downloads a model and waits until it's installed, showing its progress. Find an id with toskarctl models list.",
					usage: []string{"toskarctl models install <id> [flags]"},
					flags: []helpFlag{{"--node id", "install on this computer in the network (default this one)"}},
				},
			},
			examples: []string{"toskarctl models list"},
		},
	}},
	{"Automations", []helpEntry{
		{
			name: "automations", summary: "Schedule work for Toskar to do by itself",
			about: "Manages automations: work Toskar does on a schedule, or when something happens.",
			usage: []string{"toskarctl automations <command>"},
			subs: []helpEntry{
				{name: "list", summary: "List automations", about: "Lists automations.", usage: []string{"toskarctl automations list"}, examples: []string{"toskarctl automations list"}},
				{name: "get", summary: "Show an automation", about: "Shows an automation.", usage: []string{"toskarctl automations get <id>"}},
				{
					name: "parse", summary: "Show what a request in your own words would schedule, without making it",
					about:    "Shows what a request in your own words would schedule, without making it.",
					usage:    []string{"toskarctl automations parse <request> [flags]"},
					flags:    []helpFlag{{"--zone tz", "the time zone (default this computer's)"}, {"--language tag", "the request's language"}},
					examples: []string{`toskarctl automations parse "every friday at 9:00 AM, summarize this week's news"`},
				},
				{
					name: "create", summary: "Make an automation, from your own words or from flags",
					about:    "Makes an automation. With --request, Toskar reads it from your own words, and any other flag changes what it read. Otherwise give --name, --prompt, and --schedule.\n\nTimes: daily, weekly, and monthly use HH:MM, or several such as 08:00,17:00. once uses RFC3339. interval uses Go durations such as 6h. --weekday takes 0-6 (Sunday is 0) or names, several such as 1,3,5 or mon-fri, or \"weekdays\". monthly runs on --day, or a month's last day when it has fewer. cron takes five fields, such as --cron \"0 9 * * 1-5\".",
					usage:    []string{"toskarctl automations create --request <text> [flags]", "toskarctl automations create --name <name> --prompt <text> --schedule <kind> [flags]"},
					flags:    automationFlags,
					examples: []string{`toskarctl automations create --name "Morning news" --prompt "Summarize the news" --schedule daily --at 08:00`},
				},
				{
					name: "update", summary: "Change an automation",
					about: "Changes an automation: the same flags as create, for what should change. --camera with an empty value removes the cameras.",
					usage: []string{"toskarctl automations update <id> [flags]"},
					flags: automationFlags,
				},
				{name: "delete", summary: "Delete an automation", about: "Deletes an automation.", usage: []string{"toskarctl automations delete <id>"}},
				{name: "run", summary: "Run an automation now, and wait for it", about: "Runs an automation now, and waits for it to finish.", usage: []string{"toskarctl automations run <id>"}},
				{name: "pause", summary: "Stop an automation running until it's resumed", about: "Stops an automation running until it's resumed.", usage: []string{"toskarctl automations pause <id>"}},
				{name: "resume", summary: "Start a paused automation again", about: "Starts a paused automation again.", usage: []string{"toskarctl automations resume <id>"}},
				{name: "hook", summary: "Print a new webhook link", about: "Prints a new webhook link for an automation with --trigger webhook. The old one stops working.", usage: []string{"toskarctl automations hook <id>"}},
			},
			examples: []string{"toskarctl automations list"},
		},
	}},
	{"Connect other tools", []helpEntry{
		{
			name: "connect", summary: "Set up a coding agent or editor to use a model on Toskar",
			about: "Prints the settings a coding agent needs to use a model on Toskar, or writes them with --write. The agent drives the model directly, with its own tools.\n\nAgents: claude-code, codex, opencode, aider.\nEditors, which print what to enter: continue, cline, roo-code, zed, jetbrains, copilot.\n\nOn another computer, set TOSKAR_API_KEY to a key with raw access (API Access, \"Drive models directly, for coding agents\").",
			usage: []string{"toskarctl connect <agent> [flags]"},
			flags: []helpFlag{
				{"--model id", "the model (default the best installed one for coding agents)"},
				{"--write", "write the settings instead of printing them; only the agent's Toskar settings change"},
			},
			examples: []string{"toskarctl connect aider --model qwen2.5-7b-q4"},
		},
		{
			name: "mcp", summary: "Let an app that starts MCP servers as programs use Toskar",
			about:    "Lets an app that starts MCP servers as programs, such as a desktop chat app, use Toskar. The app starts this command, and it passes each message on to Toskar.",
			usage:    []string{"toskarctl mcp [flags]"},
			flags:    []helpFlag{{"--url address", "where Toskar is (default TOSKAR_URL, or this computer)"}},
			examples: []string{"toskarctl mcp < /dev/null"},
		},
	}},
	{"Computers", []helpEntry{
		{
			name: "join", summary: "Join this computer to a network, with the command another computer printed",
			about: "Joins this computer to the Toskar network of the computer that made the join command. Run the command that computer printed, as it is. Make one with toskarctl join-token create.",
			usage: []string{"toskarctl join --server <host:port> --token <token> --fingerprint <sha256:…> [flags]"},
			flags: []helpFlag{
				{"--server host:port", "the computer to join"},
				{"--token token", "the one-time join token"},
				{"--fingerprint sha256:…", "that computer's fingerprint, so it can't be another's"},
				{"--name name", "rename this computer as it joins"},
				{"--wait d", "wait this long for Toskar to start here, for provisioning scripts"},
				{"--output json", outputFlagAbout},
			},
		},
		{
			name: "join-token", summary: "Make the one-time tokens that add computers",
			about: "Makes, lists, and stops the one-time tokens that add a computer to the network.",
			usage: []string{"toskarctl join-token <command> [--output json]"},
			subs: []helpEntry{
				{
					name: "create", summary: "Make a token, and print the command to run on the new computer",
					about: "Makes a one-time join token, and prints the command to run on the new computer.",
					usage: []string{"toskarctl join-token create [flags]"},
					flags: []helpFlag{{"--ttl d", "how long it lasts, 1m to 24h (default 15m)"}, {"--output json", outputFlagAbout}},
				},
				{name: "list", summary: "List tokens made in the last week", about: "Lists tokens made in the last week.", usage: []string{"toskarctl join-token list [--output json]"}, flags: []helpFlag{{"--output json", outputFlagAbout}}, examples: []string{"toskarctl join-token list"}},
				{name: "revoke", summary: "Stop an unused token", about: "Stops an unused token.", usage: []string{"toskarctl join-token revoke <id> [--output json]"}, flags: []helpFlag{{"--output json", outputFlagAbout}}},
			},
			examples: []string{"toskarctl join-token list"},
		},
		{
			name: "network", summary: "This computer's address, fingerprint, network, and paired computers",
			about:    "Shows this computer's network, its address and fingerprint, and the computers it trusts.",
			usage:    []string{"toskarctl network [status] [--output json]"},
			flags:    []helpFlag{{"--output json", outputFlagAbout}},
			examples: []string{"toskarctl network"},
		},
		{
			name: "leave", summary: "Leave the network, and forget the paired computers",
			about: "Leaves the network: tells each paired computer, then forgets them all. Models, settings, and this computer's identity stay.",
			usage: []string{"toskarctl leave [--output json]"},
			flags: []helpFlag{{"--output json", outputFlagAbout}},
		},
	}},
	{"People", []helpEntry{
		{
			name: "people", summary: "Add, change, and remove the people who use this Toskar",
			about: "Adds, changes, and removes the people who use this Toskar. Passwords are read from standard input, never from the command line, where they'd show in the process list and shell history.",
			usage: []string{"toskarctl people <command>"},
			subs: []helpEntry{
				{
					name: "list", summary: "List people", about: "Lists people.",
					usage:    []string{"toskarctl people list [flags]"},
					flags:    []helpFlag{{"--role role", "only this role, such as member"}, {"--disabled true|false", "only people who are, or aren't, turned off"}, {"--output json", outputFlagAbout}},
					examples: []string{"toskarctl people list"},
				},
				{
					name: "add", summary: "Add someone", about: "Adds someone. With --username and --password-stdin, they can sign in.",
					usage: []string{"toskarctl people add NAME [flags]"},
					flags: []helpFlag{{"--role role", "their role (default member)"}, {"--email address", "their email"}, {"--username name", "the name they sign in with"}, {"--password-stdin", "read their password from standard input"}, {"--must-change", "they choose a new password when they sign in"}},
				},
				{name: "show", summary: "Show someone", about: "Shows someone.", usage: []string{"toskarctl people show ID|NAME"}},
				{
					name: "set", summary: "Change someone", about: "Changes someone: their role, name, email, sign-in, or whether they're turned off.",
					usage: []string{"toskarctl people set ID|NAME [flags]"},
					flags: []helpFlag{{"--role role", "their role"}, {"--name name", "their name"}, {"--email address", "their email"}, {"--disable", "turn them off"}, {"--enable", "turn them back on"}, {"--username name", "the name they sign in with"}, {"--password-stdin", "read their password from standard input"}, {"--must-change", "they choose a new password when they sign in"}},
				},
				{name: "link", summary: "Make a link for them to set up sign-in", about: "Makes a link for them to set up sign-in.", usage: []string{"toskarctl people link ID|NAME"}},
				{name: "keys", summary: "List their API keys, or stop one", about: "Lists their API keys, or stops one.", usage: []string{"toskarctl people keys ID|NAME [flags]"}, flags: []helpFlag{{"--revoke key-id", "stop this key"}}},
				{name: "devices", summary: "List their devices, or disconnect one", about: "Lists their devices, or disconnects one.", usage: []string{"toskarctl people devices ID|NAME [flags]"}, flags: []helpFlag{{"--revoke key-id", "disconnect this device"}}},
				{name: "rm", summary: "Remove someone", about: "Removes someone. It asks first unless given --yes.", usage: []string{"toskarctl people rm ID|NAME [flags]"}, flags: []helpFlag{{"--transfer-to id|name", "give their chats and automations to this person"}, {"--yes", "don't ask first"}}},
				{name: "activity", summary: "What someone has done, or everyone", about: "Shows what someone has done, or everyone.", usage: []string{"toskarctl people activity [ID|NAME]"}, examples: []string{"toskarctl people activity"}},
			},
			examples: []string{"toskarctl people list"},
		},
	}},
	{"About Toskar", []helpEntry{
		{name: "version", summary: "The version, license, and source", about: "Prints the version, license, and corresponding source.", usage: []string{"toskarctl version"}, examples: []string{"toskarctl version"}},
		{name: "about", summary: "The same as version", about: "Prints the version, license, and corresponding source.", usage: []string{"toskarctl about"}, examples: []string{"toskarctl about"}},
		{name: "paths", summary: "Where Toskar keeps its data, models, and logs", about: "Prints the default data, model, runtime, log, and database paths.", usage: []string{"toskarctl paths"}, examples: []string{"toskarctl paths"}},
		{
			name: "completion", summary: "Print a shell completion script",
			about:    "Prints a shell completion script.\n\nbash: eval \"$(toskarctl completion bash)\"\nzsh:  toskarctl completion zsh > ~/.zfunc/_toskarctl   (with ~/.zfunc on fpath before compinit)\nfish: toskarctl completion fish > ~/.config/fish/completions/toskarctl.fish",
			usage:    []string{"toskarctl completion <bash|zsh|fish>"},
			examples: []string{"toskarctl completion bash"},
		},
		{name: "help", summary: "Help for any command", about: "Shows help for any command. toskarctl <command> --help is the same.", usage: []string{"toskarctl help [command] [subcommand]"}, examples: []string{"toskarctl help models install"}},
	}},
}

// automationFlags are what create and update take.
var automationFlags = []helpFlag{
	{"--request text", "what it should do and when, in your own words (create only)"},
	{"--name name", "its name"},
	{"--prompt text", "what it does each time"},
	{"--profile id", "run as this profile"},
	{"--model id", "run with this model"},
	{"--schedule kind", "once, daily, weekly, monthly, interval, cron, or manual"},
	{"--at time", "when, for once, daily, weekly, and monthly"},
	{"--every duration", "how often, for interval"},
	{"--weekday days", "which days, for weekly"},
	{"--day n", "which day from 1 to 31, for monthly"},
	{"--cron expr", "five fields, for cron"},
	{"--zone tz", "the time zone (default this computer's)"},
	{"--tool id", "a tool it can use"},
	{"--notify mode", "how it tells you"},
	{"--save-folder path", "save what it makes here"},
	{"--camera entity", "a camera it looks at, such as camera.door; repeat for more"},
	{"--trigger kind", "page, feed, folder, webhook, after, device, or none"},
	{"--trigger-url url", "the page or feed to watch"},
	{"--trigger-path path", "the folder to watch"},
	{"--after id", "run after this automation"},
	{"--after-when notified", "only when that one notified you"},
	{"--device entity", "the device to watch"},
	{"--to state", "only when the device changes to this"},
	{"--cooldown duration", "the least time between runs"},
	{"--disabled", "make it turned off"},
}

// topHelp is toskarctl help: every command, by what it's for.
func topHelp() string {
	var b strings.Builder
	b.WriteString("toskarctl talks to Toskar from a terminal or a script: chat with it, manage its models, and connect your tools and computers.\n\n")
	b.WriteString("Usage:\n  toskarctl <command> [flags]\n")
	for _, g := range helpGroups {
		fmt.Fprintf(&b, "\n%s:\n", g.title)
		writeList(&b, g.commands)
	}
	b.WriteString("\nExamples:\n")
	for _, e := range []string{`toskarctl ask "Name three rivers in Alaska"`, "toskarctl models list --installed", "toskarctl help chat"} {
		fmt.Fprintf(&b, "  %s\n", e)
	}
	b.WriteString("\nEnvironment:\n")
	b.WriteString("  TOSKAR_URL      where Toskar is (default http://127.0.0.1:7331, this computer)\n")
	b.WriteString("  TOSKAR_API_KEY  a key, for Toskar on another computer\n")
	b.WriteString("\nEvery command takes --help. toskarctl help <command> is the same.\n")
	fmt.Fprintf(&b, "Docs: %s\n", docsURL)
	return b.String()
}

func writeList(b *strings.Builder, entries []helpEntry) {
	width := 0
	for _, e := range entries {
		width = max(width, len(e.name))
	}
	for _, e := range entries {
		fmt.Fprintf(b, "  %-*s  %s\n", width, e.name, e.summary)
	}
}

// text is one command's help, always in the same shape.
func (e helpEntry) text(path string) string {
	var b strings.Builder
	b.WriteString(e.about)
	b.WriteString("\n\nUsage:\n")
	for _, u := range e.usage {
		fmt.Fprintf(&b, "  %s\n", u)
	}
	if len(e.subs) > 0 {
		b.WriteString("\nCommands:\n")
		writeList(&b, e.subs)
	}
	if len(e.flags) > 0 {
		b.WriteString("\nFlags:\n")
		width := 0
		for _, f := range e.flags {
			width = max(width, len(f.name))
		}
		for _, f := range e.flags {
			fmt.Fprintf(&b, "  %-*s  %s\n", width, f.name, f.about)
		}
	}
	if len(e.examples) > 0 {
		b.WriteString("\nExamples:\n")
		for _, x := range e.examples {
			fmt.Fprintf(&b, "  %s\n", x)
		}
	}
	if len(e.subs) > 0 {
		fmt.Fprintf(&b, "\nMore on each: toskarctl help %s <command>\n", path)
	}
	fmt.Fprintf(&b, "\nDocs: %s\n", docsURL)
	return b.String()
}

func helpEntries() []helpEntry {
	var all []helpEntry
	for _, g := range helpGroups {
		all = append(all, g.commands...)
	}
	return all
}

func entryNames(entries []helpEntry) []string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.name
	}
	return names
}

func findEntry(entries []helpEntry, name string) (helpEntry, bool) {
	for _, e := range entries {
		if e.name == name {
			return e, true
		}
	}
	return helpEntry{}, false
}

// helpTopic is the help for a command path such as models install.
func helpTopic(words []string) (string, error) {
	if len(words) == 0 {
		return topHelp(), nil
	}
	entry, ok := findEntry(helpEntries(), words[0])
	if !ok {
		return "", unknownCommand("", words[0], helpEntries())
	}
	if len(words) == 1 {
		return entry.text(words[0]), nil
	}
	sub, ok := findEntry(entry.subs, words[1])
	if !ok {
		return "", unknownCommand(words[0]+" ", words[1], entry.subs)
	}
	return sub.text(words[0] + " " + words[1]), nil
}

// askedForHelp says whether args ask for help, and which: no arguments, help,
// or --help or -h anywhere before --. A prompt that is only "--help" goes
// after --, as with any other program.
func askedForHelp(args []string) ([]string, bool) {
	if len(args) == 0 {
		return nil, true
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		return args[1:], true
	}
	for _, a := range args {
		if a == "--" {
			return nil, false
		}
		if a != "--help" && a != "-h" {
			continue
		}
		entry, ok := findEntry(helpEntries(), args[0])
		if !ok {
			return args[:1], true
		}
		if len(args) > 1 {
			if _, ok := findEntry(entry.subs, args[1]); ok {
				return args[:2], true
			}
		}
		return args[:1], true
	}
	return nil, false
}

// unknownCommand says what was wrong and suggests the nearest command.
func unknownCommand(parent, word string, entries []helpEntry) error {
	msg := fmt.Sprintf("toskarctl has no command %q.", parent+word)
	if near := nearestName(word, entryNames(entries)); near != "" {
		msg += fmt.Sprintf(" Did you mean %s?", parent+near)
	}
	return &exitError{code: 2, msg: msg + "\nSee them all with: " + strings.TrimSpace("toskarctl help "+parent)}
}

// nearestName is the name a mistyped word is closest to, if any is close.
func nearestName(word string, names []string) string {
	best, bestDistance := "", 3
	for _, name := range names {
		if d := editDistance(word, name); d < bestDistance {
			best, bestDistance = name, d
		}
	}
	return best
}

func editDistance(a, b string) int {
	row := make([]int, len(b)+1)
	for j := range row {
		row[j] = j
	}
	for i := 1; i <= len(a); i++ {
		diagonal := row[0]
		row[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			above := diagonal
			diagonal = row[j]
			row[j] = min(row[j]+1, row[j-1]+1, above+cost)
		}
	}
	return row[len(b)]
}
