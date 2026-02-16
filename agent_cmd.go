package main

import (
	"flag"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/phinze/sophon/agent"
)

func runAgent(args []string) error {
	fs := flag.NewFlagSet("agent", flag.ExitOnError)
	port := fs.Int("port", 2588, "listen port")
	daemonURL := fs.String("daemon-url", "", "sophon daemon URL for registration")
	claudeDir := fs.String("claude-dir", defaultClaudeDir(), "Claude Code config directory")
	nodeName := fs.String("node-name", defaultNodeName(), "node name for this machine")
	logLevel := fs.String("log-level", "info", "log level (debug, info, warn, error)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Environment variable fallbacks
	if *daemonURL == "" {
		*daemonURL = os.Getenv("SOPHON_DAEMON_URL")
	}

	// Resolve claude dir to absolute path
	if !filepath.IsAbs(*claudeDir) {
		abs, err := filepath.Abs(*claudeDir)
		if err == nil {
			*claudeDir = abs
		}
	}

	level := slog.LevelInfo
	switch *logLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	cfg := agent.Config{
		Port:      *port,
		DaemonURL: *daemonURL,
		ClaudeDir: *claudeDir,
		NodeName:  *nodeName,
	}

	a := agent.New(cfg, logger)
	return a.Run()
}
