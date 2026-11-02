package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/internal/qualityscore"
)

const (
	modelListURL = "https://raw.githubusercontent.com/yeixio/toskar-model-data/main/quality/models.json"
	resultsURL   = "https://raw.githubusercontent.com/yeixio/toskar-model-data/main/quality/results.json"
)

// asker asks a person questions, on the terminal itself: with curl | sh, standard
// input is the script. yes means nobody's there, and each answer is the default.
type asker struct {
	yes  bool
	file *os.File
	in   *bufio.Reader
}

func newAsker(yes bool) *asker {
	a := &asker{yes: yes}
	if yes {
		return a
	}
	// /dev/tty on Linux and macOS; CONIN$ on Windows.
	for _, name := range []string{"/dev/tty", "CONIN$"} {
		if file, err := os.Open(name); err == nil {
			a.file, a.in = file, bufio.NewReader(file)
			return a
		}
	}
	a.in = bufio.NewReader(os.Stdin)
	return a
}

func (a *asker) close() {
	if a.file != nil {
		a.file.Close()
	}
}

// ask returns a trimmed answer, or "" when nobody answers.
func (a *asker) ask(question string) string {
	if a.yes {
		return ""
	}
	fmt.Print(question + " ")
	line, err := a.in.ReadString('\n')
	if err != nil && line == "" {
		return ""
	}
	return strings.TrimSpace(line)
}

func readJSON(address string, into any) error {
	client := http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(address)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: status %d", address, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(into)
}

// chooseModel suggests a model for this computer, and lets the person pick another.
func chooseModel(toskarURL string, ask *asker) (string, error) {
	var listed struct {
		Models []qualityscore.ListedModel `json:"models"`
	}
	if err := readJSON(modelListURL, &listed); err != nil {
		return "", fmt.Errorf("couldn't read the model list: %w; name a model with --model", err)
	}
	var published struct {
		Results []qualityscore.Row `json:"results"`
	}
	// Only suggesting something new depends on this, so it can fail.
	_ = readJSON(resultsURL, &published)
	hardware, room, err := hardwareRoom(toskarURL)
	if err != nil {
		return "", err
	}
	suggested, ok := qualityscore.Suggest(listed.Models, published.Results, hardware, room)
	if !ok {
		return "", fmt.Errorf("none of the listed models fits this computer's memory (%s)", gigabytes(room))
	}
	fmt.Printf("This computer: %s.\n\n", hardware)
	fmt.Printf("Suggested model: %s, a %s download.\n", suggested.Name, gigabytes(suggested.SizeBytes))
	fmt.Println("A small model took about 25 minutes on a recent laptop. Larger models and slower computers can take hours.")
	answer := ask.ask("Measure it? [Enter for yes, l to list others, n to stop]")
	switch strings.ToLower(answer) {
	case "", "y", "yes":
		return suggested.ID, nil
	case "n", "no":
		return "", errors.New("stopped; nothing was downloaded")
	}
	fits := qualityscore.Fitting(listed.Models, room)
	for i, m := range fits {
		fmt.Printf("%3d  %s, %s\n", i+1, m.Name, gigabytes(m.SizeBytes))
	}
	choice, err := strconv.Atoi(ask.ask("Which one?"))
	if err != nil || choice < 1 || choice > len(fits) {
		return "", errors.New("stopped; nothing was downloaded")
	}
	return fits[choice-1].ID, nil
}

// hardwareRoom is the name results use for this computer, and how much memory a model can use.
func hardwareRoom(toskarURL string) (string, uint64, error) {
	hw, err := readHardware(toskarURL)
	if err != nil {
		return "", 0, err
	}
	var dedicated uint64
	for _, a := range hw.Accelerators {
		if a.DedicatedVRAM > dedicated {
			dedicated = a.DedicatedVRAM
		}
	}
	return hw.label(), qualityscore.RoomFor(dedicated, hw.Memory.TotalBytes), nil
}

func gigabytes(bytes uint64) string { return fmt.Sprintf("%.1f GB", float64(bytes)/(1<<30)) }

// offerToShare shows exactly what would be sent, and gives a link that fills in
// the results form. Nothing leaves this computer unless the person sends the form.
func offerToShare(row qualityscore.Row, ask *asker, out string) {
	if ask.yes {
		return
	}
	if !strings.HasPrefix(strings.ToLower(ask.ask("\nShare this result in the public results? It's a form you check and send yourself. [y/N]")), "y") {
		return
	}
	row.Submitted = true
	name := strings.TrimPrefix(ask.ask("A GitHub username to credit, or Enter to show as Community. We remove it, or the row, if you ask:"), "@")
	if qualityscore.ValidCredit(name) {
		row.SubmittedBy = name
	} else if name != "" {
		fmt.Println("That isn't a GitHub username, so this shows as Community.")
	}
	raw, err := json.MarshalIndent(row, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(out+string(os.PathSeparator)+"result.json", append(raw, '\n'), 0o644)
	fmt.Printf("\nThis is everything it sends, and nothing names your computer:\n\n%s\n\n", raw)
	fmt.Printf("Open this to check and send it:\n%s&result=%s\n", resultsForm, url.QueryEscape("```json\n"+string(raw)+"\n```"))
}

func tail(text string, most int) string {
	if len(text) > most {
		return text[len(text)-most:]
	}
	return text
}
