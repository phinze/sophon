import { marked } from "marked";

interface SophonConfig {
  apiBase: string;
  sessionId: string;
}

interface AskQuestionOption {
  label: string;
  description?: string;
}

interface AskQuestion {
  header?: string;
  question: string;
  options?: AskQuestionOption[];
}

interface AskQuestionInput {
  questions?: AskQuestion[];
}

interface TranscriptBlock {
  type: string;
  text: string;
  input?: AskQuestionInput;
}

interface TranscriptMessage {
  role: string;
  blocks?: TranscriptBlock[];
}

interface TranscriptData {
  messages?: TranscriptMessage[];
}

declare global {
  interface Window {
    __SOPHON__: SophonConfig;
    send: (text: string) => void;
    sendText: () => void;
  }
}

const config = window.__SOPHON__;
const apiBase = config.apiBase;
const sessionId = config.sessionId;

function showStatus(msg: string, ok: boolean): void {
  const el = document.getElementById("status")!;
  el.textContent = msg;
  el.className = "status " + (ok ? "ok" : "err");
  if (ok) setTimeout(() => { el.className = "status"; }, 3000);
}

function send(text: string): void {
  fetch(apiBase + "/api/respond/" + sessionId, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ text: text }),
  })
    .then((r) => {
      if (r.ok) showStatus("Sent: " + text, true);
      else r.text().then((t) => showStatus("Error: " + t, false));
    })
    .catch((e) => showStatus("Network error: " + e, false));
}

function sendText(): void {
  const input = document.getElementById("text") as HTMLInputElement;
  const text = input.value.trim();
  if (!text) return;
  send(text);
  input.value = "";
}

function escapeHtml(s: string): string {
  const d = document.createElement("div");
  d.textContent = s;
  return d.innerHTML;
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
      const el = document.getElementById("conversation")!;
      const messages = data.messages || [];
      if (messages.length === 0) return;

      let html = "";
      messages.forEach((msg) => {
        const cls = msg.role === "user" ? "user" : "assistant";
        let content = "";
        (msg.blocks || []).forEach((b) => {
          if (b.type === "tool_use" && b.text === "AskUserQuestion" && b.input) {
            content += renderAskQuestion(b.input);
          } else if (b.type === "tool_use") {
            content += '<div class="tool-use">' + escapeHtml(b.text) + "</div>";
          } else {
            content += renderMarkdown(b.text);
          }
        });
        html += '<div class="msg ' + cls + '">' + content + "</div>";
      });
      el.innerHTML = html;
      el.scrollTop = el.scrollHeight;
    })
    .catch(() => {}); // graceful failure
}

// Expose for inline onclick/onkeydown handlers in template
window.send = send;
window.sendText = sendText;

loadTranscript();
