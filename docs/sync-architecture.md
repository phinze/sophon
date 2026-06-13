# Sync Architecture Notes

Working notes on how sophon should observe and relay Claude Code sessions, and
why the current JSONL-transcript-rendering path is the part that keeps needing
maintenance. Captured from a design session on 2026-06-12. This is thinking, not
a spec; treat it as a compass like ROADMAP.md.

## The constraint stack

Four constraints shape everything, and they're load-bearing in this order:

1. **tmux is the source of truth.** The canonical session is a stock `claude`
   running in a tmux pane that you can `tmux attach` into from any SSH shell.
2. **Stock interactive `claude`, not a wrapper sophon owns.** Sophon observes and
   injects; it does not become the thing you launch.
3. **Thin relay, not a brain** (the existing ROADMAP principle).
4. **Must work on the Max subscription, not metered API billing.**

Constraint 4 turns out to be the decisive one. It quietly rules out half the
design space, including an idea that looked attractive at first (see below).

## Why the transcript renderer is the treadmill

`transcript/transcript.go` is ~586 lines reverse-engineering Claude Code's
*internal* JSONL: the message envelope, `isMeta`, `<synthetic>` models,
`isApiErrorMessage`, thinking blocks, system-reminder stripping, per-tool input
field knowledge in `summarizeTool`, and so on. The canary is
`preservePlanWriteInputs`, a backwards-scanning heuristic that re-associates a
`Write` call with a later `ExitPlanMode` because the plan text isn't where we
want it. That function is the "fallen behind" feeling made concrete.

None of this is sophon's actual value. It's incidental infra we got conscripted
into owning, and every Claude Code release that nudges the transcript shape
silently degrades it. The problem is structural, not a matter of catching up.

## The billing fact that decides the design

Interactive `claude` used in the terminal draws from the Max subscription, same
as before. But `claude -p` (headless), `--output-format stream-json`, the Agent
SDK, Claude Code GitHub Actions, and any third-party app authenticating through
the Agent SDK do **not**. As of the June 15, 2026 billing change, those
programmatic surfaces stop drawing from subscription usage and instead consume a
separate metered credit pool ($20 Pro / $100 Max 5x / $200 Max 20x, at API
rates), and in some configurations require an `ANTHROPIC_API_KEY` outright.

So the tempting "become the wrapper and consume stream-json for clean structured
events" idea is dead twice over. It breaks constraint 2 (sophon would own the
invocation), and it breaks constraint 4 (every session observed that way falls
off the subscription onto metered billing). Scratch it for good.

The flip side is the happy part: everything sophon already does is billing-free
and subscription-safe, because none of it originates inference. Hooks are
callbacks from the interactive session you're already paying for. Reading the
JSONL is reading a file. `tmux capture-pane` is reading the terminal.
`send-keys` is typing into it. The whole observe-and-inject spine touches zero
billing surface.

## The job split (the actual architecture)

Match the surface to the job. The bug today is that one surface (JSONL) is doing
all three jobs, including the one it's worst at.

- **Actions** (plan-approval buttons, permission prompts) come from **hooks**.
  The permission details already arrive in the hook payload. For plan approval,
  capture `tool_input` at hook time (a `PreToolUse` hook on `ExitPlanMode` gets
  the plan directly) instead of reconstructing it from the transcript. Hooks are
  a small, consumer-designed, stable surface. Use them for anything structured
  and action-shaped.
- **Live "what's happening right now" glance** comes from **capture-pane**.
  Display, do not extract (see the parsing note below). Mirror the terminal,
  don't interpret it.
- **Complete history, search, summaries, cross-workspace view** comes from
  **JSONL**. It's the only complete, append-only record of a whole session.
  Here its churn is tolerable because this work is async and non-blocking: if the
  renderer lags a schema change for a week, nobody waiting on a notification is
  blocked.

The mistake right now is that JSONL parsing sits on the path of "respond to
Claude *right now*," which is exactly where its two weaknesses (lag and
schema-chasing) bite hardest.

## capture-pane, honestly

capture-pane is not superior to log parsing in general. It's superior for the
live-glance job only. The honest accounting:

**Wins:**
- *Durability.* It tracks tmux's interface, frozen for a decade, instead of
  Claude Code's internal JSONL schema, which churns. This is the whole point.
- *Fidelity.* It is literally the screen, no reconstruction gap.

**Loses:**
- *Lossy.* Bounded by tmux `history-limit`; anything older is gone. JSONL is the
  only complete record, so the archive half of the annex cannot run on
  capture-pane.
- *Unstructured.* Raw bytes with ANSI. No typed roles or block types.
- *Geometry-dependent.* The bytes are a function of terminal width and wrap at
  capture time.

**Empirical finding (2026-06-12):** Claude Code's TUI is *not* using the
alternate screen buffer (`tmux display-message -p '#{alternate_on}'` returns 0).
It scrolls inline in the normal buffer and tmux retains real scrollback. So
`capture-pane -S -` gets genuine backscroll, not just the visible viewport. The
worst-case failure mode (a vim-style full-screen app where capture-pane sees
only the current screen) does not apply. Caveat: this depends on `history-limit`
being set high. 100k is fine; the tmux default of 2000 would be lossy.

## Are we just chasing TUI parsing?

Only if we *extract*. The escape hatch is to treat capture-pane output as pixels
to render, never data to scrape. There's a real difference between:

- **Parsing a TUI** (brittle): matching box-drawing characters and layout
  heuristics to guess "this is a tool call." Breaks on width, wrap, theme, TUI
  version. Arguably a worse treadmill than JSONL.
- **Emulating a terminal** (solved): running the frozen VT100/ANSI spec through a
  state machine to get the authoritative screen grid. Stable because the spec
  hasn't meaningfully changed in decades.

So the rule is: capture-pane is for display, never for extraction. Semantic
structure comes from hooks. Keep that line clean and there's no TUI-parsing
treadmill.

## Terminal-emulation options (Ghostty et al.)

These make the emulation path concrete. All are pure emulation of bytes the
interactive `claude` already produced, so all are subscription-safe and keep
tmux as the source of truth.

- **`coder/ghostty-web`** — Ghostty's emulator compiled to WASM, shipping on npm,
  ~400KB, zero runtime deps, with an **xterm.js-compatible API**. The phone
  render target. Lets you prototype with plain xterm.js and swap Ghostty in later
  with near-zero churn. Bytes in, pixels out, no parsing.
- **`libghostty-vt` / `coder/libghostty-vt-node`** — a zero-dependency VT parser
  plus terminal-state machine, server-side. Feed it the pane or pty byte stream,
  read out a clean, geometry-normalized **grid** (rows, cells, styles, cursor) as
  a data structure, no regex. This is emulation, not scraping. It's the power
  tool for when you want to *reason about* the live screen (diff it between
  captures, detect working-vs-idle from screen state, derive a normalized text
  view for summaries). It gives a text grid, **not** semantic block structure;
  knowing "this is a tool_use" still comes from hooks.
- **Vercel `wterm`** — DOM-rendered terminal output (real HTML), so native text
  selection, browser find-in-page, and screen-reader support come for free. On a
  phone that's a meaningful UX win over a canvas blob.

## Salvage path for sophon

The job is smaller than it feels. We are demoting `transcript.go` off the live
path, not re-spining the whole thing.

1. Move the **live** context view to a `capture-pane -S -` snapshot taken on
   notification, rendered on the phone. Start with whatever renderer is quickest;
   `ghostty-web` is a drop-in fidelity upgrade later.
2. Move **actions** fully onto hooks. Capture `tool_input` at `ExitPlanMode` hook
   time, which deletes `preservePlanWriteInputs`. Also read `transcript_path`
   from the hook payload instead of recomputing the cwd slug in `TranscriptPath`.
3. Keep the JSONL transcript parser only for the **async archive**: `ExtractSummary`,
   session browsing, the Phase 2 cross-workspace dashboard. Completeness is the
   asset there and lag is harmless.
4. Hold `libghostty-vt` server-side in reserve for when sophon wants to reason
   about live screen state rather than just show it.

## Roadmap implication (Phase 3 / 4)

Originating work (scheduled sessions, routing) hits the same billing wall. If
sophon launches a session via `claude -p` to capture its output, that session
burns metered credits, not the subscription. To keep originated sessions on the
subscription, sophon has to launch them as a *real interactive* `claude` inside a
fresh tmux pane and drive them the same way it drives a human-started one
(`send-keys` in, `capture-pane` / hooks out), rather than using headless mode.

It's uglier than `claude -p "run /whatsup"`, but it's the only subscription-safe
way to make a workspace wake up and do work. Worth designing Phase 3 around a
"tmux-pane session launcher" primitive from the start rather than discovering the
billing cliff later.

## Minimal next step

Hooks for actions, capture-pane snapshot on the phone, `libghostty-vt` in
reserve. The decision that picks the render lib isn't fidelity, it's canvas
(`ghostty-web`) versus selectable DOM text (`wterm`); lean DOM on a phone.

## Refinements after re-examination (2026-06-13)

Three sharpenings from a second pass over the plan. None overturn the
architecture; they tighten the live-view section and surface a Phase 3 hazard.

**The "live view" is two jobs, and which one capture-pane serves is still open.**
"Live view" quietly bundles a *status glance* (is it working, what did it just
say, why did it stop) with a *readable conversation* (actually read the
back-and-forth). capture-pane is unambiguously right for the glance. Whether it
should ever become the readable transcript on the phone is the open question. The
conservative line is **glance from capture-pane, read from JSONL, no overlap**,
which keeps the TUI-parsing treadmill out by construction: you never extract
structure from the pane, you only display it. The other branch lets the phone
read the whole conversation off the pane, buying billing-free completeness on the
live path but pulling in reflow and geometry work (this is the `libghostty-vt`
job: feed it the byte stream, read a normalized grid). We don't have to decide
now. The respond-view MVP only needs the glance, so build that first and let the
readable-transcript question stay open until the live view is real and we can
feel the tradeoff instead of guessing at it.

**The geometry problem has a cheap MVP before libghostty-vt.** The canonical pane
is the human's attach target, so its width is whatever they run (easily 200
cols), which is hostile to a phone. Before reaching for a server-side VT emulator,
`tmux capture-pane -p -e -J -S -<N>` covers the glance: `-J` joins soft-wrapped
lines into logical lines, `-e` preserves color as ANSI, and xterm.js (or
ghostty-web later) re-wraps at the phone's width. That's a real render with color
and scrollback and no emulator. `libghostty-vt` earns its place only when we want
to *reason about* screen state (working-vs-idle from the grid, diffing captures),
which is a different need than showing it.

**Phase 3's puppet-launcher can hang on a permission prompt.** Interactive-in-tmux
beats `claude -p` for billing, but an *autonomous* interactive session has a
failure mode a headless one doesn't: hit a permission prompt with no human at the
pane and it blocks forever. So the launcher primitive isn't just spawn-pane +
send-keys + watch-Stop-hook; it needs a permission posture per workspace (a
pre-approved skill/tool allowlist, or a sandboxed workspace running
skip-permissions, which has to square with bottom-up trust). Noted now because it
shapes what the launcher must expose, even though it's far off.

## Implementation sequence

Concrete steps, ordered. A and B are independent; A ships first because it
deletes the canary.

**Step A — Actions onto hooks.** Today `HookEvent` (`hook/hook.go`) carries six
fields and neither `tool_input` nor `transcript_path`. Add them, plus the
hook-event discriminator for `PreToolUse`. Register a `PreToolUse` hook on
`ExitPlanMode` so the plan text arrives directly in the payload; forward it to the
daemon and render the approval view from hook data. Capture `transcript_path`
once per session and store it, so the agent stops recomputing the cwd slug in
`TranscriptPath`. Then delete `preservePlanWriteInputs` and the live-path
plan-reconstruction scans in `transcript.go`. Catch: the new hook registration is
a Claude Code settings change deployed per-node via `nix-config`, so this lands
across two repos.

**Step B — capture-pane glance on the respond view.** Add `CapturePane` to
`tmux.go` and an agent endpoint to proxy it (the agent already does send-keys and
pane queries; this is the read direction). On notification the respond view pulls
a snapshot, and notify-time is the ideal trigger because it's exactly the moment
context is wanted. MVP renders `-e -J` ANSI with xterm.js; ghostty-web is a later
drop-in fidelity upgrade.

**Step C — Demote JSONL to the async archive.** Mostly falls out of A and B:
confirm nothing on the live path still calls the transcript parser, then keep it
only for `ExtractSummary`, session browsing, and the Phase 2 dashboard, where
completeness is the asset and lag is harmless.

**Step D — Reserve.** `libghostty-vt` for reasoning about live screen state; the
puppet-launcher for Phase 3, designed around the permission-posture note above.

## Sources

- Anthropic ends subscription subsidy for agents June 15: https://www.techtimes.com/articles/317625/20260602/anthropic-ends-subscription-subsidy-agents-june-15-credit-pool-replaces-flat-rate-access.htm
- `claude -p` caused unintended API billing (anthropics/claude-code#37686): https://github.com/anthropics/claude-code/issues/37686
- coder/ghostty-web: https://github.com/coder/ghostty-web
- coder/libghostty-vt-node: https://github.com/coder/libghostty-vt-node
- Mitchell Hashimoto, Libghostty Is Coming: https://mitchellh.com/writing/libghostty-is-coming
- Vercel wterm: https://www.stork.ai/blog/vercels-new-tool-ends-terminal-hell
