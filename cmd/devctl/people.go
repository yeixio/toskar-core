package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"
)

// Managing people without a browser (#592): toskarctl people …, as the
// Owner on this computer, or with an Admin's key (TOSKAR_API_KEY) from
// elsewhere (TOSKAR_URL).

const peopleUsage = `usage:
  toskarctl people list [--role member] [--disabled true|false] [--output json]
  toskarctl people add NAME [--role member] [--email ADDRESS] [--username NAME --password-stdin [--must-change]]
  toskarctl people show ID|NAME
  toskarctl people set ID|NAME [--role R] [--name N] [--email E] [--disable|--enable] [--username U] [--password-stdin [--must-change]]
  toskarctl people link ID|NAME
  toskarctl people keys ID|NAME [--revoke KEY-ID]
  toskarctl people devices ID|NAME [--revoke KEY-ID]
  toskarctl people rm ID|NAME [--transfer-to ID|NAME] [--yes]
  toskarctl people activity [ID|NAME]

Passwords are read from standard input, never from the command line,
where they'd show in the process list and shell history.`

type person struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Username   string     `json:"username,omitempty"`
	Role       string     `json:"role"`
	Email      string     `json:"email,omitempty"`
	SignIn     bool       `json:"sign_in"`
	MustChange bool       `json:"must_change,omitempty"`
	Disabled   *time.Time `json:"disabled_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type personLink struct {
	Path      string    `json:"path"`
	Kind      string    `json:"kind"`
	ExpiresAt time.Time `json:"expires_at"`
}

type personKey struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

type activityEntry struct {
	At         time.Time `json:"at"`
	ActorName  string    `json:"actor_name"`
	TargetName string    `json:"target_name"`
	Action     string    `json:"action"`
	Detail     string    `json:"detail,omitempty"`
}

// peopleIO is where the command reads a password and the confirmation
// to remove someone, and writes its prompts.
type peopleIO struct {
	in     io.Reader
	out    io.Writer
	prompt io.Writer
	// terminal reports stdin is a terminal, where a password is typed with
	// echo off.
	terminal bool
}

func peopleCommand(args []string, c daemonClient, pio peopleIO) error {
	if len(args) == 0 {
		return &exitError{code: 2, msg: peopleUsage}
	}
	sub, rest := args[0], args[1:]
	fs := flag.NewFlagSet("people", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	output := outputFlag(fs)
	role := fs.String("role", "", "")
	disabled := fs.String("disabled", "", "")
	email := fs.String("email", "", "")
	name := fs.String("name", "", "")
	username := fs.String("username", "", "")
	passwordStdin := fs.Bool("password-stdin", false, "")
	mustChange := fs.Bool("must-change", false, "")
	disable := fs.Bool("disable", false, "")
	enable := fs.Bool("enable", false, "")
	revoke := fs.String("revoke", "", "")
	transferTo := fs.String("transfer-to", "", "")
	yes := fs.Bool("yes", false, "")
	positional, err := parseInterleaved(fs, rest)
	if err != nil {
		return &exitError{code: 2, msg: err.Error() + "\n" + peopleUsage}
	}
	if err := checkOutput(*output); err != nil {
		return err
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	json := *output == "json"
	out := pio.out
	fail := func(err error) error { return &exitError{code: 1, msg: err.Error()} }
	one := func() (person, error) {
		if len(positional) != 1 {
			return person{}, &exitError{code: 2, msg: peopleUsage}
		}
		p, err := findPerson(c, positional[0])
		if err != nil {
			return person{}, fail(err)
		}
		return p, nil
	}
	password := func() (string, error) {
		if !*passwordStdin {
			return "", nil
		}
		pw, err := readPassword(pio)
		if err != nil {
			return "", fail(err)
		}
		return pw, nil
	}

	switch sub {
	case "list":
		if len(positional) != 0 {
			return &exitError{code: 2, msg: peopleUsage}
		}
		q := url.Values{}
		if *role != "" {
			q.Set("role", *role)
		}
		if *disabled != "" {
			q.Set("disabled", *disabled)
		}
		var list []person
		path := "/people"
		if len(q) > 0 {
			path += "?" + q.Encode()
		}
		if err := c.request(http.MethodGet, path, nil, &list); err != nil {
			return fail(err)
		}
		if json {
			return writeJSON(out, list)
		}
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tNAME\tROLE\tUSERNAME\tEMAIL\tSTATUS")
		for _, p := range list {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", p.ID, p.Name, p.Role, dash(p.Username), dash(p.Email), status(p))
		}
		return tw.Flush()

	case "add":
		if len(positional) != 1 {
			return &exitError{code: 2, msg: peopleUsage}
		}
		if *passwordStdin && *username == "" {
			return &exitError{code: 2, msg: "--password-stdin needs --username, the name they sign in with"}
		}
		pw, err := password()
		if err != nil {
			return err
		}
		body := map[string]any{"name": positional[0], "role": *role, "email": *email}
		if pw != "" {
			body["username"], body["password"], body["must_change"] = *username, pw, *mustChange
		}
		var added struct {
			Person person      `json:"person"`
			Link   *personLink `json:"link"`
		}
		if err := c.request(http.MethodPost, "/people", body, &added); err != nil {
			return fail(err)
		}
		if json {
			return writeJSON(out, added)
		}
		fmt.Fprintf(out, "Added %s (%s, ID %s).\n", added.Person.Name, added.Person.Role, added.Person.ID)
		if added.Link != nil {
			fmt.Fprintf(out, "They set up their sign-in with this link, which works once until %s:\n\n  %s\n",
				added.Link.ExpiresAt.Local().Format("Jan 2 15:04"), c.base+added.Link.Path)
		} else if added.Person.MustChange {
			fmt.Fprintf(out, "They sign in as %s and choose a new password first.\n", added.Person.Username)
		}
		return nil

	case "show":
		p, err := one()
		if err != nil {
			return err
		}
		if json {
			return writeJSON(out, p)
		}
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "ID\t%s\nName\t%s\nRole\t%s\nUsername\t%s\nEmail\t%s\nStatus\t%s\nAdded\t%s\n",
			p.ID, p.Name, p.Role, dash(p.Username), dash(p.Email), status(p), p.CreatedAt.Local().Format("2006-01-02"))
		return tw.Flush()

	case "set":
		p, err := one()
		if err != nil {
			return err
		}
		if *disable && *enable {
			return &exitError{code: 2, msg: "--disable and --enable can't go together"}
		}
		body := map[string]any{}
		if set["role"] {
			body["role"] = *role
		}
		if set["name"] {
			body["name"] = *name
		}
		if set["email"] {
			body["email"] = *email
		}
		if *disable || *enable {
			body["disabled"] = *disable
		}
		pw, err := password()
		if err != nil {
			return err
		}
		if pw != "" {
			body["password"], body["username"], body["must_change"] = pw, *username, *mustChange
		}
		if len(body) == 0 {
			return &exitError{code: 2, msg: "nothing to change\n" + peopleUsage}
		}
		var changed person
		if err := c.request(http.MethodPatch, "/people/"+p.ID, body, &changed); err != nil {
			return fail(err)
		}
		if json {
			return writeJSON(out, changed)
		}
		fmt.Fprintf(out, "Changed %s: %s, %s.\n", changed.Name, changed.Role, status(changed))
		return nil

	case "link":
		p, err := one()
		if err != nil {
			return err
		}
		var link personLink
		if err := c.request(http.MethodPost, "/people/"+p.ID+"/link", nil, &link); err != nil {
			return fail(err)
		}
		if json {
			return writeJSON(out, link)
		}
		what := "set up their sign-in"
		if link.Kind == "reset" {
			what = "choose a new password"
		}
		fmt.Fprintf(out, "%s can %s with this link, which works once until %s:\n\n  %s\n", p.Name, what,
			link.ExpiresAt.Local().Format("Jan 2 15:04"), c.base+link.Path)
		return nil

	case "keys", "devices":
		p, err := one()
		if err != nil {
			return err
		}
		base := "/people/" + p.ID + "/" + sub
		if *revoke != "" {
			if err := c.request(http.MethodDelete, base+"/"+url.PathEscape(*revoke), nil, nil); err != nil {
				return fail(err)
			}
			if !json {
				fmt.Fprintf(out, "Revoked %s for %s.\n", *revoke, p.Name)
			}
			return nil
		}
		var list []personKey
		if err := c.request(http.MethodGet, base, nil, &list); err != nil {
			return fail(err)
		}
		if json {
			return writeJSON(out, list)
		}
		if len(list) == 0 {
			fmt.Fprintf(out, "%s has no %s.\n", p.Name, map[string]string{"keys": "API keys", "devices": "connected devices"}[sub])
			return nil
		}
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tNAME\tPREFIX\tCREATED\tLAST USED")
		for _, k := range list {
			used := "never"
			if k.LastUsedAt != nil {
				used = k.LastUsedAt.Local().Format("2006-01-02")
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", k.ID, k.Name, k.Prefix, k.CreatedAt.Local().Format("2006-01-02"), used)
		}
		return tw.Flush()

	case "rm":
		p, err := one()
		if err != nil {
			return err
		}
		q := ""
		to := person{}
		if *transferTo != "" {
			if to, err = findPerson(c, *transferTo); err != nil {
				return fail(err)
			}
			q = "?transfer_to=" + url.QueryEscape(to.ID)
		}
		if !*yes {
			what := "Their chats, memories, automations, and files are deleted."
			if to.ID != "" {
				what = "Their chats, memories, automations, and files go to " + to.Name + "."
			}
			fmt.Fprintf(pio.prompt, "Remove %s? %s Their keys and devices stop working.\nType their name to confirm: ", p.Name, what)
			typed, _ := bufio.NewReader(pio.in).ReadString('\n')
			if strings.TrimSpace(typed) != p.Name {
				return &exitError{code: 1, msg: "not removed: the name didn't match"}
			}
		}
		if err := c.request(http.MethodDelete, "/people/"+p.ID+q, map[string]string{"confirm": p.Name}, nil); err != nil {
			return fail(err)
		}
		if !json {
			fmt.Fprintf(out, "Removed %s.\n", p.Name)
		}
		return nil

	case "activity":
		path := "/people/activity"
		if len(positional) == 1 {
			p, err := findPerson(c, positional[0])
			if err != nil {
				return fail(err)
			}
			path += "?person=" + url.QueryEscape(p.ID)
		} else if len(positional) > 1 {
			return &exitError{code: 2, msg: peopleUsage}
		}
		var list []activityEntry
		if err := c.request(http.MethodGet, path, nil, &list); err != nil {
			return fail(err)
		}
		if json {
			return writeJSON(out, list)
		}
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "WHEN\tWHO\tDID\tTO\tDETAIL")
		for _, e := range list {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", e.At.Local().Format("2006-01-02 15:04"), e.ActorName, e.Action, e.TargetName, e.Detail)
		}
		return tw.Flush()
	}
	return &exitError{code: 2, msg: peopleUsage}
}

// parseInterleaved parses flags wherever they are among the positional
// arguments, as in "people add Ana --role member".
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return positional, nil
		}
		positional = append(positional, args[0])
		args = args[1:]
	}
}

// findPerson is the person with this ID, or else the one with this name or
// username; more than one with a name is an error that lists them.
func findPerson(c daemonClient, ref string) (person, error) {
	var list []person
	if err := c.request(http.MethodGet, "/people", nil, &list); err != nil {
		return person{}, err
	}
	var named []person
	for _, p := range list {
		if p.ID == ref {
			return p, nil
		}
		if strings.EqualFold(p.Name, ref) || (p.Username != "" && strings.EqualFold(p.Username, ref)) {
			named = append(named, p)
		}
	}
	switch len(named) {
	case 0:
		return person{}, fmt.Errorf("nobody here is called %q; see toskarctl people list", ref)
	case 1:
		return named[0], nil
	}
	ids := make([]string, len(named))
	for i, p := range named {
		ids[i] = p.ID
	}
	return person{}, fmt.Errorf("more than one person is called %q; use an ID: %s", ref, strings.Join(ids, ", "))
}

// readPassword reads one line from standard input: piped in, or typed at
// a terminal with echo off where stty can turn it off.
func readPassword(pio peopleIO) (string, error) {
	if pio.terminal {
		fmt.Fprint(pio.prompt, "Password: ")
		if runtime.GOOS != "windows" {
			if off := exec.Command("stty", "-echo"); func() error { off.Stdin = os.Stdin; return off.Run() }() == nil {
				defer func() {
					on := exec.Command("stty", "echo")
					on.Stdin = os.Stdin
					_ = on.Run()
					fmt.Fprintln(pio.prompt)
				}()
			}
		}
	}
	line, err := bufio.NewReader(pio.in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return "", errors.New("no password on standard input")
	}
	return line, nil
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func status(p person) string {
	switch {
	case p.Disabled != nil:
		return "disabled"
	case p.MustChange:
		return "must change password"
	case p.SignIn:
		return "signs in"
	}
	return "invited"
}
