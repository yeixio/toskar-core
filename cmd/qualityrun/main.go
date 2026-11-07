// Command qualityrun (toskar-quality in releases) measures a model with Toskar's
// quality sets on this computer, in a throwaway Toskar, and adds a scheduled
// run's scores to the public results (docs/quality.md, #1037).
//
//	toskar-quality run --toskar <toskar binary> [--model <id>]
//	toskar-quality publish <reports> <results.json> <version.json> <catalog.json> <models.json> <run URL>
//	toskar-quality next <models.json> <results.json> <version.json> <count>
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/internal/qualityscore"
)

const resultsForm = "https://github.com/yeixio/toskar-core/issues/new?template=quality_results.yml"

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "run":
		err = run(os.Args[2:])
	case "publish":
		err = publish(os.Args[2:])
	case "next":
		err = next(os.Args[2:])
	case "intake":
		err = intake(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: toskar-quality run --toskar <toskar binary> [--model <id>] [--yes] [--harness <folder>] [--out <folder>] [--data <folder>] [--port <port>] [--sets quality,topics,files,reasoning] [--only <case>] [--speed-only] [--all] [--most <n>]")
	fmt.Fprintln(os.Stderr, "       toskar-quality publish <reports> <results.json> <version.json> <catalog.json> <models.json> <run URL>")
	fmt.Fprintln(os.Stderr, "       toskar-quality next <models.json> <results.json> <version.json> <count>")
	fmt.Fprintln(os.Stderr, "       toskar-quality intake <issue body> <author> <issue URL> <models.json> <results.json> <version.json> [--publish]")
	os.Exit(2)
}

// setTests are the test and report variable of each set.
var setTests = map[string][2]string{
	"quality":   {"TestQualitySet", "TOSKAR_QUALITY_REPORT"},
	"topics":    {"TestTopicQuality", "TOSKAR_QUALITY_TOPIC_REPORT"},
	"files":     {"TestFilesQuality", "TOSKAR_QUALITY_FILES_REPORT"},
	"reasoning": {"TestReasoningQuality", "TOSKAR_QUALITY_REASONING_REPORT"},
}

func run(args []string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	model := fs.String("model", "", "the model to measure: its ID, or a link to its .gguf file on Hugging Face")
	toskar := fs.String("toskar", "", "the Toskar binary to run")
	harness := fs.String("harness", filepath.Dir(self), "the folder with the test binary and sets")
	out := fs.String("out", "", "where reports go (default: quality-score-<model> in this folder)")
	data := fs.String("data", filepath.Join(cacheDir, "toskar-quality"), "the throwaway Toskar's data folder, which keeps its models between runs")
	port := fs.Int("port", 7361, "the throwaway Toskar's port")
	only := fs.String("sets", "", "only these sets, comma-separated; a run without all of them gets no Score")
	onlyCase := fs.String("only", "", "only the cases matching this, for checking this program")
	yes := fs.Bool("yes", false, "ask nothing: take the most popular model that fits, and don't offer to share")
	speedOnly := fs.Bool("speed-only", false, "only how fast it runs, which takes minutes, with no quality sets")
	all := fs.Bool("all", false, "every listed model that fits this computer, removing each one it downloaded")
	most := fs.Int("most", 0, "with --all, at most this many models, smallest first")
	_ = fs.Parse(args)
	if *toskar == "" {
		usage()
	}
	for _, p := range []*string{toskar, harness, data} {
		abs, err := filepath.Abs(*p)
		if err != nil {
			return err
		}
		*p = abs
	}
	version, err := readVersion(filepath.Join(*harness, "version.json"))
	if err != nil {
		return err
	}
	// The folder for reports is made once the model is known; Toskar's log waits in here.
	logs, err := os.MkdirTemp("", "toskar-quality-logs-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(logs)
	if err := os.MkdirAll(*data, 0o755); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	url := fmt.Sprintf("http://127.0.0.1:%d", *port)
	stopToskar, err := startToskar(ctx, *toskar, *harness, logs, *data, *port, url)
	if err != nil {
		return err
	}
	defer stopToskar()

	ask := newAsker(*yes)
	defer ask.close()
	defer keepAwake()()
	w := &work{
		ctx: ctx, url: url, test: filepath.Join(*harness, "quality.test"), harness: *harness, logs: logs, version: version,
		only: *only, onlyCase: *onlyCase, out: *out, speedOnly: *speedOnly,
	}
	if runtime.GOOS == "windows" {
		w.test += ".exe"
	}
	chosen := []chosenModel{{ID: *model, Name: displayName(url, *model)}}
	switch {
	case *all:
		listed, room, err := fitting(url)
		if err != nil {
			return err
		}
		chosen = everyModel(listed, room, *most)
		w.many = true
	case qualityscore.ModelLink(*model):
		one, err := chooseLink(url, *model, ask, ^uint64(0))
		if err != nil {
			return err
		}
		chosen = []chosenModel{one}
	case *model == "":
		plan, err := chooseModel(url, ask)
		if err != nil {
			return err
		}
		chosen, w.many = plan.Models, plan.Many
		w.speedOnly = w.speedOnly || plan.SpeedOnly
	}
	if w.many {
		return w.measureMany(chosen, ask, *data)
	}
	row, folder, err := w.measure(chosen[0])
	if err != nil {
		return err
	}
	w.report(row)
	fmt.Printf("\nSaved in %s.\n", folder)
	fmt.Printf("Models stay in %s for your next run. Delete that folder to get the space back.\n", *data)
	if w.shareable(row) {
		offerToShare(row, ask, folder)
	} else {
		fmt.Println("This run didn't finish everything, so it has nothing to share.")
	}
	return nil
}

// work is what every measured model in a run has in common.
type work struct {
	ctx                context.Context
	url, test, harness string
	logs, out          string
	only, onlyCase     string
	version            int
	speedOnly, many    bool
}

// measure runs one model, and writes result.json beside its reports when the run
// is whole. How it runs comes first: there's no use in hours of cases for a model
// that won't load.
func (w *work) measure(m chosenModel) (qualityscore.Row, string, error) {
	row := qualityscore.Row{
		Model: m.ID, Name: m.Name, MethodVersion: w.version, OffList: m.Source != "",
		Date:   time.Now().UTC().Format("2006-01-02"),
		Scores: map[string]qualityscore.Passed{},
	}
	folder := w.out
	if folder == "" || w.many {
		folder = filepath.Join(w.out, "quality-score-"+m.ID)
	}
	folder, err := filepath.Abs(folder)
	if err != nil {
		return row, folder, err
	}
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return row, folder, err
	}
	// Toskar's log goes with the reports, for whoever has to read why a run failed.
	defer func() {
		if raw, err := os.ReadFile(filepath.Join(w.logs, "toskar.log")); err == nil {
			_ = os.WriteFile(filepath.Join(folder, "toskar.log"), raw, 0o644)
		}
	}()
	logFile, err := os.Create(filepath.Join(folder, "run.log"))
	if err != nil {
		return row, folder, err
	}
	defer logFile.Close()
	// A file from outside the list: the tests install it from its link.
	env := append(os.Environ(), "TOSKAR_QUALITY_URL="+w.url, "TOSKAR_QUALITY_MODEL="+m.ID)
	if m.Source != "" {
		env = append(env, "TOSKAR_QUALITY_MODEL_SOURCE="+m.Source)
	}
	runTest := func(pattern, timeout, reportVar, report string) {
		_ = os.Remove(report)
		cmd := exec.CommandContext(w.ctx, w.test, "-test.run", pattern, "-test.count=1", "-test.v", "-test.timeout="+timeout)
		cmd.Dir = w.harness
		cmd.Stdout, cmd.Stderr = logFile, logFile
		cmd.Env = append(env[:len(env):len(env)], reportVar+"="+report)
		// A set below its bar still has a result to report, so its failure doesn't stop this.
		_ = cmd.Run()
	}

	fmt.Printf("Measuring %s. Logs go to %s.\n", m.Name, logFile.Name())
	fmt.Println("speed: running...")
	runTest("^TestPerformance$", "70m", "TOSKAR_QUALITY_PERFORMANCE_REPORT", filepath.Join(folder, qualityscore.PerformanceFile))
	if w.ctx.Err() != nil {
		return row, folder, w.ctx.Err()
	}
	performance, perfErr := qualityscore.ReadPerformance(filepath.Join(folder, qualityscore.PerformanceFile), m.ID)
	if perfErr != nil {
		fmt.Printf("speed: not measured. See %s.\n", logFile.Name())
		if w.speedOnly || w.many {
			raw, _ := os.ReadFile(logFile.Name())
			return row, folder, fmt.Errorf("couldn't measure %s. The end of its log:\n%s", m.Name, tail(string(raw), 1500))
		}
	} else {
		row.Runs, row.Why, row.OS = performance.Runs, performance.Why, performance.OS
	}
	if row.Hardware, err = hardwareName(w.url); err != nil {
		return row, folder, err
	}
	if performance != nil && performance.Runs == qualityscore.RunsNo {
		return row, folder, writeResult(row, folder)
	}
	row.Performance = performance
	if !w.speedOnly {
		for _, set := range qualityscore.Sets {
			if w.only != "" && !contains(strings.Split(w.only, ","), set.Name) {
				continue
			}
			started := time.Now()
			fmt.Printf("%s: running...\n", set.Name)
			report := filepath.Join(folder, set.Report)
			pattern := "^" + setTests[set.Name][0] + "$"
			if w.onlyCase != "" {
				pattern += "/" + w.onlyCase
			}
			runTest(pattern, "600m", setTests[set.Name][1], report)
			if w.ctx.Err() != nil {
				return row, folder, w.ctx.Err()
			}
			if err := readSet(&row, set, report, m.ID); err != nil {
				return row, folder, err
			}
			if s, ok := row.Scores[set.Name]; ok {
				fmt.Printf("%s: %d of %d, in %s\n", set.Name, s.Passed, s.Of, time.Since(started).Round(time.Second))
			} else {
				fmt.Printf("%s: no score. See %s.\n", set.Name, report)
			}
		}
		if len(row.Scores) == 0 {
			raw, _ := os.ReadFile(logFile.Name())
			return row, folder, fmt.Errorf("no scores. The end of %s:\n%s", logFile.Name(), tail(string(raw), 3000))
		}
	}
	if w.shareable(row) {
		return row, folder, writeResult(row, folder)
	}
	return row, folder, nil
}

// shareable is whether a row is one the results take: all the sets with its
// figures, a speed test, or a model that doesn't run.
func (w *work) shareable(row qualityscore.Row) bool {
	if row.Runs == qualityscore.RunsNo {
		return true
	}
	if row.Performance == nil {
		return false
	}
	if w.speedOnly {
		return len(row.Scores) == 0
	}
	_, complete := qualityscore.Score(row.Scores)
	return complete
}

func writeResult(row qualityscore.Row, folder string) error {
	raw, err := json.MarshalIndent(row, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(folder, "result.json"), append(raw, '\n'), 0o644)
}

// report shows a person a row's numbers.
func (w *work) report(row qualityscore.Row) {
	fmt.Printf("\n%s on %s\n", row.Name, row.Hardware)
	if row.Runs == qualityscore.RunsNo {
		fmt.Printf("It doesn't run here: %s.\n", row.Why)
		return
	}
	if p := row.Performance; p != nil {
		if score, ok := qualityscore.ToskarScore(row.Model, p); ok {
			fmt.Printf("Toskar Score: %d\n", score)
		}
		fmt.Printf("Writes %.1f tokens a second. Reads %.0f tokens a second.\n", p.TokensPerSecond, p.PromptTokensPerSecond)
		fmt.Printf("With a prompt of %d tokens, it starts in %.1f s and averages %.1f tokens a second from start to finish.\n", p.PromptTokens, p.FirstTokenMs/1000, p.EffectiveTokensPerSecond)
		fmt.Printf("%s: %d of %d layers on the GPU.\n", map[string]string{qualityscore.RunsFull: "Runs fully", qualityscore.RunsPartial: "Runs partly, with the rest in system memory", qualityscore.RunsCPU: "Runs on the processor"}[row.Runs], p.GPULayers, p.GPULayersTotal)
	}
	if score, ok := qualityscore.Score(row.Scores); ok {
		fmt.Printf("Quality score: %s\n", qualityscore.Describe(score, row.MethodVersion))
	}
}

// measureMany runs each model in turn. One failing doesn't stop the rest, what's
// already measured is skipped, so a run that stopped picks up where it left off,
// and each model it downloaded is removed to make room for the next.
func (w *work) measureMany(models []chosenModel, ask *asker, data string) error {
	var rows []qualityscore.Row
	var failed []string
	for i, m := range models {
		fmt.Printf("\n[%d of %d] %s\n", i+1, len(models), m.Name)
		folder := filepath.Join(w.out, "quality-score-"+m.ID)
		if row, ok := finished(folder, w.version, w.speedOnly); ok {
			fmt.Println("Already measured, so this is skipped.")
			rows = append(rows, row)
			continue
		}
		if m.SizeBytes > 0 {
			if free, err := freeBytes(w.url); err == nil && free < m.SizeBytes+m.SizeBytes/10+1<<30 {
				fmt.Printf("Skipped: it needs %s, and %s is free.\n", gigabytes(m.SizeBytes), gigabytes(free))
				failed = append(failed, m.Name+": not enough disk space")
				continue
			}
		}
		had := installed(w.url, m.ID)
		row, _, err := w.measure(m)
		if !had {
			removeModel(w.url, m.ID)
		}
		if w.ctx.Err() != nil {
			fmt.Printf("\nStopped. Run the same line again to pick up from %s.\n", m.Name)
			return w.ctx.Err()
		}
		if err != nil {
			fmt.Println(err)
			failed = append(failed, m.Name)
			continue
		}
		w.report(row)
		if w.shareable(row) {
			rows = append(rows, row)
		}
	}
	fmt.Printf("\nDone: %d measured", len(rows))
	if len(failed) > 0 {
		fmt.Printf(", %d didn't finish: %s", len(failed), strings.Join(failed, "; "))
	}
	fmt.Printf(".\nReports are in %s. Delete %s to get the space back.\n", absOr(w.out), data)
	offerToShareAll(rows, ask)
	return nil
}

func absOr(path string) string {
	if path == "" {
		path = "."
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

// finished is a result this mode already has, at this method version.
func finished(folder string, version int, speedOnly bool) (qualityscore.Row, bool) {
	var row qualityscore.Row
	raw, err := os.ReadFile(filepath.Join(folder, "result.json"))
	if err != nil || json.Unmarshal(raw, &row) != nil || row.MethodVersion != version {
		return row, false
	}
	if row.Runs == qualityscore.RunsNo {
		return row, true
	}
	if speedOnly {
		return row, row.Performance != nil
	}
	_, complete := qualityscore.Score(row.Scores)
	return row, complete
}

// displayName is a model's name in Toskar's catalog or on the list, or else its ID.
func displayName(url, id string) string {
	var models []struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	}
	if readJSON(url+"/api/v1/models", &models) == nil {
		for _, m := range models {
			// A model installed from Hugging Face can have its id for a
			// name here; the list's name is the one to show then (#1087).
			if m.ID == id && m.DisplayName != "" && m.DisplayName != id {
				return m.DisplayName
			}
		}
	}
	var listed struct {
		Models []qualityscore.ListedModel `json:"models"`
	}
	if readJSON(modelListURL, &listed) == nil {
		for _, m := range listed.Models {
			if m.ID == id {
				return m.Name
			}
		}
	}
	return id
}

// installed is whether Toskar already has a model, so it's not removed after.
func installed(url, id string) bool {
	var models []struct {
		ID        string `json:"id"`
		Installed bool   `json:"installed"`
	}
	if readJSON(url+"/api/v1/models", &models) != nil {
		return true
	}
	for _, m := range models {
		if m.ID == id {
			return m.Installed
		}
	}
	return false
}

func removeModel(url, id string) {
	req, err := http.NewRequest(http.MethodDelete, url+"/api/v1/models/"+id, nil)
	if err != nil {
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

func freeBytes(url string) (uint64, error) {
	hw, err := readHardware(url)
	return hw.Disk.AvailableBytes, err
}

// readSet adds a set's report to the row. A report naming another model is a
// run that didn't measure what it says, so it stops here.
func readSet(row *qualityscore.Row, set qualityscore.Set, report, model string) error {
	raw, err := os.ReadFile(report)
	if err != nil {
		return nil
	}
	tested, ok := qualityscore.TestedOn(string(raw))
	if ok && tested.Model != model {
		return fmt.Errorf("%s tested %s and not %s, so it has no result", report, tested.Model, model)
	}
	if score, ok := qualityscore.ScoreOf(set.Name, string(raw)); ok {
		row.Scores[set.Name] = score
	}
	if set.Name == "reasoning" {
		row.SecondsPerQuestion = qualityscore.SecondsPerQuestion(string(raw))
	}
	return nil
}

// startToskar runs its own Toskar, with its own data folder and port, in an empty
// folder, so a person's everyday Toskar and its chats aren't touched. Web lookups
// answer from the set's fixed pages, and every tool that asks first is answered no.
func startToskar(ctx context.Context, toskar, harness, logs, data string, port int, url string) (func(), error) {
	if conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second); err == nil {
		conn.Close()
		return nil, fmt.Errorf("something already answers on port %d; choose another with --port", port)
	}
	work, err := os.MkdirTemp("", "toskar-quality-")
	if err != nil {
		return nil, err
	}
	logFile, err := os.Create(filepath.Join(logs, "toskar.log"))
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(toskar, "-data-dir", data)
	cmd.Dir = work
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.Env = append(os.Environ(),
		"TOSKAR_API_PORT="+strconv.Itoa(port), "TOSKAR_INTERNAL_PORT="+strconv.Itoa(port+1),
		"TOSKAR_DISCOVERY_ENABLED=false", "TOSKAR_WEB_FIXTURES="+filepath.Join(harness, "web.json"))
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	stop := func() {
		if runtime.GOOS == "windows" {
			_ = cmd.Process.Kill()
		} else {
			_ = cmd.Process.Signal(os.Interrupt)
		}
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
		logFile.Close()
		_ = os.RemoveAll(work)
	}
	for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline) && ctx.Err() == nil; time.Sleep(500 * time.Millisecond) {
		resp, err := http.Get(url + "/api/v1/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return stop, nil
			}
		}
	}
	raw, _ := os.ReadFile(logFile.Name())
	stop()
	return nil, fmt.Errorf("the throwaway Toskar didn't start:\n%s", tail(string(raw), 2000))
}

// hardware is what the daemon says about this computer, keeping out what could
// identify it.
type hardware struct {
	CPU struct {
		Model string `json:"model"`
	} `json:"cpu"`
	Memory struct {
		TotalBytes uint64 `json:"total_bytes"`
	} `json:"memory"`
	Disk struct {
		AvailableBytes uint64 `json:"available_bytes"`
	} `json:"disk"`
	Accelerators []struct {
		Name          string `json:"name"`
		Model         string `json:"model"`
		DedicatedVRAM uint64 `json:"dedicated_vram_bytes"`
	} `json:"accelerators"`
}

func readHardware(url string) (hardware, error) {
	var hw hardware
	err := readJSON(url+"/api/v1/hardware", &hw)
	return hw, err
}

// virtualAdapter is a display adapter that isn't a graphics card, as a virtual
// machine has.
var virtualAdapter = regexp.MustCompile(`(?i)hyper-v|basic display|basic render|vmware|virtualbox|llvmpipe|virtio`)

// label is the graphics card with the most memory, or the chip, and the memory a
// model can use there. Nothing that identifies the computer.
func (hw hardware) label() string {
	chip, memory, card := hw.CPU.Model, hw.Memory.TotalBytes, uint64(0)
	found := false
	for _, a := range hw.Accelerators {
		name := a.Name
		if name == "" {
			name = a.Model
		}
		if name == "" || virtualAdapter.MatchString(name) {
			continue
		}
		if !found || a.DedicatedVRAM > card {
			chip, card, found = name, a.DedicatedVRAM, true
		}
	}
	if card > 0 {
		memory = card
	}
	return qualityscore.Hardware(chip, (memory+1<<29)>>30)
}

func hardwareName(url string) (string, error) {
	hw, err := readHardware(url)
	if err != nil {
		return "", err
	}
	return hw.label(), nil
}

// next prints what a scheduled run measures, as JSON for a workflow's matrix.
func next(args []string) error {
	if len(args) != 4 {
		usage()
	}
	list, err := qualityscore.ReadModelList(args[0])
	if err != nil {
		return err
	}
	results, err := qualityscore.ReadResults(args[1])
	if err != nil {
		return err
	}
	version, err := readVersion(args[2])
	if err != nil {
		return err
	}
	count, err := strconv.Atoi(args[3])
	if err != nil {
		return fmt.Errorf("count: %w", err)
	}
	raw, err := json.Marshal(qualityscore.NextModels(list, results, version, count))
	if err != nil {
		return err
	}
	fmt.Println(string(raw))
	return nil
}

// intake checks a result sent as an issue, says what it found as JSON for the
// workflow, and with --publish adds rows that pass to the results file. Nothing in
// the issue is trusted: it's only read as data.
func intake(args []string) error {
	if len(args) < 6 {
		usage()
	}
	raw, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	list, err := qualityscore.ReadModelList(args[3])
	if err != nil {
		return err
	}
	published, err := qualityscore.ReadResults(args[4])
	if err != nil {
		return err
	}
	version, err := readVersion(args[5])
	if err != nil {
		return err
	}
	verdict := qualityscore.Intake(string(raw), args[1], args[2], list, published, version, time.Now())
	if len(args) > 6 && args[6] == "--publish" && verdict.Passed {
		if err := qualityscore.AddRows(args[4], verdict.Rows); err != nil {
			return err
		}
	}
	return json.NewEncoder(os.Stdout).Encode(verdict)
}

func publish(args []string) error {
	if len(args) != 6 {
		usage()
	}
	version, err := readVersion(args[2])
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(args[3])
	if err != nil {
		return err
	}
	var catalog struct {
		Models []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return err
	}
	names := map[string]string{}
	for _, m := range catalog.Models {
		names[m.ID] = m.DisplayName
	}
	// A model on the list that isn't in the catalog has its name there.
	list, err := qualityscore.ReadModelList(args[4])
	if err != nil {
		return err
	}
	for _, m := range list {
		names[m.ID] = m.Name
	}
	return qualityscore.Publish(args[0], args[1], version, names, args[5], time.Now(), func(line string) { fmt.Println(line) })
}

func readVersion(path string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var v struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}
	return v.Version, nil
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if strings.TrimSpace(s) == want {
			return true
		}
	}
	return false
}
