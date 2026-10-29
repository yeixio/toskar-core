package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

const modelsUsage = `usage: toskarctl models <list|install>
  list [--installed] [--json]   the models Toskar offers, with their size and how well each fits this computer
  install <id> [--node <id>]    download a model and wait until it's installed, showing its progress
The daemon address is TOSKAR_URL, or 127.0.0.1:7331. A remote daemon uses TOSKAR_API_KEY.`

func modelsCommand(args []string, client daemonClient, out io.Writer) error {
	if len(args) == 0 {
		return &exitError{code: 2, msg: modelsUsage}
	}
	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("list", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		installed := fs.Bool("installed", false, "only installed models")
		asJSON := fs.Bool("json", false, "print JSON")
		if err := fs.Parse(args[1:]); err != nil {
			return &exitError{code: 2, msg: modelsUsage}
		}
		models, err := listModels(client)
		if err != nil {
			return err
		}
		if *installed {
			var kept []contracts.Model
			for _, m := range models {
				if m.Installed {
					kept = append(kept, m)
				}
			}
			models = kept
		}
		if *asJSON {
			return writeJSON(out, models)
		}
		return writeModels(out, models)
	case "install":
		fs := flag.NewFlagSet("install", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		node := fs.String("node", "", "the computer to install it on; empty is the one toskarctl talks to")
		id, rest := "", args[1:]
		if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
			id, rest = rest[0], rest[1:]
		}
		if err := fs.Parse(rest); err != nil || id == "" || fs.NArg() > 0 {
			return &exitError{code: 2, msg: modelsUsage}
		}
		return installModel(client, id, *node, out)
	}
	return &exitError{code: 2, msg: modelsUsage}
}

// listModels is the catalog, smallest first, each with its fit on the
// computer toskarctl talks to (the first /models/fit lists).
func listModels(client daemonClient) ([]contracts.Model, error) {
	var models []contracts.Model
	if err := client.call(http.MethodGet, "/models", nil, &models); err != nil {
		return nil, err
	}
	var fits []contracts.ModelsFitResponse
	if client.call(http.MethodGet, "/models/fit", nil, &fits) == nil && len(fits) > 0 {
		byID := map[string]contracts.ModelFit{}
		for _, f := range fits[0].Fits {
			byID[f.ModelID] = f
		}
		for i := range models {
			if f, ok := byID[models[i].ID]; ok && models[i].Fit == nil {
				models[i].Fit = &f
			}
		}
	}
	sort.SliceStable(models, func(i, j int) bool { return models[i].SizeBytes < models[j].SizeBytes })
	return models, nil
}

// writeModels prints one model a line: its id, to install it by, then its
// name, download size, fit, and whether it's installed. Supporting models,
// which never answer chats, are left out.
func writeModels(out io.Writer, models []contracts.Model) error {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tSIZE\tFIT\tINSTALLED")
	for _, m := range models {
		if contracts.SupportRoleOf(m) != "" {
			continue
		}
		fit := "-"
		if m.Fit != nil && m.Fit.Label != "" {
			fit = string(m.Fit.Label)
		}
		installed := ""
		if m.Installed {
			installed = "yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", m.ID, m.DisplayName, sizeText(m.SizeBytes), fit, installed)
	}
	return w.Flush()
}

// sizeText is a size as a person reads it: 4.7 GB, or 84 MB under a gigabyte.
func sizeText(n uint64) string {
	switch {
	case n == 0:
		return "-"
	case n < 1e9:
		return fmt.Sprintf("%d MB", (n+500_000)/1e6)
	}
	return fmt.Sprintf("%.1f GB", float64(n)/1e9)
}

// installModel installs a model and waits for it, printing its download's
// progress from the daemon's events every tenth of the way.
func installModel(client daemonClient, id, node string, out io.Writer) error {
	models, err := listModels(client)
	if err != nil {
		return err
	}
	var model *contracts.Model
	for i := range models {
		if models[i].ID == id {
			model = &models[i]
		}
	}
	if model == nil {
		return fmt.Errorf("there's no model %q; toskarctl models list shows the models there are", id)
	}
	name := model.DisplayName
	if name == "" {
		name = id
	}
	if model.Installed && node == "" {
		fmt.Fprintf(out, "%s is already installed.\n", name)
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchDownload(ctx, client, id, name, out)
	}()
	body := map[string]any{"wait": true}
	if node != "" {
		body["node_id"] = node
	}
	err = client.call(http.MethodPost, "/models/"+id+"/install", body, nil)
	// The last progress can still be on its way when the install answers.
	select {
	case <-done:
	case <-time.After(watchGrace):
	}
	cancel()
	<-done
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Installed %s.\n", name)
	return nil
}

// watchGrace is how long an install that has answered waits for the rest
// of its progress.
var watchGrace = 2 * time.Second

// watchDownload prints a model's download progress from /events until it
// finishes or ctx ends. It's only for showing progress: when the events can't be read, the
// install still finishes.
func watchDownload(ctx context.Context, client daemonClient, id, name string, out io.Writer) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, client.base+"/api/v1/events", nil)
	if err != nil {
		return
	}
	if client.key != "" {
		req.Header.Set("Authorization", "Bearer "+client.key)
	}
	hc := client.client
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	shown := -1
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		data, ok := strings.CutPrefix(scanner.Text(), "data: ")
		if !ok {
			continue
		}
		var evt struct {
			Type    string `json:"type"`
			Payload struct {
				ModelID         string  `json:"model_id"`
				BytesDownloaded uint64  `json:"bytes_downloaded"`
				BytesTotal      uint64  `json:"bytes_total"`
				Percent         float64 `json:"percent"`
			} `json:"payload"`
		}
		if json.Unmarshal([]byte(data), &evt) != nil || evt.Payload.ModelID != id {
			continue
		}
		if evt.Type == "model.download.completed" || evt.Type == "model.download.failed" {
			return
		}
		if evt.Type != "model.download.progress" {
			continue
		}
		if tenth := int(evt.Payload.Percent) / 10; tenth > shown {
			shown = tenth
			fmt.Fprintf(out, "Downloading %s: %d%% (%s of %s)\n", name, tenth*10, sizeText(evt.Payload.BytesDownloaded), sizeText(evt.Payload.BytesTotal))
		}
		if evt.Payload.Percent >= 100 {
			return
		}
	}
}
