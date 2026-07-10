# sophon

A notification and response relay for Claude Code, Codex, and Antigravity CLI sessions. Sophon tracks agents running in tmux, shows their conversations in a phone-friendly web UI, and sends responses back with `tmux send-keys`.

## How it works

```text
Claude Code / Codex / Antigravity (in tmux)
    │
    ├─ lifecycle hook ──→ sophon daemon ←── sophon agent
    │                         │                  │
    │                         │                  ├─ reads local transcripts
    │                         │                  └─ checks tmux panes
    │                         ▼
    │                    response web UI
    │                         │
    ◄────── tmux send-keys ───┘
```

`sophon daemon` is the coordinator. It stores session state and serves the web UI. `sophon agent` runs on each development machine and provides node-local transcript and tmux access. `sophon hook` normalizes each supported agent's lifecycle events into the coordinator API.

Sophon reads the native transcript format for each provider. Claude Code JSONL, Codex rollout JSONL, and Antigravity `transcript.jsonl` are all rendered into the same conversation view.

## Install

Sophon is packaged as a Nix flake with a Home Manager module:

```nix
# flake.nix inputs
sophon.url = "github:phinze/sophon";

# home-manager config
imports = [ inputs.sophon.homeManagerModules.default ];

services.sophon = {
  enable = true;
  daemonUrl = "https://sophon.example.com";
  nodeName = "workstation";
  agent.enable = true;
};
```

The module installs `sophon` and can run the per-node agent under systemd or launchd. `services.sophon.hookCommand` exposes the fully configured base hook command.

## Agent hooks

All three agents must run inside tmux for phone responses and pane reconciliation. Replace `/path/to/sophon` and the daemon/node values below, or use the module's `services.sophon.hookCommand` value.

### Claude Code

Point the existing Claude Code lifecycle hooks at the base command:

```json
{
  "hooks": {
    "SessionStart": [{ "hooks": [{ "type": "command", "command": "/path/to/sophon hook --provider claude --daemon-url https://sophon.example.com --node-name workstation" }] }],
    "Notification": [{ "hooks": [{ "type": "command", "command": "/path/to/sophon hook --provider claude --daemon-url https://sophon.example.com --node-name workstation" }] }],
    "PreToolUse": [{ "hooks": [{ "type": "command", "command": "/path/to/sophon hook --provider claude --daemon-url https://sophon.example.com --node-name workstation" }] }],
    "PostToolUse": [{ "hooks": [{ "type": "command", "command": "/path/to/sophon hook --provider claude --daemon-url https://sophon.example.com --node-name workstation" }] }],
    "Stop": [{ "hooks": [{ "type": "command", "command": "/path/to/sophon hook --provider claude --daemon-url https://sophon.example.com --node-name workstation" }] }],
    "SessionEnd": [{ "hooks": [{ "type": "command", "command": "/path/to/sophon hook --provider claude --daemon-url https://sophon.example.com --node-name workstation" }] }]
  }
}
```

### Codex

Codex uses the same lifecycle payload shape. Add the following to `~/.codex/hooks.json`:

```json
{
  "hooks": {
    "SessionStart": [{ "hooks": [{ "type": "command", "command": "/path/to/sophon hook --provider codex --daemon-url https://sophon.example.com --node-name workstation" }] }],
    "PermissionRequest": [{ "hooks": [{ "type": "command", "command": "/path/to/sophon hook --provider codex --daemon-url https://sophon.example.com --node-name workstation" }] }],
    "PreToolUse": [{ "hooks": [{ "type": "command", "command": "/path/to/sophon hook --provider codex --daemon-url https://sophon.example.com --node-name workstation" }] }],
    "PostToolUse": [{ "hooks": [{ "type": "command", "command": "/path/to/sophon hook --provider codex --daemon-url https://sophon.example.com --node-name workstation" }] }],
    "Stop": [{ "hooks": [{ "type": "command", "command": "/path/to/sophon hook --provider codex --daemon-url https://sophon.example.com --node-name workstation" }] }]
  }
}
```

Codex has no `SessionEnd` event. Sophon's agent marks the session stopped when the `codex` process leaves its tmux pane.

### Antigravity CLI

Antigravity uses camelCase payloads and requires hook-specific JSON responses. Create a small global plugin at `~/.gemini/antigravity-cli/plugins/sophon/`:

`plugin.json`:

```json
{
  "name": "sophon"
}
```

`hooks.json`:

```json
{
  "sophon": {
    "PreInvocation": [
      { "type": "command", "command": "/path/to/sophon hook --provider antigravity --event PreInvocation --daemon-url https://sophon.example.com --node-name workstation" }
    ],
    "PostInvocation": [
      { "type": "command", "command": "/path/to/sophon hook --provider antigravity --event PostInvocation --daemon-url https://sophon.example.com --node-name workstation" }
    ],
    "Stop": [
      { "type": "command", "command": "/path/to/sophon hook --provider antigravity --event Stop --daemon-url https://sophon.example.com --node-name workstation" }
    ]
  }
}
```

Antigravity does not currently expose permission prompts as an observational hook, so Sophon can relay responses and completed turns but cannot distinguish a pending permission dialog from other in-progress work. Like Codex, process-based reconciliation closes the session when `agy` exits.

## Development

```bash
go build -o sophon .
go test ./...
nix build
```

This remains tailored to a NixOS/macOS, tmux, and private-network workflow. The provider adapters are deliberately small and the daemon API is provider-neutral, so adding another hook and transcript format should not require forking the rest of the system.
