package main

import (
	"flag"
	"os"

	"github.com/phinze/sophon/hook"
)

func runHook(args []string) error {
	fs := flag.NewFlagSet("hook", flag.ExitOnError)
	daemonURL := fs.String("daemon-url", "", "sophon daemon URL")
	ntfyURL := fs.String("ntfy-url", "", "ntfy URL for direct fallback")
	nodeName := fs.String("node-name", defaultNodeName(), "node name for this machine")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Environment variable fallbacks
	if *daemonURL == "" {
		*daemonURL = os.Getenv("SOPHON_DAEMON_URL")
	}
	if *daemonURL == "" {
		*daemonURL = "http://127.0.0.1:2587"
	}
	if *ntfyURL == "" {
		*ntfyURL = os.Getenv("SOPHON_NTFY_URL")
	}

	cfg := hook.Config{
		DaemonURL: *daemonURL,
		NtfyURL:   *ntfyURL,
		NodeName:  *nodeName,
	}

	return hook.Run(cfg)
}

func defaultNodeName() string {
	name, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return name
}
