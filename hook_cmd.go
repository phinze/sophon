package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/phinze/sophon/hook"
)

func runHook(args []string) error {
	fs := flag.NewFlagSet("hook", flag.ExitOnError)
	daemonURL := fs.String("daemon-url", "", "sophon daemon URL")
	nodeName := fs.String("node-name", defaultNodeName(), "node name for this machine")
	provider := fs.String("provider", "auto", "hook provider (auto, claude, codex, antigravity)")
	eventName := fs.String("event", "", "provider event name (required for Antigravity hooks)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *provider == "antigravity" {
		switch *eventName {
		case "PreInvocation", "PostInvocation", "Stop":
		default:
			return fmt.Errorf("--event must be PreInvocation, PostInvocation, or Stop for Antigravity hooks")
		}
	}

	// Environment variable fallbacks
	if *daemonURL == "" {
		*daemonURL = os.Getenv("SOPHON_DAEMON_URL")
	}
	if *daemonURL == "" {
		*daemonURL = "http://127.0.0.1:2587"
	}

	cfg := hook.Config{
		DaemonURL: *daemonURL,
		NodeName:  *nodeName,
		Provider:  *provider,
		EventName: *eventName,
	}

	err := hook.Run(cfg)
	if *provider == "antigravity" {
		// Antigravity requires event-specific JSON on stdout. Sophon is an
		// observer, so each response preserves the default execution flow.
		var response any = map[string]any{}
		switch *eventName {
		case "PreInvocation", "PostInvocation":
			response = map[string]any{"injectSteps": []any{}}
		case "Stop":
			response = map[string]any{"decision": "allow"}
		}
		if encodeErr := json.NewEncoder(os.Stdout).Encode(response); encodeErr != nil && err == nil {
			err = encodeErr
		}
	}
	return err
}

func defaultNodeName() string {
	name, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return name
}
