// Package codeexec runs Python the assistant writes, for calculations and
// data analysis, inside an operating-system sandbox (Gungnir §20). There is
// no fallback: where no sandbox is available, the tool is unavailable and
// says why.
package codeexec

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Sandbox wraps a command so it can read only what it needs, write only its
// working folder, and reach no network.
type Sandbox interface {
	// Name is what the sandbox is called, for diagnostics.
	Name() string
	// Available reports whether it can run here, and why not.
	Available() (bool, string)
	// Command returns the command that runs argv inside the sandbox, with
	// work as its only writable folder and readOnly as extra folders it may
	// read (the Python environment).
	Command(ctx context.Context, work string, readOnly []string, env []string, argv []string) *exec.Cmd
}

// Detect returns the sandbox for this operating system.
func Detect() Sandbox {
	switch runtime.GOOS {
	case "darwin":
		return &seatbelt{exe: "/usr/bin/sandbox-exec"}
	case "linux":
		return &bubblewrap{}
	}
	return unsupported{reason: "running code needs a sandbox, and there is none for " + runtime.GOOS + " yet"}
}

type unsupported struct{ reason string }

func (u unsupported) Name() string              { return "none" }
func (u unsupported) Available() (bool, string) { return false, u.reason }
func (u unsupported) Command(ctx context.Context, _ string, _ []string, _ []string, argv []string) *exec.Cmd {
	return exec.CommandContext(ctx, "false")
}

// seatbelt is macOS's sandbox (sandbox-exec). The profile denies the
// network, writing anywhere but the working folder, and reading the users'
// folders except the working folder and the Python environment. Later
// rules override earlier ones.
type seatbelt struct{ exe string }

func (s *seatbelt) Name() string { return "macOS sandbox" }

func (s *seatbelt) Available() (bool, string) {
	if _, err := os.Stat(s.exe); err != nil {
		return false, "macOS's sandbox (sandbox-exec) is not on this computer"
	}
	return true, ""
}

// sbQuote quotes a path for a sandbox profile.
func sbQuote(p string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(p) + `"`
}

// realPath resolves symbolic links, which the sandbox matches against
// (/tmp is /private/tmp on macOS).
func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func seatbeltProfile(work string, readOnly []string) string {
	var b strings.Builder
	b.WriteString("(version 1)\n(allow default)\n(deny network*)\n(deny file-write*)\n")
	b.WriteString(`(deny file-read* (subpath "/Users") (subpath "/Volumes") (subpath "/private/var/root"))` + "\n")
	for _, p := range readOnly {
		fmt.Fprintf(&b, "(allow file-read* (subpath %s))\n", sbQuote(realPath(p)))
	}
	fmt.Fprintf(&b, "(allow file-read* file-write* (subpath %s))\n", sbQuote(realPath(work)))
	b.WriteString(`(allow file-write* (literal "/dev/null") (literal "/dev/stdout") (literal "/dev/stderr") (literal "/dev/tty"))` + "\n")
	return b.String()
}

func (s *seatbelt) Command(ctx context.Context, work string, readOnly []string, env []string, argv []string) *exec.Cmd {
	args := append([]string{"-p", seatbeltProfile(work, readOnly)}, argv...)
	cmd := exec.CommandContext(ctx, s.exe, args...)
	cmd.Dir = work
	cmd.Env = env
	return cmd
}

// bubblewrap runs the command in new namespaces (bwrap): no network, the
// system folders read-only, the Python environment read-only, and the
// working folder as the only writable place, seen as /work.
type bubblewrap struct {
	// exe is found on PATH when empty.
	exe string
}

func (b *bubblewrap) Name() string { return "bubblewrap" }

func (b *bubblewrap) path() string {
	if b.exe != "" {
		return b.exe
	}
	p, _ := exec.LookPath("bwrap")
	return p
}

func (b *bubblewrap) Available() (bool, string) {
	exe := b.path()
	if exe == "" {
		return false, "running code needs bubblewrap (the bwrap command); install it with your package manager, such as apt install bubblewrap"
	}
	// Some systems forbid the unprivileged namespaces bubblewrap needs.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	probe := exec.CommandContext(ctx, exe, "--unshare-all", "--ro-bind", "/", "/", "--", "true")
	if out, err := probe.CombinedOutput(); err != nil {
		return false, "bubblewrap cannot create a sandbox on this computer: " + strings.TrimSpace(string(out))
	}
	return true, ""
}

func bubblewrapArgs(work string, readOnly []string, argv []string) []string {
	args := []string{"--unshare-all", "--die-with-parent", "--new-session", "--clearenv"}
	for _, dir := range []string{"/usr", "/bin", "/lib", "/lib64", "/lib32", "/sbin", "/etc/alternatives", "/etc/ssl", "/etc/fonts", "/etc/ld.so.cache", "/etc/localtime"} {
		args = append(args, "--ro-bind-try", dir, dir)
	}
	for _, p := range readOnly {
		args = append(args, "--ro-bind", p, p)
	}
	args = append(args, "--bind", work, "/work", "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp", "--chdir", "/work")
	return append(append(args, "--"), argv...)
}

func (b *bubblewrap) Command(ctx context.Context, work string, readOnly []string, env []string, argv []string) *exec.Cmd {
	args := bubblewrapArgs(work, readOnly, argv)
	// --clearenv drops the daemon's variables; set the sandbox's own.
	var setenv []string
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			setenv = append(setenv, "--setenv", k, strings.ReplaceAll(v, work, "/work"))
		}
	}
	cmd := exec.CommandContext(ctx, b.path(), append(setenv, args...)...)
	cmd.Dir = work
	return cmd
}
