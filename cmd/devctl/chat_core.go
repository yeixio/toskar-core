package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/internal/config"
)

// launchdLabel and windowsTask are what the installers register Toskar as.
const (
	launchdLabel = "ai.toskar.toskar"
	windowsTask  = "Toskar"
)

// coreHost is what starting Toskar asks of this computer; a test stands in
// for it.
type coreHost struct {
	goos, home string
	uid        int
	exists     func(path string) bool
	// run runs a command quietly, and attached runs one on this terminal.
	run, attached func(name string, args ...string) error
	// find is the installed toskar, or "".
	find func() string
	// spawn starts it in the background, and says when it exits.
	spawn func(path string) (<-chan error, error)
	// installSize is the download's size in bytes, or 0 when unknown.
	installSize func() int64
}

func thisComputer() coreHost {
	home, _ := os.UserHomeDir()
	return coreHost{
		goos: runtime.GOOS, home: home, uid: os.Getuid(),
		exists: func(path string) bool {
			_, err := os.Stat(path)
			return err == nil
		},
		run: func(name string, args ...string) error { return exec.Command(name, args...).Run() },
		attached: func(name string, args ...string) error {
			cmd := exec.Command(name, args...)
			cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
			return cmd.Run()
		},
		find:        findCore,
		spawn:       spawnCore,
		installSize: func() int64 { return releaseSize(runtime.GOOS, runtime.GOARCH) },
	}
}

// findCore looks beside this program first, since the two install together.
// LookPath never answers with something in the folder chat was started in, so
// a project can't put its own toskar in the way.
func findCore() string {
	name := "toskar"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		beside := filepath.Join(filepath.Dir(exe), name)
		if info, err := os.Stat(beside); err == nil && !info.IsDir() {
			return beside
		}
	}
	found, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return found
}

// spawnCore runs Toskar from its own folder, as the installers do, so it finds
// the web folder beside it. It writes its own log.
func spawnCore(path string) (<-chan error, error) {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	cmd := exec.Command(path)
	cmd.Dir = filepath.Dir(path)
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	died := make(chan error, 1)
	go func() { died <- cmd.Wait() }()
	return died, nil
}

// coreService is a service the installers registered. Its Toskar may keep its
// data elsewhere, as the Linux packages' system user does, so running toskar
// directly would be a second, empty Toskar: the service is the one to start.
type coreService struct {
	// start starts it, or is nil when only an administrator can.
	start func() error
	// manual is the command for a person to run when start can't.
	manual, stop string
}

func (h coreHost) service() *coreService {
	switch h.goos {
	case "linux":
		if h.run("systemctl", "cat", "toskar.service") != nil {
			return nil
		}
		return &coreService{
			start:  func() error { return h.run("systemctl", "start", "--no-ask-password", "toskar") },
			manual: "sudo systemctl start toskar",
			stop:   "sudo systemctl stop toskar",
		}
	case "darwin":
		agent := filepath.Join(h.home, "Library", "LaunchAgents", launchdLabel+".plist")
		if h.exists(agent) {
			domain := fmt.Sprintf("gui/%d", h.uid)
			return &coreService{
				start: func() error {
					// A service someone stopped with bootout has to be loaded again.
					if h.run("launchctl", "kickstart", domain+"/"+launchdLabel) == nil {
						return nil
					}
					return h.run("launchctl", "bootstrap", domain, agent)
				},
				manual: fmt.Sprintf("launchctl bootstrap %s %q", domain, agent),
				stop:   fmt.Sprintf("launchctl bootout %s/%s", domain, launchdLabel),
			}
		}
		if h.exists(filepath.Join("/Library/LaunchDaemons", launchdLabel+".plist")) {
			return &coreService{
				manual: "sudo launchctl kickstart system/" + launchdLabel,
				stop:   "sudo launchctl kill TERM system/" + launchdLabel,
			}
		}
	case "windows":
		if h.run("schtasks", "/Query", "/TN", windowsTask) != nil {
			return nil
		}
		return &coreService{
			start:  func() error { return h.run("schtasks", "/Run", "/TN", windowsTask) },
			manual: "schtasks /Run /TN " + windowsTask,
			stop:   "schtasks /End /TN " + windowsTask,
		}
	}
	return nil
}

// stopCommand stops a Toskar that chat started directly.
func stopCommand(goos string) string {
	if goos == "windows" {
		return "taskkill /F /IM toskar.exe"
	}
	return "pkill -x toskar"
}

// installCommand is the one in the README.
func installCommand(goos string) string {
	if goos == "windows" {
		return "irm https://github.com/yeixio/toskar-core/releases/latest/download/install.ps1 | iex"
	}
	return "curl -fsSL https://github.com/yeixio/toskar-core/releases/latest/download/install.sh | sh"
}

// startCore starts Toskar on this computer when it isn't running, and says so
// in a sentence (#1017). It stays running after the chat, so the next one opens
// right away and a long task doesn't depend on this terminal. A person who set
// TOSKAR_URL chose a Toskar, and starting another wouldn't help them.
func startCore(d daemonClient, streams chatIO, h coreHost, wait time.Duration) error {
	if config.Env("URL") != "" {
		return nil
	}
	first := d.request(http.MethodGet, "/health", nil, nil)
	var down *notRunningError
	if !errors.As(first, &down) {
		return nil
	}

	var died <-chan error
	stop := stopCommand(h.goos)
	service := h.service()
	switch {
	case service != nil:
		stop = service.stop
		fmt.Fprintln(streams.errOut, "Starting Toskar on this computer...")
		if service.start == nil || service.start() != nil {
			return &exitError{code: 1, msg: "Toskar isn't running, and starting its service here needs an administrator. Run this, then try again: " + service.manual}
		}
	case h.find() == "":
		if err := installCore(streams, h); err != nil {
			return err
		}
		// The installer starts the service it registers.
		if service := h.service(); service != nil {
			stop = service.stop
		} else if path := h.find(); path != "" {
			var err error
			if died, err = h.spawn(path); err != nil {
				return fmt.Errorf("couldn't start Toskar: %w", err)
			}
		}
	default:
		fmt.Fprintln(streams.errOut, "Starting Toskar on this computer...")
		var err error
		if died, err = h.spawn(h.find()); err != nil {
			return fmt.Errorf("couldn't start Toskar: %w", err)
		}
	}

	log := filepath.Join(config.DefaultDataDir(), "logs", "daemon.log")
	deadline := time.Now().Add(wait)
	for {
		err := d.request(http.MethodGet, "/health", nil, nil)
		if !errors.As(err, &down) {
			fmt.Fprintf(streams.errOut, "Toskar is running in the background and stays running after this chat, so the next one opens right away. To stop it: %s\n", stop)
			return nil
		}
		if !time.Now().Before(deadline) {
			return &exitError{code: 1, msg: "Toskar didn't start. Its log may say why: " + log}
		}
		select {
		case <-died:
			// Another chat starting it at the same moment wins the port, and
			// this one exits: give that one a moment to answer.
			died = nil
			if soon := time.Now().Add(2 * time.Second); soon.Before(deadline) {
				deadline = soon
			}
		case <-streams.interrupts:
			return &exitError{code: 130, msg: "Stopped waiting. Toskar may still be starting: try again in a moment."}
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// installCore asks, then runs the installer on this terminal, where it can ask
// for an administrator's password. Piped in, nobody can answer, so it gives the
// command instead.
func installCore(streams chatIO, h coreHost) error {
	command := installCommand(h.goos)
	if !streams.inTerminal {
		return &exitError{code: 2, msg: "Toskar isn't installed on this computer. Install it with: " + command}
	}
	size := ""
	if n := h.installSize(); n > 0 {
		size = fmt.Sprintf(" (%s)", sizeText(uint64(n)))
	}
	fmt.Fprintf(streams.out, "Toskar isn't installed on this computer, and chat needs it. Download and install it now%s? [y]es, [n]o: ", size)
	answer, interrupted := readAnswerLine(streams)
	if interrupted {
		fmt.Fprintln(streams.out)
	}
	if answer = strings.ToLower(strings.TrimSpace(answer)); answer != "y" && answer != "yes" {
		return &exitError{code: 2, msg: "Nothing was installed. This installs it: " + command}
	}
	var err error
	if h.goos == "windows" {
		err = h.attached("powershell", "-NoProfile", "-Command", command)
	} else {
		err = h.attached("sh", "-c", command)
	}
	if err != nil {
		return &exitError{code: 1, msg: "The install didn't finish. This runs it again: " + command}
	}
	return nil
}

// readAnswerLine reads one line a byte at a time, so nothing typed after it is
// lost to the chat that reads next. Ctrl-C ends it.
func readAnswerLine(streams chatIO) (string, bool) {
	done := make(chan string, 1)
	go func() {
		var line []byte
		one := make([]byte, 1)
		for {
			n, err := streams.in.Read(one)
			if n > 0 && one[0] == '\n' {
				break
			}
			line = append(line, one[:n]...)
			if err != nil {
				break
			}
		}
		done <- string(line)
	}()
	select {
	case line := <-done:
		return line, false
	case <-streams.interrupts:
		return "", true
	}
}

// releaseSize is the size of what the installer downloads here, from the
// latest release, or 0 when that can't be told quickly.
func releaseSize(goos, goarch string) int64 {
	want := releaseAsset(goos, goarch)
	if want == nil {
		return 0
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/yeixio/toskar-core/releases/latest")
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	var release struct {
		Assets []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&release) != nil {
		return 0
	}
	for _, a := range release.Assets {
		if want.MatchString(a.Name) {
			return a.Size
		}
	}
	return 0
}

// releaseAsset matches the file install.sh or install.ps1 picks. Linux here
// means the deb; an rpm is much the same size.
func releaseAsset(goos, goarch string) *regexp.Regexp {
	switch goos {
	case "darwin":
		return regexp.MustCompile(`^toskar-[0-9].*-darwin-` + goarch + `-headless\.tar\.gz$`)
	case "windows":
		return regexp.MustCompile(`^toskar-[0-9].*-windows-amd64-headless\.tar\.gz$`)
	case "linux":
		return regexp.MustCompile(`^toskar_[0-9].*_` + goarch + `\.deb$`)
	}
	return nil
}
