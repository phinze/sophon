import { Session, SessionsResponse, GlobalEvent } from "../types";
import { escapeHtml, timeAgo, debounce } from "../util";
import { SSEManager } from "../sse";

const apiBase = "";
let unsubs: (() => void)[] = [];

function renderActiveCard(s: Session): string {
  const isOffline = s.agent_online === false;
  const dotClass = isOffline ? "dot-offline" : s.notify_message ? "dot-waiting" : "dot-active";
  let detail = "Started " + timeAgo(s.started_at);
  if (s.last_activity_at) detail += " \u00b7 active " + timeAgo(s.last_activity_at);
  if (isOffline) {
    detail += " \u00b7 agent offline";
  } else if (s.notify_message && s.notified_at) {
    detail += " \u00b7 notified " + timeAgo(s.notified_at);
  }

  const tag = isOffline ? "div" : "a";
  let html = "<" + tag + ' class="card"';
  if (!isOffline) html += ' href="/respond/' + escapeHtml(s.session_id) + '"';
  html += ">";
  html += '<div class="card-header">';
  html += '<span class="dot ' + dotClass + '"></span>';
  html += '<span class="project-name">' + escapeHtml(s.project) + "</span>";
  if (s.node_name) html += '<span class="node-name">' + escapeHtml(s.node_name) + "</span>";
  html += "</div>";
  html += '<div class="card-detail">' + escapeHtml(detail) + "</div>";
  if (s.notify_message && !isOffline) {
    html += '<div class="card-message">' + escapeHtml(s.notify_message) + "</div>";
  }
  html += "</" + tag + ">";
  return html;
}

function renderRecentCard(s: Session): string {
  let html = '<div class="card">';
  html += '<div class="card-header">';
  html += '<span class="dot dot-stopped"></span>';
  html += '<span class="project-name">' + escapeHtml(s.project) + "</span>";
  if (s.node_name) html += '<span class="node-name">' + escapeHtml(s.node_name) + "</span>";
  html += "</div>";
  html += '<div class="card-detail">Stopped ' + timeAgo(s.stopped_at || "") + "</div>";
  html += "</div>";
  return html;
}

function refreshSessions(): void {
  fetch(apiBase + "/api/sessions")
    .then((r) => r.json())
    .then((data: SessionsResponse) => {
      const activeEl = document.getElementById("active-sessions");
      const recentEl = document.getElementById("recent-sessions");
      if (!activeEl || !recentEl) return;

      const active = (data.active || []).sort((a, b) => {
        const aOnline = a.agent_online !== false ? 0 : 1;
        const bOnline = b.agent_online !== false ? 0 : 1;
        return aOnline - bOnline;
      });
      if (active.length > 0) {
        activeEl.innerHTML = active.map(renderActiveCard).join("");
      } else {
        activeEl.innerHTML = '<div class="empty">No active sessions</div>';
      }

      const recent = data.recent || [];
      if (recent.length > 0) {
        recentEl.innerHTML = recent.map(renderRecentCard).join("");
      } else {
        recentEl.innerHTML = '<div class="empty">No recent sessions</div>';
      }
    })
    .catch(() => {});
}

export function mount(_params: Record<string, string>, sse: SSEManager): void {
  document.body.dataset.page = "sessions";
  const app = document.getElementById("app")!;
  app.innerHTML = `
    <h1>sophon</h1>
    <h2>Active Sessions</h2>
    <div id="active-sessions"><div class="empty">Loading...</div></div>
    <h2>Recent Sessions</h2>
    <div id="recent-sessions"><div class="empty">Loading...</div></div>
  `;

  refreshSessions();

  const debouncedRefresh = debounce(refreshSessions, 1000);

  unsubs.push(sse.on("notification", () => refreshSessions()));
  unsubs.push(sse.on("session_start", () => refreshSessions()));
  unsubs.push(sse.on("session_end", () => refreshSessions()));
  unsubs.push(sse.on("activity", () => refreshSessions()));
  unsubs.push(sse.on("response", () => refreshSessions()));
  unsubs.push(sse.on("tool_activity", () => debouncedRefresh()));
}

export function unmount(): void {
  for (const u of unsubs) u();
  unsubs = [];
}
