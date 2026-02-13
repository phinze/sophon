# sophon

Notification + response relay for Claude Code sessions. Observes hook events, sends push notifications via ntfy, and lets you respond from your phone via a web UI that injects text into the tmux pane.

## Architecture

- `sophon daemon` — HTTP server managing session state, serving web UI, sending ntfy notifications, and executing tmux send-keys
- `sophon hook` — Reads Claude Code hook JSON from stdin, forwards to daemon. Falls back to direct ntfy if daemon is down.

## Building

```bash
go build -o sophon .   # or: make build
go test ./...           # or: make test
nix build              # Nix package
```

## Testing

```bash
# Start daemon
sophon daemon --ntfy-url https://host/topic --base-url https://host

# Simulate hook events
echo '{"hook_event_name":"SessionStart","session_id":"test1","cwd":"/tmp/test"}' | sophon hook
echo '{"hook_event_name":"Notification","session_id":"test1","notification_type":"permission_prompt","message":"Allow Bash?"}' | sophon hook
echo '{"hook_event_name":"Stop","session_id":"test1"}' | sophon hook
```

## Nix Integration

The flake exports `homeManagerModules.default` for declarative configuration including systemd service and Claude Code hook wiring.
