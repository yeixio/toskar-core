package main

import (
	"bufio"
	"bytes"
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

// chosenModel is what a run measures. Source is the file's link for a model
// from outside the list.
type chosenModel struct {
	ID, Name, Source string
}

// chooseModel offers the most popular model that fits, one nobody has measured
// yet, anything else on the list, or any model by its link.
func chooseModel(toskarURL string, ask *asker) (chosenModel, error) {
	var listed struct {
		Models []qualityscore.ListedModel `json:"models"`
	}
	if err := readJSON(modelListURL, &listed); err != nil {
		return chosenModel{}, fmt.Errorf("couldn't read the model list: %w; name a model with --model", err)
	}
	var published struct {
		Results []qualityscore.Row `json:"results"`
	}
	// Only offering something new depends on this, so it can fail.
	_ = readJSON(resultsURL, &published)
	hardware, room, err := hardwareRoom(toskarURL)
	if err != nil {
		return chosenModel{}, err
	}
	fmt.Printf("This computer: %s.\n\n", hardware)
	popular, hasPopular := qualityscore.Popular(listed.Models, room)
	unmeasured, hasUnmeasured := qualityscore.Unmeasured(listed.Models, published.Results, room)
	if hasUnmeasured && unmeasured.ID == popular.ID {
		hasUnmeasured = false
		fmt.Println("The most popular model that fits has no results yet, so yours would be the first.")
	}
	// Numbered as they're shown, since the first two aren't always there.
	var actions []func() (chosenModel, error)
	offer := func(label string, action func() (chosenModel, error)) {
		actions = append(actions, action)
		fmt.Printf("  %d  %s\n", len(actions), label)
	}
	fmt.Println("Which model?")
	if hasPopular {
		offer(fmt.Sprintf("%s, a %s download: popular, and fits this computer", popular.Name, gigabytes(popular.SizeBytes)), func() (chosenModel, error) {
			return chosenModel{ID: popular.ID, Name: popular.Name}, nil
		})
	}
	if hasUnmeasured {
		offer(fmt.Sprintf("%s, a %s download: nobody has measured it yet, so yours would be the first", unmeasured.Name, gigabytes(unmeasured.SizeBytes)), func() (chosenModel, error) {
			return chosenModel{ID: unmeasured.ID, Name: unmeasured.Name}, nil
		})
	}
	stopped := errors.New("stopped; nothing was downloaded")
	offer("Another from our list", func() (chosenModel, error) {
		fits := qualityscore.Fitting(listed.Models, room)
		for i, m := range fits {
			fmt.Printf("%3d  %s, %s\n", i+1, m.Name, gigabytes(m.SizeBytes))
		}
		choice, err := strconv.Atoi(ask.ask("Which one?"))
		if err != nil || choice < 1 || choice > len(fits) {
			return chosenModel{}, stopped
		}
		return chosenModel{ID: fits[choice-1].ID, Name: fits[choice-1].Name}, nil
	})
	offer("Any other model, by its Hugging Face link", func() (chosenModel, error) {
		return chooseLink(toskarURL, ask.ask("Paste the link to its .gguf file on Hugging Face:"), ask, room)
	})
	fmt.Println("A small model took about 25 minutes on a recent laptop. Larger models and slower computers can take hours.")
	answer := ask.ask("Enter a number, or n to stop. [1]")
	if answer == "" {
		answer = "1"
	}
	choice, err := strconv.Atoi(answer)
	if err != nil || choice < 1 || choice > len(actions) {
		return chosenModel{}, stopped
	}
	return actions[choice-1]()
}

// chooseLink is a model from outside the list: Toskar says what its file is
// before anything downloads.
func chooseLink(toskarURL, link string, ask *asker, room uint64) (chosenModel, error) {
	if !qualityscore.ModelLink(link) {
		return chosenModel{}, fmt.Errorf("that isn't a link to a .gguf file on Hugging Face, such as https://huggingface.co/owner/model/resolve/main/file.gguf")
	}
	body, err := json.Marshal(map[string]string{"source_url": link})
	if err != nil {
		return chosenModel{}, err
	}
	client := http.Client{Timeout: time.Minute}
	resp, err := client.Post(toskarURL+"/api/v1/models/install-from-url/preview", "application/json", bytes.NewReader(body))
	if err != nil {
		return chosenModel{}, err
	}
	defer resp.Body.Close()
	var preview struct {
		ModelID   string `json:"model_id"`
		SizeBytes uint64 `json:"size_bytes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&preview); err != nil || resp.StatusCode != http.StatusOK || preview.ModelID == "" {
		return chosenModel{}, fmt.Errorf("couldn't read that model (status %d)", resp.StatusCode)
	}
	if preview.SizeBytes+preview.SizeBytes/4 > room {
		fmt.Printf("It may not fit: %s, with %s for a model here. If it runs out of memory, that's a result too.\n", gigabytes(preview.SizeBytes), gigabytes(room))
	}
	answer := strings.ToLower(ask.ask(fmt.Sprintf("Measure %s, a %s download? [Y/n]", preview.ModelID, gigabytes(preview.SizeBytes))))
	if answer == "n" || answer == "no" {
		return chosenModel{}, errors.New("stopped; nothing was downloaded")
	}
	return chosenModel{ID: preview.ModelID, Name: preview.ModelID, Source: link}, nil
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
	fmt.Printf("Open this to check and send it:\n%s&result=%s\n", resultsForm, url.QueryEscape(string(raw)))
}

func tail(text string, most int) string {
	if len(text) > most {
		return text[len(text)-most:]
	}
	return text
}
