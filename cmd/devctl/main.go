package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"time"

	"golang.org/x/term"

	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/version"
)

func main() {
	if words, ok := askedForHelp(os.Args[1:]); ok {
		text, err := helpTopic(words)
		exit(err)
		fmt.Print(text)
		return
	}
	switch os.Args[1] {
	case "version", "about":
		fmt.Print(version.CurrentOffer().Text())
	case "paths":
		cfg := config.DefaultConfig()
		fmt.Printf("data:     %s\n", cfg.DataDir)
		fmt.Printf("models:   %s\n", cfg.ModelsDir)
		fmt.Printf("runtimes: %s\n", cfg.RuntimesDir)
		fmt.Printf("logs:     %s\n", cfg.LogsDir)
		fmt.Printf("db:       %s\n", cfg.DBPath)
	case "automations":
		if err := automationsCommand(os.Args[2:], os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "mcp":
		if err := mcpCommand(os.Args[2:], os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "join":
		exit(joinCommand(os.Args[2:], newDaemon(), os.Stdout))
	case "join-token":
		exit(joinTokenCommand(os.Args[2:], newDaemon(), os.Stdout, time.Now))
	case "network":
		exit(networkCommand(os.Args[2:], newDaemon(), os.Stdout))
	case "leave":
		exit(leaveCommand(os.Args[2:], newDaemon(), os.Stdout))
	case "people":
		info, _ := os.Stdin.Stat()
		terminal := info != nil && info.Mode()&os.ModeCharDevice != 0
		exit(peopleCommand(os.Args[2:], newDaemon(), peopleIO{in: os.Stdin, out: os.Stdout, prompt: os.Stderr, terminal: terminal}))
	case "models":
		exit(modelsCommand(os.Args[2:], newDaemon(), os.Stdout))
	case "ask":
		info, _ := os.Stdin.Stat()
		piped := info != nil && info.Mode()&os.ModeCharDevice == 0
		exit(askCommand(os.Args[2:], askIO{stdin: os.Stdin, stdinPiped: piped, out: os.Stdout}, newAskClient))
	case "chat":
		// Ctrl-C stops the work, not the chat. At the prompt it leaves.
		interrupts := make(chan os.Signal, 1)
		signal.Notify(interrupts, os.Interrupt)
		streams := chatIO{
			interrupts: interrupts,
			in:         os.Stdin, inTerminal: term.IsTerminal(int(os.Stdin.Fd())),
			out: os.Stdout, outTerminal: term.IsTerminal(int(os.Stdout.Fd())),
			errOut: os.Stderr,
			size: func() (int, int) {
				cols, rows, err := term.GetSize(int(os.Stdout.Fd()))
				if err != nil {
					return 0, 0
				}
				return cols, rows
			},
		}
		exit(chatCommand(os.Args[2:], streams, func() (chatAPI, error) {
			if err := startCore(newDaemon(), streams, thisComputer(), time.Minute); err != nil {
				return nil, err
			}
			return newDaemonChat()
		}))
	case "projects":
		exit(projectsCommand(os.Args[2:], newDaemon(), os.Stdout))
	case "connect":
		dir, _ := os.Getwd()
		home, _ := os.UserHomeDir()
		exit(connectCommand(os.Args[2:], newDaemon(), os.Stdout, dir, home))
	case "completion":
		if err := completionCommand(os.Args[2:], os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	default:
		exit(unknownCommand("", os.Args[1], helpEntries()))
	}
}

// exit ends with err's message and exit status: 2 for usage, 1 otherwise.
func exit(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, err)
	var e *exitError
	if errors.As(err, &e) {
		os.Exit(e.code)
	}
	os.Exit(1)
}
