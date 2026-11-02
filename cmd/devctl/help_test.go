package main

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Every command main runs has help, and help lists nothing main doesn't run.
func TestHelpListsEveryCommand(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var run []string
	ast.Inspect(file, func(n ast.Node) bool {
		sw, ok := n.(*ast.SwitchStmt)
		if !ok {
			return true
		}
		if tag, ok := sw.Tag.(*ast.IndexExpr); !ok || !strings.Contains(types(tag), "Args") {
			return true
		}
		for _, clause := range sw.Body.List {
			for _, e := range clause.(*ast.CaseClause).List {
				name, err := strconv.Unquote(e.(*ast.BasicLit).Value)
				if err != nil {
					t.Fatal(err)
				}
				run = append(run, name)
			}
		}
		return false
	})
	if len(run) == 0 {
		t.Fatal("found no commands in main.go")
	}
	listed := entryNames(helpEntries())
	want := append(slices.Clone(run), "help")
	slices.Sort(want)
	slices.Sort(listed)
	if !slices.Equal(want, listed) {
		t.Fatalf("main.go runs %v; help lists %v", want, listed)
	}
}

func types(e *ast.IndexExpr) string {
	if sel, ok := e.X.(*ast.SelectorExpr); ok {
		return sel.Sel.Name
	}
	return ""
}

// Completions offer every command and subcommand help lists.
func TestCompletionsOfferEveryCommand(t *testing.T) {
	for shell, path := range completionPaths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range helpEntries() {
			if !regexp.MustCompile(`(^|[\s'"])` + regexp.QuoteMeta(e.name) + `\b`).Match(raw) {
				t.Errorf("%s doesn't offer %s", shell, e.name)
			}
			for _, sub := range e.subs {
				if !regexp.MustCompile(`\b` + regexp.QuoteMeta(sub.name) + `\b`).Match(raw) {
					t.Errorf("%s doesn't offer %s %s", shell, e.name, sub.name)
				}
			}
		}
	}
}

// Each help has the same shape: what it does, usage, and how to read more.
func TestHelpShape(t *testing.T) {
	// These change the network or need something from another computer, so
	// they can't show an example that runs anywhere.
	noExamples := []string{"join", "leave"}
	check := func(path string, e helpEntry) {
		t.Helper()
		if e.summary == "" || e.about == "" || len(e.usage) == 0 {
			t.Errorf("%s: needs a summary, what it does, and usage", path)
		}
		for _, u := range e.usage {
			if !strings.HasPrefix(u, "toskarctl "+path) {
				t.Errorf("%s: usage %q", path, u)
			}
		}
		text := e.text(path)
		if !strings.Contains(text, "Usage:\n") || !strings.HasSuffix(text, "Docs: "+docsURL+"\n") {
			t.Errorf("%s:\n%s", path, text)
		}
		for _, x := range e.examples {
			if !strings.Contains(x, "toskarctl "+strings.Fields(path)[0]) {
				t.Errorf("%s: example %q is for something else", path, x)
			}
		}
	}
	for _, e := range helpEntries() {
		check(e.name, e)
		if len(e.examples) == 0 && !slices.Contains(noExamples, e.name) {
			t.Errorf("%s has no examples", e.name)
		}
		for _, sub := range e.subs {
			check(e.name+" "+sub.name, sub)
		}
	}
}

// The usage a command prints when it's used wrongly and its help name the same flags.
func TestHelpNamesEveryFlag(t *testing.T) {
	flag := regexp.MustCompile(`--[a-z][a-z-]*`)
	for name, usage := range map[string]string{
		"ask": askUsage, "chat": chatUsage, "connect": connectUsage, "models": modelsUsage, "automations": automationsUsage,
		"join": joinUsage, "join-token": joinTokenUsage, "network": networkUsage, "leave": leaveUsage, "people": peopleUsage,
	} {
		entry, ok := findEntry(helpEntries(), name)
		if !ok {
			t.Fatalf("no help for %s", name)
		}
		text := entry.text(name)
		for _, sub := range entry.subs {
			text += sub.text(name + " " + sub.name)
		}
		for _, f := range flag.FindAllString(usage, -1) {
			// connect's usage says to run codex --profile toskar: Codex's own flag.
			if name == "connect" && f == "--profile" {
				continue
			}
			if !regexp.MustCompile(regexp.QuoteMeta(f) + `\b`).MatchString(text) {
				t.Errorf("%s --help doesn't mention %s", name, f)
			}
		}
	}
}

func TestAskedForHelp(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
		ok   bool
	}{
		{nil, "", true},
		{[]string{"help"}, "", true},
		{[]string{"--help"}, "", true},
		{[]string{"-h"}, "", true},
		{[]string{"help", "models", "install"}, "models install", true},
		{[]string{"models", "--help"}, "models", true},
		{[]string{"models", "install", "--help"}, "models install", true},
		{[]string{"models", "install", "x", "-h"}, "models install", true},
		{[]string{"ask", "--model", "m", "--help"}, "ask", true},
		{[]string{"nosuch", "--help"}, "nosuch", true},
		{[]string{"ask", "what does --help do?"}, "", false},
		{[]string{"ask", "--", "--help"}, "", false},
		{[]string{"models", "list"}, "", false},
	} {
		words, ok := askedForHelp(c.args)
		if ok != c.ok || strings.Join(words, " ") != c.want {
			t.Errorf("%v: got %v, %v", c.args, words, ok)
		}
	}
}

func TestHelpTopics(t *testing.T) {
	top, err := helpTopic(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Usage:", "Chat with Toskar:", "models", "TOSKAR_URL", "Docs: " + docsURL} {
		if !strings.Contains(top, want) {
			t.Errorf("no %q in:\n%s", want, top)
		}
	}
	sub, err := helpTopic([]string{"models", "install"})
	if err != nil || !strings.Contains(sub, "toskarctl models install <id>") {
		t.Fatalf("%v\n%s", err, sub)
	}
	for _, c := range []struct {
		words []string
		want  string
	}{
		{[]string{"modle"}, "Did you mean models?"},
		{[]string{"cat"}, "Did you mean chat?"},
		{[]string{"models", "instal"}, "Did you mean models install?"},
		{[]string{"xyzzyplugh"}, "See them all with: toskarctl help"},
	} {
		_, err := helpTopic(c.words)
		var e *exitError
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v", c.words, err)
		}
		if !errors.As(err, &e) || e.code != 2 {
			t.Errorf("%v: should exit 2: %v", c.words, err)
		}
	}
}
