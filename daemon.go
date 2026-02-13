package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/phinze/sophon/server"
)

func runDaemon(args []string) error {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	port := fs.Int("port", 2587, "listen port")
	ntfyURL := fs.String("ntfy-url", "", "ntfy server URL (e.g. https://host/topic)")
	baseURL := fs.String("base-url", "", "public base URL for sophon (e.g. https://host)")
	minAge := fs.Int("min-session-age", 120, "minimum session age in seconds before stop notifications")
	logLevel := fs.String("log-level", "info", "log level (debug, info, warn, error)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Environment variable fallbacks
	if *ntfyURL == "" {
		*ntfyURL = os.Getenv("SOPHON_NTFY_URL")
	}
	if *baseURL == "" {
		*baseURL = os.Getenv("SOPHON_BASE_URL")
	}
	if *ntfyURL == "" {
		return fmt.Errorf("--ntfy-url or SOPHON_NTFY_URL is required")
	}
	if *baseURL == "" {
		return fmt.Errorf("--base-url or SOPHON_BASE_URL is required")
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

	cfg := server.Config{
		Port:          *port,
		NtfyURL:       *ntfyURL,
		BaseURL:       *baseURL,
		MinSessionAge: *minAge,
	}

	srv := server.New(cfg, logger)
	return srv.Run()
}
