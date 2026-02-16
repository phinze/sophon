package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const currentSchemaVersion = 3

// ErrNotFound is returned when a session is not found.
var ErrNotFound = errors.New("session not found")

// Session represents a Claude Code session.
type Session struct {
	ID             string    `json:"session_id"`
	TmuxPane       string    `json:"tmux_pane"`
	Cwd            string    `json:"cwd"`
	Project        string    `json:"project"`
	NodeName       string    `json:"node_name"`
	StartedAt      time.Time `json:"started_at"`
	StoppedAt      time.Time `json:"stopped_at,omitempty"`      // zero means active
	LastActivityAt time.Time `json:"last_activity_at,omitempty"` // tracks last meaningful event

	// Latest notification context
	NotificationType string    `json:"notification_type,omitempty"`
	NotifyTitle      string    `json:"notify_title,omitempty"`
	NotifyMessage    string    `json:"notify_message,omitempty"`
	NotifiedAt       time.Time `json:"notified_at,omitempty"`
}

// Store provides SQLite-backed session persistence.
type Store struct {
	db *sql.DB
}

// Open opens a SQLite database at the given path, runs migrations, and enables WAL mode.
func Open(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enabling WAL mode: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	// Create schema_version table if it doesn't exist
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return err
	}

	var version int
	err := s.db.QueryRow(`SELECT version FROM schema_version LIMIT 1`).Scan(&version)
	if err == sql.ErrNoRows {
		version = 0
	} else if err != nil {
		return err
	}

	if version < 1 {
		if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS sessions (
			id                TEXT PRIMARY KEY,
			tmux_pane         TEXT NOT NULL DEFAULT '',
			cwd               TEXT NOT NULL DEFAULT '',
			project           TEXT NOT NULL DEFAULT '',
			started_at        TEXT NOT NULL,
			stopped_at        TEXT,
			notification_type TEXT NOT NULL DEFAULT '',
			notify_title      TEXT NOT NULL DEFAULT '',
			notify_message    TEXT NOT NULL DEFAULT '',
			notified_at       TEXT
		)`); err != nil {
			return err
		}
		version = 1
	}

	if version < 2 {
		if _, err := s.db.Exec(`ALTER TABLE sessions ADD COLUMN last_activity_at TEXT`); err != nil {
			// Column may already exist if migration was partially applied
			if !strings.Contains(err.Error(), "duplicate column") {
				return err
			}
		}
		version = 2
	}

	if version < 3 {
		if _, err := s.db.Exec(`ALTER TABLE sessions ADD COLUMN node_name TEXT NOT NULL DEFAULT ''`); err != nil {
			if !strings.Contains(err.Error(), "duplicate column") {
				return err
			}
		}
		version = 3
	}

	// Upsert the version
	if _, err := s.db.Exec(`DELETE FROM schema_version`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`INSERT INTO schema_version (version) VALUES (?)`, version); err != nil {
		return err
	}
	return nil
}

// CreateSession inserts or replaces a session.
func (s *Store) CreateSession(sess *Session) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO sessions
		(id, tmux_pane, cwd, project, node_name, started_at, stopped_at, last_activity_at, notification_type, notify_title, notify_message, notified_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.TmuxPane, sess.Cwd, sess.Project, sess.NodeName,
		formatTime(sess.StartedAt), formatNullableTime(sess.StoppedAt),
		formatNullableTime(sess.LastActivityAt),
		sess.NotificationType, sess.NotifyTitle, sess.NotifyMessage,
		formatNullableTime(sess.NotifiedAt),
	)
	return err
}

// GetSession retrieves a session by ID. Returns ErrNotFound if not found.
func (s *Store) GetSession(id string) (*Session, error) {
	row := s.db.QueryRow(`SELECT id, tmux_pane, cwd, project, node_name, started_at, stopped_at, last_activity_at,
		notification_type, notify_title, notify_message, notified_at
		FROM sessions WHERE id = ?`, id)
	sess, err := scanSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return sess, err
}

// UpdateSession updates an existing session by ID.
func (s *Store) UpdateSession(sess *Session) error {
	result, err := s.db.Exec(`UPDATE sessions SET
		tmux_pane = ?, cwd = ?, project = ?, node_name = ?, started_at = ?, stopped_at = ?, last_activity_at = ?,
		notification_type = ?, notify_title = ?, notify_message = ?, notified_at = ?
		WHERE id = ?`,
		sess.TmuxPane, sess.Cwd, sess.Project, sess.NodeName,
		formatTime(sess.StartedAt), formatNullableTime(sess.StoppedAt),
		formatNullableTime(sess.LastActivityAt),
		sess.NotificationType, sess.NotifyTitle, sess.NotifyMessage,
		formatNullableTime(sess.NotifiedAt),
		sess.ID,
	)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ReapStoppedSessions deletes sessions that have been stopped longer than ttl.
// Returns the IDs of deleted sessions.
func (s *Store) ReapStoppedSessions(ttl time.Duration) ([]string, error) {
	cutoff := time.Now().Add(-ttl)
	rows, err := s.db.Query(`DELETE FROM sessions WHERE stopped_at IS NOT NULL AND stopped_at < ? RETURNING id`,
		formatTime(cutoff))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return ids, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ListActiveSessions returns sessions that haven't been stopped, newest first.
func (s *Store) ListActiveSessions() ([]*Session, error) {
	rows, err := s.db.Query(`SELECT id, tmux_pane, cwd, project, node_name, started_at, stopped_at, last_activity_at,
		notification_type, notify_title, notify_message, notified_at
		FROM sessions WHERE stopped_at IS NULL ORDER BY started_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSessions(rows)
}

// ListRecentSessions returns stopped sessions ordered by stopped_at DESC, limited to n.
func (s *Store) ListRecentSessions(limit int) ([]*Session, error) {
	rows, err := s.db.Query(`SELECT id, tmux_pane, cwd, project, node_name, started_at, stopped_at, last_activity_at,
		notification_type, notify_title, notify_message, notified_at
		FROM sessions WHERE stopped_at IS NOT NULL ORDER BY stopped_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSessions(rows)
}

// ProjectFromCwd extracts last two path components as project name.
func ProjectFromCwd(cwd string) string {
	trimmed := strings.TrimRight(cwd, "/")
	if trimmed == "" {
		return "unknown"
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return "unknown"
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanSession(s scanner) (*Session, error) {
	var sess Session
	var startedAt string
	var stoppedAt, lastActivityAt, notifiedAt sql.NullString

	err := s.Scan(
		&sess.ID, &sess.TmuxPane, &sess.Cwd, &sess.Project, &sess.NodeName,
		&startedAt, &stoppedAt, &lastActivityAt,
		&sess.NotificationType, &sess.NotifyTitle, &sess.NotifyMessage,
		&notifiedAt,
	)
	if err != nil {
		return nil, err
	}

	sess.StartedAt, err = parseTime(startedAt)
	if err != nil {
		return nil, fmt.Errorf("parsing started_at: %w", err)
	}
	if stoppedAt.Valid {
		sess.StoppedAt, err = parseTime(stoppedAt.String)
		if err != nil {
			return nil, fmt.Errorf("parsing stopped_at: %w", err)
		}
	}
	if lastActivityAt.Valid {
		sess.LastActivityAt, err = parseTime(lastActivityAt.String)
		if err != nil {
			return nil, fmt.Errorf("parsing last_activity_at: %w", err)
		}
	}
	if notifiedAt.Valid {
		sess.NotifiedAt, err = parseTime(notifiedAt.String)
		if err != nil {
			return nil, fmt.Errorf("parsing notified_at: %w", err)
		}
	}
	return &sess, nil
}

func scanSessions(rows *sql.Rows) ([]*Session, error) {
	var sessions []*Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return sessions, err
		}
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func formatNullableTime(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func parseTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339, s)
}
