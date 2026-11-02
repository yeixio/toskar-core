// Command qualityrun (toskar-quality in releases) measures a model with Toskar's
// quality sets on this computer, in a throwaway Toskar, and adds a scheduled
// run's scores to the public results (docs/quality.md, #1037).
//
//	toskar-quality run --model <id> --toskar <toskar binary>
//	toskar-quality publish <reports> <results.json> <version.json> <catalog.json> <run URL>
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
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: toskar-quality run --model <id> --toskar <toskar binary> [--harness <folder>] [--out <folder>] [--data <folder>] [--port <port>] [--sets quality,topics,files,reasoning]")
	fmt.Fprintln(os.Stderr, "       toskar-quality publish <reports> <results.json> <version.json> <catalog.json> <run URL>")
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
	model := fs.String("model", "", "the model to measure")
	toskar := fs.String("toskar", "", "the Toskar binary to run")
	harness := fs.String("harness", filepath.Dir(self), "the folder with the test binary and sets")
	out := fs.String("out", "", "where reports go (default: quality-score-<model> in this folder)")
	data := fs.String("data", filepath.Join(cacheDir, "toskar-quality"), "the throwaway Toskar's data folder, which keeps its models between runs")
	port := fs.Int("port", 7361, "the throwaway Toskar's port")
	only := fs.String("sets", "", "only these sets, comma-separated; a run without all of them gets no Score")
	_ = fs.Parse(args)
	if *model == "" || *toskar == "" {
		usage()
	}
	if *out == "" {
		*out = "quality-score-" + *model
	}
	for _, p := range []*string{toskar, harness, out, data} {
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
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(*data, 0o755); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	url := fmt.Sprintf("http://127.0.0.1:%d", *port)
	stopToskar, err := startToskar(ctx, *toskar, *harness, *out, *data, *port, url)
	if err != nil {
		return err
	}
	defer stopToskar()

	test := filepath.Join(*harness, "quality.test")
	if runtime.GOOS == "windows" {
		test += ".exe"
	}
	logFile, err := os.Create(filepath.Join(*out, "run.log"))
	if err != nil {
		return err
	}
	defer logFile.Close()

	row := qualityscore.Row{
		Model: *model, Name: *model, MethodVersion: version,
		Date:   time.Now().UTC().Format("2006-01-02"),
		Scores: map[string]qualityscore.Passed{},
	}
	fmt.Printf("Measuring %s. This can take a long time, and logs go to %s.\n", *model, logFile.Name())
	for _, set := range qualityscore.Sets {
		if *only != "" && !contains(strings.Split(*only, ","), set.Name) {
			continue
		}
		started := time.Now()
		fmt.Printf("%s: running...\n", set.Name)
		report := filepath.Join(*out, set.Report)
		_ = os.Remove(report)
		cmd := exec.CommandContext(ctx, test, "-test.run", "^"+setTests[set.Name][0]+"$", "-test.count=1", "-test.v", "-test.timeout=600m")
		cmd.Dir = *harness
		cmd.Stdout, cmd.Stderr = logFile, logFile
		cmd.Env = append(os.Environ(), "TOSKAR_QUALITY_URL="+url, "TOSKAR_QUALITY_MODEL="+*model, setTests[set.Name][1]+"="+report)
		// A set below its bar still has a result to report, so its failure doesn't stop this.
		_ = cmd.Run()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := readSet(&row, set, report, *model); err != nil {
			return err
		}
		if s, ok := row.Scores[set.Name]; ok {
			fmt.Printf("%s: %d of %d, in %s\n", set.Name, s.Passed, s.Of, time.Since(started).Round(time.Second))
		} else {
			fmt.Printf("%s: no score. See %s.\n", set.Name, report)
		}
	}
	if len(row.Scores) == 0 {
		return fmt.Errorf("no scores; see %s", logFile.Name())
	}
	if row.Hardware, err = hardwareName(url); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(row, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*out, "result.json"), append(raw, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("\n%s on %s\n", row.Model, row.Hardware)
	if score, ok := qualityscore.Score(row.Scores); ok {
		fmt.Printf("Score: %s\n", qualityscore.Describe(score, version))
	}
	if row.SecondsPerQuestion > 0 {
		fmt.Printf("Speed: %.1f s a question\n", row.SecondsPerQuestion)
	}
	fmt.Printf("\nSaved in %s. To share it: %s\n", *out, resultsForm)
	return nil
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
func startToskar(ctx context.Context, toskar, harness, out, data string, port int, url string) (func(), error) {
	if conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second); err == nil {
		conn.Close()
		return nil, fmt.Errorf("something already answers on port %d; choose another with --port", port)
	}
	work, err := os.MkdirTemp("", "toskar-quality-")
	if err != nil {
		return nil, err
	}
	logFile, err := os.Create(filepath.Join(out, "toskar.log"))
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
	stop()
	return nil, fmt.Errorf("Toskar didn't start; see %s", logFile.Name())
}

// hardwareName is the graphics card or chip, and memory. Nothing that identifies the computer.
func hardwareName(url string) (string, error) {
	resp, err := http.Get(url + "/api/v1/hardware")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var hw struct {
		CPU    struct{ Model string } `json:"cpu"`
		Memory struct {
			TotalBytes int64 `json:"total_bytes"`
		} `json:"memory"`
		Accelerators []struct{ Name, Model string } `json:"accelerators"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&hw); err != nil {
		return "", err
	}
	var cards []string
	for _, a := range hw.Accelerators {
		cards = append(cards, a.Name, a.Model)
	}
	return hardwareLabel(hw.CPU.Model, hw.Memory.TotalBytes, cards), nil
}

func hardwareLabel(cpu string, memoryBytes int64, accelerators []string) string {
	chip := cpu
	for _, a := range accelerators {
		if a != "" {
			chip = a
			break
		}
	}
	return fmt.Sprintf("%s, %d GB", chip, (memoryBytes+1<<29)>>30)
}

func publish(args []string) error {
	if len(args) != 5 {
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
	return qualityscore.Publish(args[0], args[1], version, names, args[4], time.Now(), func(line string) { fmt.Println(line) })
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
