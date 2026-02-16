# sophon

Notification + response relay for Claude Code sessions. Observes hook events, sends push notifications via ntfy, and lets you respond from your phone via a web UI that injects text into the tmux pane.

## Architecture

- `sophon daemon` — Coordinator HTTP server: session state, web UI, ntfy notifications. Proxies node-local operations (transcript, tmux send-keys, pane focus) to the appropriate agent.
- `sophon agent` — Per-node agent: reads transcripts from local filesystem, executes tmux send-keys, checks pane focus. Registers with daemon via heartbeat.
- `sophon hook` — Reads Claude Code hook JSON from stdin, forwards to daemon with `node_name`. Falls back to direct ntfy if daemon is down.

```
Hook → Daemon (coordinator) → Agent (per-node) → tmux / transcript files
```

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

# Start agent (on same or different node)
sophon agent --port 2588 --daemon-url http://127.0.0.1:2587

# Simulate hook events
echo '{"hook_event_name":"SessionStart","session_id":"test1","cwd":"/tmp/test"}' | sophon hook --node-name myhost
echo '{"hook_event_name":"Notification","session_id":"test1","notification_type":"permission_prompt","message":"Allow Bash?"}' | sophon hook --node-name myhost
echo '{"hook_event_name":"Stop","session_id":"test1"}' | sophon hook --node-name myhost
```

## Nix Integration

The flake exports `homeManagerModules.default` for declarative configuration including systemd services (daemon + agent) and Claude Code hook wiring.
