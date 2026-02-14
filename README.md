# sophon

A notification and response relay for [Claude Code](https://docs.anthropic.com/en/docs/claude-code) sessions. Sends push notifications via [ntfy](https://ntfy.sh) when Claude needs attention, and lets you respond from your phone through a web UI that injects text into the tmux pane.

## How it works

```
Claude Code (in tmux)
    │
    ├─ Hook fires ──→ sophon daemon (HTTP)
    │                     │
    │                     ├─ Sends ntfy notification with Click URL
    │                     │
    │                 You tap notification
    │                     │
    │                     ▼
    │                 Web form: context + response buttons
    │                     │
    ◄── tmux send-keys ───┘
```

**`sophon daemon`** — HTTP server that manages session state, sends ntfy notifications, serves the response web UI, and executes `tmux send-keys` to relay your input.

**`sophon hook`** — Reads Claude Code hook JSON from stdin and forwards it to the daemon. Falls back to direct ntfy if the daemon is down.

## Install

Sophon is packaged as a Nix flake with a home-manager module that wires up the systemd service:

```nix
# flake.nix inputs
sophon.url = "github:phinze/sophon";

# home-manager config
imports = [ inputs.sophon.homeManagerModules.default ];

services.sophon = {
  enable = true;
  ntfyUrl = "https://your-host/topic";
  baseUrl = "https://your-host";
};
```

The module creates a systemd user service and you wire the hooks in your Claude Code settings to point at `sophon hook`.
