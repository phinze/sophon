# Sophon Roadmap

Directional guidance for where sophon is headed. Not a commitment schedule — a
compass for incremental development.

## Context

Sophon exists because of a pattern that emerged from building a constellation of
Claude Code workspaces (pim-stuff, music-stuff, money-stuff, ha-stuff, etc.).
Each workspace is a domain-scoped tool environment where capabilities are built
incrementally from the bottom up — every tool is understood because it was built
collaboratively in-session.

The gap that appeared: **the more workspaces you have, the more you want to
interact outside the terminal window.** Sophon started as a notification relay
and is growing toward a central coordination layer for all Claude Code activity.

### Design Principles

These come from watching OpenClaw (the viral "AI agent OS") stumble on security
while nailing interaction design. We want the interaction model without the trust
model.

1. **Bottom-up trust.** Capabilities are earned incrementally, not granted by
   default. Each workspace has only the permissions its domain requires.
2. **Thin routing, deep specialization.** Sophon is a message bus, not a brain.
   Domain expertise lives in the workspaces. Sophon just connects them.
3. **Composable primitives.** Small, understandable pieces wired together beat a
   monolithic agent runtime. We should be able to explain every component.
4. **Chat is the interface, not the architecture.** Being reachable from your
   phone is a UX feature, not an architectural commitment. The underlying
   model stays workspace-centric.
5. **Own every line.** No marketplace of unaudited community skills. Tools are
   built collaboratively or they don't ship.

## What Exists Today (Phase 0)

Sophon is a notification + response relay for Claude Code sessions:

- **Hook command** reads Claude Code hook JSON, forwards to daemon
- **Daemon** (HTTP server) manages sessions in SQLite, sends ntfy notifications
- **Response web UI** receives text input, injects into tmux via send-keys
- **Graceful fallback** — hook sends ntfy directly if daemon is down
- **Nix/home-manager module** for declarative systemd deployment
- **Sessions dashboard** showing active and recent sessions

This covers the core loop: Claude Code needs attention → phone buzzes → you
respond from the web form → text lands in the terminal.

### Transcript Rendering — Future Ideas

Things that would improve transcript quality but aren't urgent enough to track as
issues yet. Inspired by analysis of
[claude-devtools](https://github.com/matt1398/claude-devtools).

- **Compaction markers** — Detect `summary` type JSONL entries and insert a
  visual divider in the transcript. Helps the mobile reviewer understand that
  older messages are no longer in Claude's context window.
- **Subagent rendering** — Discover `agent_*.jsonl` sidechain files and render
  subagent work inline or as expandable sections. More relevant if/when we build
  a richer dashboard view.

## Phase 1: Bidirectional Chat Interface

**Goal:** Replace the notification-and-web-form loop with a proper
conversational interface. Text your assistant, get a response back.

### Why This Matters

The single highest-impact capability from OpenClaw's model is being reachable
from any chat app. Right now responding to sophon means: tap notification → wait
for browser → type in a form → submit. A native chat experience (iMessage,
Signal, or similar) would make interaction feel natural instead of transactional.

### Possible Approaches

- **ntfy → Poke (ntfy iOS client)** — Poke supports reply actions on
  notifications. This might get us bidirectional with zero new infrastructure.
  Worth prototyping first as the simplest path.
- **iMessage bridge** — BlueBubbles or similar on macOS. Native chat feel, no
  new app to install. Platform-locked to Apple ecosystem.
- **Signal bot** — signal-cli provides a programmatic interface. Cross-platform,
  encrypted, but requires a dedicated phone number.
- **Matrix** — Self-hosted, open protocol, good bridge ecosystem. More
  infrastructure to run but most flexible long-term.

### Key Design Questions

- Does the response need to target a specific session, or can sophon route based
  on context? (If there's only one active session waiting, route there.)
- How do we handle multiple sessions waiting simultaneously? Thread-per-session?
  Numbered selection?
- Should the chat interface show Claude's output too, or just
  notifications/prompts?

## Phase 2: Cross-Workspace Awareness

**Goal:** Sophon knows what's happening across all workspaces and can provide a
unified view.

### The Problem

Today each workspace is a silo. pim-stuff doesn't know music-stuff is
mid-discovery session. There's no way to ask "what's going on across all my
projects?" without opening each terminal.

### What This Looks Like

- **Session registry** — Sophon already tracks sessions with project names
  derived from cwd. Extend this to maintain richer metadata: what skill is
  running, last activity time, session summary.
- **Status dashboard** — Evolve the existing sessions page into a unified
  operations view. "pim-stuff: idle, last triage 3h ago. music-stuff: active,
  running /discover. ha-stuff: no recent sessions."
- **Cross-workspace context in chat** — When interacting via the Phase 1 chat
  interface, sophon can include workspace status as context. "You have 2 active
  sessions. pim-stuff is waiting for triage approval."

### What This Is NOT

This is not agent-to-agent messaging. Workspaces don't talk to each other
autonomously. Sophon provides visibility; the human decides what to act on.

## Phase 3: Proactive Scheduling

**Goal:** Workspaces can wake up on a schedule and do useful work without
waiting for you to open a terminal.

### The Insight

OpenClaw's heartbeat feature (agent wakes up every 30 minutes, checks for
pending tasks) is genuinely useful. A morning email triage summary, a weekly
portfolio check, a daily calendar briefing — these shouldn't require you to
remember to start a session.

### How It Works

- **Cron-driven session launcher** — Sophon (or a cron job it manages) starts a
  Claude Code session in a tmux pane, runs a specific skill, captures output.
- **Output capture and delivery** — Session output gets summarized and pushed
  through the Phase 1 chat interface.
- **Schedule configuration** — Define schedules per workspace. Could be as
  simple as a `schedules` table in SQLite or a config file.

### Examples

```
# Morning briefing: triage + calendar + portfolio snapshot
08:00  pim-stuff      /whatsup
08:00  pim-stuff      /triage --summary-only
08:05  money-stuff    portfolio check

# Evening wind-down
18:00  music-stuff    /discover --quick

# Weekly
Mon 09:00  money-stuff  full portfolio analysis
```

### Trust Boundary

Scheduled sessions run the same skills you'd run interactively. No new
permissions, no autonomous tool creation. The schedule is human-configured, not
agent-decided. Sophon can suggest additions to the schedule during interactive
sessions, but never modifies it autonomously.

## Phase 4: Workspace Routing

**Goal:** Send a message to sophon and have it route to the right workspace
based on intent.

### The Vision

Instead of opening specific terminals, you text sophon:

- "archive my github notifications" → routes to pim-stuff, runs archive-search
- "what's my portfolio doing today" → routes to money-stuff
- "play something chill" → routes to music-stuff
- "is my 3d print done" → routes to 3dprint-stuff, runs bambu-status

### How It Could Work

- **Intent classification** — Sophon uses a lightweight model call (or even
  keyword matching to start) to determine which workspace handles a request.
- **Workspace capability registry** — Each workspace declares what it can do
  (from CLAUDE.md, skills, or a manifest file). Sophon maintains an index.
- **Session dispatch** — Sophon starts (or reuses) a session in the target
  workspace, passes the request, streams back results.

### Complexity Warning

This is where the architecture gets genuinely hard. Routing ambiguity, session
lifecycle management, error handling across workspace boundaries, maintaining
conversational context across workspace switches — each of these is a real
problem. OpenClaw's 430K lines of TypeScript exist for a reason.

The mitigation: stay simple. Start with explicit routing (`@pim archive github
notifications`) before attempting intent classification. Build the dispatch
mechanism first, get clever about routing later.

## Phase 5 and Beyond: Open Questions

These are capabilities we might want eventually but don't have clear answers for
yet. Capturing them here so they inform earlier design decisions.

- **Voice interaction** — "Hey sophon" from a HomePod or phone. Depends on
  speech-to-text and TTS pipelines. Possibly leverage ha-stuff's Home Assistant
  integration as the voice frontend.
- **Workspace-to-workspace context sharing** — "Remember that speaker mount idea
  from the music session? Start a 3dprint-stuff session for that." Requires some
  form of cross-workspace memory, which is philosophically tricky given the
  isolation model.
- **Mobile companion** — A dedicated app instead of relying on chat bridges.
  Probably not worth building unless the chat interface proves fundamentally
  limiting.
- **Multi-user** — Other people interacting with specific workspaces. Far future
  and may never be needed for personal tooling.

## Anti-Goals

Things sophon explicitly should not become:

- **A monolithic agent runtime.** Sophon routes and relays. It does not execute
  domain logic. That's what workspaces are for.
- **A skill marketplace.** No installing untrusted code from strangers. Every
  tool is built collaboratively or vendored with full understanding.
- **A replacement for Claude Code.** Claude Code is the execution environment.
  Sophon is the connective tissue between sessions and the outside world.
- **Autonomously self-modifying.** Sophon doesn't write its own code or change
  its own configuration without human approval. Scheduled tasks are
  human-configured. Routing rules are human-configured.

## Relationship to OpenClaw

OpenClaw validates the interaction model (chat-first, proactive, multi-domain)
while serving as a cautionary tale on the trust model (broad default permissions,
unaudited community skills, 430K-line monolith with CVEs).

Sophon's thesis: you can get 80% of the interaction benefits with 1% of the
attack surface by keeping the architecture thin, the workspaces specialized, and
the trust boundaries tight.

| Capability | OpenClaw | Sophon Path |
|---|---|---|
| Chat interface | WhatsApp/Telegram/Discord/iMessage | Phase 1: chat bridge |
| Cross-domain awareness | Single omniscient agent | Phase 2: session registry |
| Proactive scheduling | Heartbeat + cron | Phase 3: cron + session launcher |
| Intent routing | One agent does everything | Phase 4: thin router to workspaces |
| Voice | ElevenLabs + wake word | Phase 5: maybe via Home Assistant |
| Community skills | ClawHub (5700+, 6% malicious) | Anti-goal: build, don't install |
| Security model | Restrict after granting | Bottom-up: grant as needed |
