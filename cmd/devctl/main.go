package main

import (
	"fmt"
	"os"

	"github.com/yeixio/yggdrasil-core/internal/config"
	"github.com/yeixio/yggdrasil-core/internal/version"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
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
	case "completion":
		if err := completionCommand(os.Args[2:], os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: yggctl <version|about|paths|automations|mcp|completion>\n")
}
