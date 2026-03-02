import { marked } from "marked";
import {
  Session,
  GlobalEvent,
  AskQuestionInput,
  TranscriptData,
  TranscriptMessage,
} from "../types";
import { escapeHtml, timeAgo, debounce } from "../util";
import { SSEManager } from "../sse";

const apiBase = "";
let unsubs: (() => void)[] = [];
let sessionId = "";
let renderedCount = 0;
let planButtonsShown = false;

function showStatus(msg: string, ok: boolean): void {
  const el = document.getElementById("status");
  if (!el) return;
  el.textContent = msg;
  el.className = "status " + (ok ? "ok" : "err");
  if (ok) setTimeout(() => { el.className = "status"; }, 3000);
}

function send(text: string): void {
  fetch(apiBase + "/api/respond/" + sessionId, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ text }),
  })
    .then((r) => {
      if (r.ok) {
        showStatus("Sent: " + text, true);
        // Clear notification UI since we've responded
        document.querySelector(".context")?.remove();
        document.querySelector(".quick-buttons")?.remove();
      } else r.text().then((t) => showStatus("Error: " + t, false));
    })
    .catch((e) => showStatus("Network error: " + e, false));
}

function sendText(): void {
  const input = document.getElementById("text") as HTMLInputElement | null;
  if (!input) return;
  const text = input.value.trim();
  if (!text) return;
  send(text);
  input.value = "";
}

function renderMarkdown(s: string): string {
  return marked.parse(s) as string;
}

function renderAskQuestion(input: AskQuestionInput): string {
  let html = "";
  const questions = input.questions || [];
  questions.forEach((q) => {
    html += '<div class="ask-question">';
    if (q.header) {
      html += '<div class="question-header">' + escapeHtml(q.header) + "</div>";
    }
    html += '<div class="question-text">' + escapeHtml(q.question) + "</div>";
    (q.options || []).forEach((opt, i) => {
      html += '<div class="option">';
      html += '<div class="option-label">' + (i + 1) + ". " + escapeHtml(opt.label) + "</div>";
      if (opt.description) {
        html += '<div class="option-desc">' + escapeHtml(opt.description) + "</div>";
      }
      html += "</div>";
    });
    html += "</div>";
  });
  return html;
}

function renderMessageContent(msg: TranscriptMessage): string {
  let content = "";
  (msg.blocks || []).forEach((b) => {
    if (b.type === "tool_use" && b.text === "AskUserQuestion" && b.input) {
      content += renderAskQuestion(b.input);
    } else if (b.type === "tool_use" && b.text === "Write" && b.input?.content) {
      content += '<div class="plan-content">' + renderMarkdown(b.input.content) + "</div>";
    } else if (b.type === "tool_use" && b.text === "ExitPlanMode") {
      content += '<div class="tool-use plan-approval">Plan ready for approval</div>';
    } else if (b.type === "tool_use") {
      const label = b.summary || b.text;
      content += '<div class="tool-use">' + escapeHtml(label) + "</div>";
    } else {
      content += renderMarkdown(b.text);
    }
  });
  return content;
}

function hasPlanApproval(messages: TranscriptMessage[]): boolean {
  // Walk backwards to find last assistant message
  for (let i = messages.length - 1; i >= 0; i--) {
    if (messages[i].role !== "assistant") continue;
    return (messages[i].blocks || []).some(
      (b) => b.type === "tool_use" && b.text === "ExitPlanMode",
    );
  }
  return false;
}

function showPlanButtons(): void {
  if (planButtonsShown) return;
  planButtonsShown = true;

  // Remove permission prompt buttons if present
  document.querySelector(".quick-buttons")?.remove();

  const inputGroup = document.querySelector(".respond-footer .input-group");
  if (!inputGroup) return;

  const div = document.createElement("div");
  div.className = "quick-buttons";
  div.innerHTML =
    '<button class="btn-plan-clear" data-send="1">Clear ctx & approve</button>' +
    '<button class="btn-plan-approve" data-send="2">Approve</button>' +
    '<button class="btn-plan-manual" data-send="3">Review edits</button>';

  inputGroup.before(div);

  div.querySelectorAll("[data-send]").forEach((btn) => {
    btn.addEventListener("click", () => send(btn.getAttribute("data-send")!));
  });
}

function loadTranscript(): void {
  fetch(apiBase + "/api/sessions/" + sessionId + "/transcript")
    .then((r) => r.json())
    .then((data: TranscriptData) => {
      const el = document.getElementById("conversation");
      if (!el) return;
      const messages = data.messages || [];
      if (messages.length === 0) return;

      // Compaction or reset: full re-render
      if (messages.length < renderedCount) {
        renderedCount = 0;
        el.innerHTML = "";
      }

      // Update last assistant message in-place (it accumulates blocks mid-turn)
      if (renderedCount > 0 && el.lastElementChild) {
        const lastMsg = messages[renderedCount - 1];
        if (lastMsg && lastMsg.role === "assistant") {
          el.lastElementChild.innerHTML = renderMessageContent(lastMsg);
        }
      }

      // Append new messages
      for (let i = renderedCount; i < messages.length; i++) {
        const msg = messages[i];
        const cls = msg.role === "user" ? "user" : "assistant";
        const div = document.createElement("div");
        div.className = "msg " + cls;
        div.innerHTML = renderMessageContent(msg);
        el.appendChild(div);
      }

      renderedCount = messages.length;
      el.scrollTop = el.scrollHeight;

      // Swap buttons for plan approval if detected
      if (hasPlanApproval(messages)) {
        showPlanButtons();
      }
    })
    .catch(() => {});
}

export function mount(params: Record<string, string>, sse: SSEManager): void {
  sessionId = params.id;
  document.body.dataset.page = "respond";

  // Fetch session data from API
  fetch(apiBase + "/api/sessions/" + sessionId)
    .then((r) => {
      if (!r.ok) throw new Error("not found");
      return r.json();
    })
    .then((sess: Session) => {
      const app = document.getElementById("app")!;
      const hasPerm = sess.notification_type === "permission_prompt";

      let html = '<div class="respond-view">';

      // Header
      html += '<div class="respond-header">';
      html += '<div class="respond-title">' + escapeHtml(sess.project) + "</div>";
      let meta = "Started " + timeAgo(sess.started_at);
      if (sess.node_name) meta += " \u00b7 " + escapeHtml(sess.node_name);
      html += '<div class="respond-meta">' + meta + "</div>";
      html += "</div>";

      // Conversation
      html += '<div id="conversation"></div>';

      // Footer
      html += '<div class="respond-footer">';

      if (sess.notify_message) {
        html += '<div class="context">' + escapeHtml(sess.notify_message) + "</div>";
      }

      html += '<div id="status" class="status"></div>';

      if (hasPerm) {
        html += '<div class="quick-buttons">';
        html += '<button class="btn-allow" data-send="y">Allow</button>';
        html += '<button class="btn-allow-all" data-send="a">Always</button>';
        html += '<button class="btn-deny" data-send="n">Deny</button>';
        html += "</div>";
      }

      html += '<div class="input-group">';
      html += '<input type="text" id="text" placeholder="Type a response...">';
      html += '<button id="send-btn">Send</button>';
      html += "</div>";

      html += "</div>"; // .respond-footer
      html += "</div>"; // .respond-view

      app.innerHTML = html;

      // Wire up event handlers
      const quickButtons = app.querySelectorAll("[data-send]");
      quickButtons.forEach((btn) => {
        btn.addEventListener("click", () => send(btn.getAttribute("data-send")!));
      });

      const sendBtn = document.getElementById("send-btn");
      sendBtn?.addEventListener("click", sendText);

      const textInput = document.getElementById("text") as HTMLInputElement | null;
      textInput?.addEventListener("keydown", (e) => {
        if (e.key === "Enter") sendText();
      });
      textInput?.focus();

      loadTranscript();
    })
    .catch(() => {
      const app = document.getElementById("app")!;
      app.innerHTML = '<div class="index-empty"><div class="index-empty-hint">Session not found</div></div>';
    });

  // Filter global SSE events to this session
  const debouncedLoad = debounce(loadTranscript, 500);
  const handleEvent = (e: MessageEvent) => {
    const evt: GlobalEvent = JSON.parse(e.data);
    if (evt.session_id !== sessionId) return;
    debouncedLoad();
  };

  unsubs.push(sse.on("notification", handleEvent));
  unsubs.push(sse.on("activity", handleEvent));
  unsubs.push(sse.on("response", handleEvent));
  unsubs.push(sse.on("tool_activity", handleEvent));
  unsubs.push(
    sse.on("session_end", (e: MessageEvent) => {
      const evt: GlobalEvent = JSON.parse(e.data);
      if (evt.session_id !== sessionId) return;
      showStatus("Session ended", true);
    }),
  );
}

export function unmount(): void {
  for (const u of unsubs) u();
  unsubs = [];
  sessionId = "";
  renderedCount = 0;
  planButtonsShown = false;
}
