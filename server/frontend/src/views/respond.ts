import { marked } from "marked";
import {
  Session,
  GlobalEvent,
  AskQuestionInput,
  TranscriptData,
} from "../types";
import { escapeHtml } from "../util";
import { SSEManager } from "../sse";

const apiBase = "";
let unsubs: (() => void)[] = [];
let sessionId = "";

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
      if (r.ok) showStatus("Sent: " + text, true);
      else r.text().then((t) => showStatus("Error: " + t, false));
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

function loadTranscript(): void {
  fetch(apiBase + "/api/sessions/" + sessionId + "/transcript")
    .then((r) => r.json())
    .then((data: TranscriptData) => {
      const el = document.getElementById("conversation");
      if (!el) return;
      const messages = data.messages || [];
      if (messages.length === 0) return;

      let html = "";
      messages.forEach((msg) => {
        const cls = msg.role === "user" ? "user" : "assistant";
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
        html += '<div class="msg ' + cls + '">' + content + "</div>";
      });
      el.innerHTML = html;
      el.scrollTop = el.scrollHeight;
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

      let html = '<a href="/" class="back-link">&larr; Sessions</a>';
      html += '<div class="project">' + escapeHtml(sess.project) + "</div>";
      html += '<div id="conversation"></div>';

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
      html += "<button id=\"send-btn\">Send</button>";
      html += "</div>";

      html += '<div class="meta">Session ' + escapeHtml(sessionId) + "</div>";

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
      app.innerHTML = '<div class="empty">Session not found</div>';
    });

  // Filter global SSE events to this session
  const handleEvent = (e: MessageEvent) => {
    const evt: GlobalEvent = JSON.parse(e.data);
    if (evt.session_id !== sessionId) return;
    loadTranscript();
  };

  unsubs.push(sse.on("notification", handleEvent));
  unsubs.push(sse.on("activity", handleEvent));
  unsubs.push(sse.on("response", handleEvent));
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
}
