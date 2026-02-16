package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/phinze/sophon/server"
	"github.com/phinze/sophon/store"
)

func runDaemon(args []string) error {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	port := fs.Int("port", 2587, "listen port")
	ntfyURL := fs.String("ntfy-url", "", "ntfy server URL (e.g. https://host/topic)")
	baseURL := fs.String("base-url", "", "public base URL for sophon (e.g. https://host)")
	minAge := fs.Int("min-session-age", 120, "minimum session age in seconds before stop notifications")
	logLevel := fs.String("log-level", "info", "log level (debug, info, warn, error)")
	dataDir := fs.String("data-dir", defaultDataDir(), "directory for persistent data (SQLite database)")
	claudeDir := fs.String("claude-dir", defaultClaudeDir(), "Claude Code config directory (for reading transcripts)")

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

	// Create data directory and open store
	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		return fmt.Errorf("creating data directory: %w", err)
	}

	dbPath := filepath.Join(*dataDir, "sophon.db")
	st, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer st.Close()

	logger.Info("database opened", "path", dbPath)

	cfg := server.Config{
		Port:          *port,
		NtfyURL:       *ntfyURL,
		BaseURL:       *baseURL,
		MinSessionAge: *minAge,
		ClaudeDir:     *claudeDir,
	}

	srv := server.New(cfg, st, logger)
	return srv.Run()
}

func defaultClaudeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".claude"
	}
	return filepath.Join(home, ".claude")
}

func defaultDataDir() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "sophon")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "sophon-data")
	}
	return filepath.Join(home, ".local", "share", "sophon")
}
