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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/internal/qualityscore"
)

const (
	modelListURL = "https://raw.githubusercontent.com/yeixio/toskar-model-data/main/quality/models.json"
	resultsURL   = "https://raw.githubusercontent.com/yeixio/toskar-model-data/main/quality/results.json"
	// resultsEndpoint takes a result from someone with no GitHub account.
	resultsEndpoint = "https://toskar.ai/api/results"
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
	SizeBytes        uint64
}

// runPlan is what a person chose to measure.
type runPlan struct {
	Models          []chosenModel
	Many, SpeedOnly bool
}

func listedModel(m qualityscore.ListedModel) chosenModel {
	// No Source: a listed model isn't off the list, and the tests find its file there.
	return chosenModel{ID: m.ID, Name: m.Name, SizeBytes: m.SizeBytes}
}

// fitting is the listed models this computer can hold, and how much memory it has for one.
func fitting(url string) ([]qualityscore.ListedModel, uint64, error) {
	var listed struct {
		Models []qualityscore.ListedModel `json:"models"`
	}
	if err := readJSON(modelListURL, &listed); err != nil {
		return nil, 0, fmt.Errorf("couldn't read the model list: %w", err)
	}
	_, room, err := hardwareRoom(url)
	if err != nil {
		return nil, 0, err
	}
	return qualityscore.Fitting(listed.Models, room), room, nil
}

// everyModel is the models that fit, smallest first, so the most results come soonest.
func everyModel(fits []qualityscore.ListedModel, _ uint64, most int) []chosenModel {
	sort.SliceStable(fits, func(i, j int) bool { return fits[i].SizeBytes < fits[j].SizeBytes })
	if most > 0 && len(fits) > most {
		fits = fits[:most]
	}
	models := make([]chosenModel, len(fits))
	for i, m := range fits {
		models[i] = listedModel(m)
	}
	return models
}

// chooseModel offers the most popular model that fits, one nobody has measured
// yet, anything else on the list, or any model by its link.
func chooseModel(toskarURL string, ask *asker) (runPlan, error) {
	var listed struct {
		Models []qualityscore.ListedModel `json:"models"`
	}
	if err := readJSON(modelListURL, &listed); err != nil {
		return runPlan{}, fmt.Errorf("couldn't read the model list: %w; name a model with --model", err)
	}
	var published struct {
		Results []qualityscore.Row `json:"results"`
	}
	// Only offering something new depends on this, so it can fail.
	_ = readJSON(resultsURL, &published)
	hardware, room, err := hardwareRoom(toskarURL)
	if err != nil {
		return runPlan{}, err
	}
	fmt.Printf("This computer: %s.\n\n", hardware)
	popular, hasPopular := qualityscore.Popular(listed.Models, room)
	unmeasured, hasUnmeasured := qualityscore.Unmeasured(listed.Models, published.Results, room)
	if hasUnmeasured && unmeasured.ID == popular.ID {
		hasUnmeasured = false
		fmt.Println("The most popular model that fits has no results yet, so yours would be the first.")
	}
	// Numbered as they're shown, since the first two aren't always there.
	var actions []func() (runPlan, error)
	offer := func(label string, action func() (runPlan, error)) {
		actions = append(actions, action)
		fmt.Printf("  %d  %s\n", len(actions), label)
	}
	fmt.Println("Which model?")
	if hasPopular {
		offer(fmt.Sprintf("%s, a %s download: popular, and fits this computer", popular.Name, gigabytes(popular.SizeBytes)), func() (runPlan, error) {
			return runPlan{Models: []chosenModel{listedModel(popular)}}, nil
		})
	}
	if hasUnmeasured {
		offer(fmt.Sprintf("%s, a %s download: nobody has measured it yet, so yours would be the first", unmeasured.Name, gigabytes(unmeasured.SizeBytes)), func() (runPlan, error) {
			return runPlan{Models: []chosenModel{listedModel(unmeasured)}}, nil
		})
	}
	stopped := errors.New("stopped; nothing was downloaded")
	fits := qualityscore.Fitting(listed.Models, room)
	for _, m := range fits {
		if m.ID == qualityscore.ReferenceModel {
			offer(fmt.Sprintf("Just this computer's Toskar Score, in minutes: %s, a %s download, speed only", m.Name, gigabytes(m.SizeBytes)), func() (runPlan, error) {
				return runPlan{Models: []chosenModel{listedModel(m)}, SpeedOnly: true}, nil
			})
		}
	}
	offer("Another from our list", func() (runPlan, error) {
		for i, m := range fits {
			fmt.Printf("%3d  %s, %s\n", i+1, m.Name, gigabytes(m.SizeBytes))
		}
		choice, err := strconv.Atoi(ask.ask("Which one?"))
		if err != nil || choice < 1 || choice > len(fits) {
			return runPlan{}, stopped
		}
		return runPlan{Models: []chosenModel{listedModel(fits[choice-1])}}, nil
	})
	offer("Any other model, by its Hugging Face link", func() (runPlan, error) {
		one, err := chooseLink(toskarURL, ask.ask("Paste the link to its .gguf file on Hugging Face:"), ask, room)
		return runPlan{Models: []chosenModel{one}}, err
	})
	if len(fits) > 1 {
		offer(fmt.Sprintf("Every model that fits: %d models, %s of downloads", len(fits), gigabytes(totalSize(fits))), func() (runPlan, error) {
			return chooseEvery(fits, ask)
		})
	}
	fmt.Println("A small model took about 25 minutes on a recent laptop. Larger models and slower computers can take hours.")
	answer := ask.ask("Enter a number, or n to stop. [1]")
	if answer == "" {
		answer = "1"
	}
	choice, err := strconv.Atoi(answer)
	if err != nil || choice < 1 || choice > len(actions) {
		return runPlan{}, stopped
	}
	return actions[choice-1]()
}

func totalSize(models []qualityscore.ListedModel) uint64 {
	var total uint64
	for _, m := range models {
		total += m.SizeBytes
	}
	return total
}

// chooseEvery asks how much of every model to measure, saying first what it costs.
func chooseEvery(fits []qualityscore.ListedModel, ask *asker) (runPlan, error) {
	fmt.Printf("\nThat downloads %s, one model at a time, and removes each one it downloaded before the next. It keeps this computer awake, and running this line again picks up where it stopped.\n", gigabytes(totalSize(fits)))
	fmt.Printf("  1  Speed only: a few minutes each, plus the downloads. Its results have speed and no quality scores.\n")
	fmt.Printf("  2  Everything: from about 15 minutes to hours each, so this could take a day or more.\n")
	answer := ask.ask("Enter a number, or n to stop. [1]")
	if answer != "" && answer != "1" && answer != "2" {
		return runPlan{}, errors.New("stopped; nothing was downloaded")
	}
	return runPlan{Models: everyModel(fits, 0, 0), Many: true, SpeedOnly: answer != "2"}, nil
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
	return chosenModel{ID: preview.ModelID, Name: preview.ModelID, Source: link, SizeBytes: preview.SizeBytes}, nil
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

// offerToShare shows exactly what would be sent, then sends it, with no account
// needed, or gives a link that fills in the results form, for credit. Nothing
// leaves this computer unless the person chooses one.
func offerToShare(row qualityscore.Row, ask *asker, out string) {
	if ask.yes {
		return
	}
	row.Submitted = true
	raw, err := json.MarshalIndent(row, "", "  ")
	if err != nil {
		return
	}
	fmt.Printf("\nShare this in the public results? This is everything it sends, and nothing names your computer:\n\n%s\n\n", raw)
	fmt.Println("  1  Send it, no account needed. It shows as Community.")
	fmt.Println("  2  Send with credit: a form on GitHub, filled in, that you check and send.")
	fmt.Println("We check each result before it goes in, and remove your credit or the row if you ask.")
	switch ask.ask("Enter a number, or Enter to keep it to yourself.") {
	case "1":
		link, err := sendResult(resultsEndpoint, raw)
		if err != nil {
			fmt.Printf("Couldn't send it: %v\nYou can send it with the form instead:\n%s\n", err, formLink(raw))
			return
		}
		fmt.Printf("Sent. Follow it here: %s\n", link)
	case "2":
		name := strings.TrimPrefix(ask.ask("Your GitHub username, or Enter to show as Community:"), "@")
		if qualityscore.ValidCredit(name) {
			row.SubmittedBy = name
		} else if name != "" {
			fmt.Println("That isn't a GitHub username, so this shows as Community.")
		}
		if raw, err = json.MarshalIndent(row, "", "  "); err != nil {
			return
		}
		_ = os.WriteFile(out+string(os.PathSeparator)+"result.json", append(raw, '\n'), 0o644)
		fmt.Printf("Open this to check and send it:\n%s\n", formLink(raw))
	}
}

// offerToShareAll sends a whole run's results together, as one issue for review.
func offerToShareAll(rows []qualityscore.Row, ask *asker) {
	if ask.yes || len(rows) == 0 {
		return
	}
	if len(rows) > maxSent {
		rows = rows[:maxSent]
	}
	for i := range rows {
		rows[i].Submitted = true
		fmt.Printf("  %s: %s\n", rows[i].Name, rows[i].Runs)
	}
	raw, err := json.MarshalIndent(map[string]any{"rows": rows}, "", "  ")
	if err != nil {
		return
	}
	fmt.Println("\nShare these in the public results? Each sends its model, this computer's graphics card or chip, memory, operating system, the date, and its figures. Nothing names your computer. They show as Community, and we check them before they go in. For credit, send result.json from each folder with the form.")
	if !strings.HasPrefix(strings.ToLower(ask.ask("Send them? [y/N]")), "y") {
		return
	}
	link, err := sendResult(resultsEndpoint, raw)
	if err != nil {
		fmt.Printf("Couldn't send them: %v\nEach folder has its result.json for the form: %s\n", err, resultsForm)
		return
	}
	fmt.Printf("Sent. Follow them here: %s\n", link)
}

// maxSent is the most results toskar.ai takes at once.
const maxSent = 20

func formLink(raw []byte) string {
	return resultsForm + "&result=" + url.QueryEscape(string(raw))
}

// sendResult posts a result to toskar.ai, which files it for review, and returns where to follow it.
func sendResult(endpoint string, raw []byte) (string, error) {
	client := http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(endpoint, "application/json", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var answer struct {
		URL   string `json:"url"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK || answer.URL == "" {
		if answer.Error.Message != "" {
			return "", errors.New(answer.Error.Message)
		}
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	return answer.URL, nil
}

func tail(text string, most int) string {
	if len(text) > most {
		return text[len(text)-most:]
	}
	return text
}
