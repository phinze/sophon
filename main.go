package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: sophon <command>\n\nCommands:\n  daemon  Run the HTTP server\n  hook    Process Claude Code hook events from stdin\n")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "daemon":
		if err := runDaemon(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "hook":
		if err := runHook(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}
