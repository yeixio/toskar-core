package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"text/tabwriter"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Projects in the terminal (#1136): toskarctl projects lists yours, and
// toskarctl chat --project name chats in one. They're made and changed in
// the apps.

const projectsUsage = `usage: toskarctl projects [list] [--output json]
  Lists your projects, most recently used first: each one's id, name, and
  how many chats are in it. toskarctl chat --project name chats in one.`

func projectsCommand(args []string, c daemonClient, out io.Writer) error {
	fs := flag.NewFlagSet("projects", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	output := outputFlag(fs)
	positional, err := parseInterleaved(fs, args)
	if err != nil {
		return &exitError{code: 2, msg: err.Error() + "\n" + projectsUsage}
	}
	if len(positional) > 1 || (len(positional) == 1 && positional[0] != "list") {
		return &exitError{code: 2, msg: projectsUsage}
	}
	if err := checkOutput(*output); err != nil {
		return err
	}
	var list []contracts.Project
	if err := c.request(http.MethodGet, "/projects", nil, &list); err != nil {
		return &exitError{code: 1, msg: err.Error()}
	}
	if *output == "json" {
		return writeJSON(out, list)
	}
	if len(list) == 0 {
		fmt.Fprintln(out, "You have no projects yet. Make one in Chat in the web or desktop app: open History and choose New project.")
		return nil
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tCHATS")
	for _, p := range list {
		fmt.Fprintf(tw, "%s\t%s\t%d\n", p.ID, p.Name, p.ChatCount)
	}
	return tw.Flush()
}
