package main

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// coreDaemon is a daemon that answers once start has been called.
type coreDaemon struct {
	mu      sync.Mutex
	running bool
}

func (c *coreDaemon) start() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.running = true
}

func (c *coreDaemon) RoundTrip(*http.Request) (*http.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running {
		return nil, errors.New("connection refused")
	}
	return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
}

func (c *coreDaemon) client() daemonClient {
	return daemonClient{base: "http://127.0.0.1:1", client: &http.Client{Transport: c}}
}

// fakeHost records what was run and started. Commands in ok succeed.
type fakeHost struct {
	coreHost
	ran, spawned []string
	files        map[string]bool
	ok           map[string]func()
}

func newFakeHost(goos, core string) *fakeHost {
	f := &fakeHost{files: map[string]bool{}, ok: map[string]func(){}}
	run := func(name string, args ...string) error {
		line := strings.TrimSpace(name + " " + strings.Join(args, " "))
		f.ran = append(f.ran, line)
		do, found := f.ok[line]
		if !found {
			return errors.New("failed")
		}
		if do != nil {
			do()
		}
		return nil
	}
	f.coreHost = coreHost{
		goos: goos, home: "/home/someone", uid: 501,
		exists:      func(path string) bool { return f.files[path] },
		run:         run,
		attached:    run,
		find:        func() string { return core },
		installSize: func() int64 { return 92_000_000 },
	}
	return f
}

func runStartCore(t *testing.T, c *coreDaemon, h coreHost, terminal bool, typed string) (string, error) {
	t.Helper()
	t.Setenv("TOSKAR_URL", "")
	t.Setenv("YGGDRASIL_URL", "")
	var out bytes.Buffer
	streams := chatIO{in: strings.NewReader(typed), inTerminal: terminal, out: &out, errOut: &out}
	err := startCore(c.client(), streams, h, 2*time.Second)
	return out.String(), err
}

func TestStartCoreDirectly(t *testing.T) {
	c := &coreDaemon{}
	f := newFakeHost("darwin", "/opt/toskar/toskar")
	f.spawn = func(path string) (<-chan error, error) {
		f.spawned = append(f.spawned, path)
		c.start()
		return make(chan error), nil
	}
	out, err := runStartCore(t, c, f.coreHost, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.spawned) != 1 || f.spawned[0] != "/opt/toskar/toskar" {
		t.Fatalf("spawned %v", f.spawned)
	}
	if !strings.Contains(out, "stays running after this chat") || !strings.Contains(out, "pkill -x toskar") {
		t.Fatalf("doesn't say it stays or how to stop it: %q", out)
	}
}

func TestStartCoreLeavesARunningOneAlone(t *testing.T) {
	c := &coreDaemon{running: true}
	f := newFakeHost("linux", "/usr/bin/toskar")
	f.spawn = func(string) (<-chan error, error) { t.Fatal("started a second Toskar"); return nil, nil }
	out, err := runStartCore(t, c, f.coreHost, true, "")
	if err != nil || out != "" || len(f.ran) != 0 {
		t.Fatalf("err %v, out %q, ran %v", err, out, f.ran)
	}
}

// Someone who chose a Toskar elsewhere keeps the plain error from chat.
func TestStartCoreLeavesAChosenURLAlone(t *testing.T) {
	c := &coreDaemon{}
	f := newFakeHost("linux", "/usr/bin/toskar")
	f.spawn = func(string) (<-chan error, error) { t.Fatal("started Toskar for another computer"); return nil, nil }
	t.Setenv("TOSKAR_URL", "http://192.168.1.10:7331")
	var out bytes.Buffer
	if err := startCore(c.client(), chatIO{in: strings.NewReader(""), out: &out, errOut: &out}, f.coreHost, time.Second); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatalf("said %q", out.String())
	}
}

// The service's data is elsewhere, so running toskar directly would be a
// second, empty Toskar.
func TestStartCoreUsesTheService(t *testing.T) {
	agent := "/home/someone/Library/LaunchAgents/ai.toskar.toskar.plist"
	for _, tc := range []struct {
		name, goos, start, stop string
		found                   string
		file                    string
	}{
		{name: "linux", goos: "linux", found: "systemctl cat toskar.service", start: "systemctl start --no-ask-password toskar", stop: "sudo systemctl stop toskar"},
		{name: "mac", goos: "darwin", file: agent, start: "launchctl kickstart gui/501/ai.toskar.toskar", stop: "launchctl bootout gui/501/ai.toskar.toskar"},
		{name: "mac stopped with bootout", goos: "darwin", file: agent, start: "launchctl bootstrap gui/501 " + agent, stop: "launchctl bootout gui/501/ai.toskar.toskar"},
		{name: "windows", goos: "windows", found: "schtasks /Query /TN Toskar", start: "schtasks /Run /TN Toskar", stop: "schtasks /End /TN Toskar"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &coreDaemon{}
			f := newFakeHost(tc.goos, "/usr/bin/toskar")
			f.spawn = func(string) (<-chan error, error) { t.Fatal("ran toskar directly"); return nil, nil }
			if tc.found != "" {
				f.ok[tc.found] = nil
			}
			if tc.file != "" {
				f.files[tc.file] = true
			}
			f.ok[tc.start] = c.start
			out, err := runStartCore(t, c, f.coreHost, true, "")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out, tc.stop) {
				t.Fatalf("doesn't say to stop with %q: %q", tc.stop, out)
			}
		})
	}
}

func TestStartCoreServiceNeedsAnAdministrator(t *testing.T) {
	c := &coreDaemon{}
	f := newFakeHost("linux", "/usr/bin/toskar")
	f.spawn = func(string) (<-chan error, error) { t.Fatal("ran toskar directly"); return nil, nil }
	f.ok["systemctl cat toskar.service"] = nil
	_, err := runStartCore(t, c, f.coreHost, true, "")
	if err == nil || !strings.Contains(err.Error(), "sudo systemctl start toskar") {
		t.Fatalf("got %v", err)
	}
}

func TestStartCoreThatExitsSaysWhere(t *testing.T) {
	c := &coreDaemon{}
	f := newFakeHost("darwin", "/opt/toskar/toskar")
	f.spawn = func(string) (<-chan error, error) {
		died := make(chan error, 1)
		died <- errors.New("exit status 1")
		return died, nil
	}
	start := time.Now()
	t.Setenv("TOSKAR_URL", "")
	t.Setenv("YGGDRASIL_URL", "")
	var out bytes.Buffer
	err := startCore(c.client(), chatIO{in: strings.NewReader(""), out: &out, errOut: &out}, f.coreHost, time.Minute)
	if err == nil || !strings.Contains(err.Error(), "daemon.log") {
		t.Fatalf("got %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatalf("waited the whole minute for a Toskar that had exited")
	}
}

func TestStartCoreNotInstalled(t *testing.T) {
	command := installCommand("darwin")
	noSpawn := func(*testing.T) func(string) (<-chan error, error) {
		return func(string) (<-chan error, error) { t.Fatal("started Toskar"); return nil, nil }
	}
	t.Run("piped gives the command", func(t *testing.T) {
		f := newFakeHost("darwin", "")
		f.spawn = noSpawn(t)
		_, err := runStartCore(t, &coreDaemon{}, f.coreHost, false, "")
		if err == nil || !strings.Contains(err.Error(), command) || len(f.ran) != 0 {
			t.Fatalf("got %v, ran %v", err, f.ran)
		}
	})
	t.Run("no installs nothing", func(t *testing.T) {
		f := newFakeHost("darwin", "")
		f.spawn = noSpawn(t)
		out, err := runStartCore(t, &coreDaemon{}, f.coreHost, true, "n\n")
		if err == nil || !strings.Contains(err.Error(), "Nothing was installed") || len(f.ran) != 0 {
			t.Fatalf("got %v, ran %v", err, f.ran)
		}
		if !strings.Contains(out, "92 MB") {
			t.Fatalf("doesn't show the size: %q", out)
		}
	})
	t.Run("yes runs the installer, whose service starts it", func(t *testing.T) {
		c := &coreDaemon{}
		f := newFakeHost("darwin", "")
		f.spawn = noSpawn(t)
		agent := "/home/someone/Library/LaunchAgents/ai.toskar.toskar.plist"
		f.ok["sh -c "+command] = func() {
			f.files[agent] = true
			c.start()
		}
		out, err := runStartCore(t, c, f.coreHost, true, "y\n")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "launchctl bootout") {
			t.Fatalf("doesn't say how to stop it: %q", out)
		}
	})
	// Where nothing can start a service, as in a container or WSL without
	// systemd, the installer puts Toskar in place and then fails waiting for
	// it: chat starts it itself.
	t.Run("an install that only fails to start it is started here", func(t *testing.T) {
		c := &coreDaemon{}
		f := newFakeHost("linux", "")
		installed := ""
		f.find = func() string { return installed }
		f.attached = func(name string, args ...string) error {
			installed = "/usr/bin/toskar"
			return errors.New("exit status 1")
		}
		f.spawn = func(path string) (<-chan error, error) {
			f.spawned = append(f.spawned, path)
			c.start()
			return make(chan error), nil
		}
		out, err := runStartCore(t, c, f.coreHost, true, "y\n")
		if err != nil {
			t.Fatal(err)
		}
		if len(f.spawned) != 1 || f.spawned[0] != "/usr/bin/toskar" {
			t.Fatalf("spawned %v", f.spawned)
		}
		if !strings.Contains(out, "installer couldn't start it") || !strings.Contains(out, "pkill -x toskar") {
			t.Fatalf("doesn't say what happened: %q", out)
		}
	})
	t.Run("an install that fails says so", func(t *testing.T) {
		f := newFakeHost("darwin", "")
		f.spawn = noSpawn(t)
		_, err := runStartCore(t, &coreDaemon{}, f.coreHost, true, "yes\n")
		if err == nil || !strings.Contains(err.Error(), "didn't finish") {
			t.Fatalf("got %v", err)
		}
	})
}

// A Toskar that chat starts takes its port from the environment, so chat has
// to look for it there.
func TestDaemonBaseFollowsThePort(t *testing.T) {
	t.Setenv("TOSKAR_URL", "")
	t.Setenv("YGGDRASIL_URL", "")
	t.Setenv("TOSKAR_API_PORT", "7361")
	t.Setenv("TOSKAR_API_HOST", "0.0.0.0")
	if got := daemonBase(); got != "http://127.0.0.1:7361" {
		t.Fatalf("got %s", got)
	}
	t.Setenv("TOSKAR_URL", "http://192.168.1.10:7331/")
	if got := daemonBase(); got != "http://192.168.1.10:7331" {
		t.Fatalf("got %s", got)
	}
}

// What follows the answer stays for the chat.
func TestReadAnswerLineLeavesTheRest(t *testing.T) {
	in := strings.NewReader("y\nhello\n")
	line, interrupted := readAnswerLine(chatIO{in: in})
	if line != "y" || interrupted {
		t.Fatalf("got %q, %v", line, interrupted)
	}
	if in.Len() != len("hello\n") {
		t.Fatalf("%d bytes left", in.Len())
	}
}

func TestReleaseAsset(t *testing.T) {
	for _, tc := range []struct{ goos, goarch, name string }{
		{"darwin", "arm64", "toskar-1.12.1-darwin-arm64-headless.tar.gz"},
		{"windows", "amd64", "toskar-1.12.1-windows-amd64-headless.tar.gz"},
		{"linux", "amd64", "toskar_1.12.1_amd64.deb"},
	} {
		if !releaseAsset(tc.goos, tc.goarch).MatchString(tc.name) {
			t.Errorf("%s/%s doesn't match %s", tc.goos, tc.goarch, tc.name)
		}
	}
	if releaseAsset("darwin", "arm64").MatchString("toskar-1.12.1-darwin-amd64-headless.tar.gz") {
		t.Error("matched another architecture")
	}
}
