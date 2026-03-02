import { Session, SessionsResponse, GlobalEvent } from "../types";
import { escapeHtml, timeAgo, debounce } from "../util";
import { SSEManager } from "../sse";

let selectedSessionId = "";

function renderSidebarCard(s: Session, isActive: boolean): string {
  const isOffline = isActive && s.agent_online === false;
  const hasNotification = isActive && !isOffline && !!s.notify_message;
  const dotClass = !isActive
    ? "dot-stopped"
    : isOffline
      ? "dot-offline"
      : hasNotification
        ? "dot-waiting"
        : "dot-active";
  const selected = s.session_id === selectedSessionId ? " selected" : "";
  const clickable = isActive && !isOffline;

  const tag = clickable ? "a" : "div";
  let html = "<" + tag + ' class="sb-card' + selected + '"';
  if (clickable) html += ' href="/respond/' + escapeHtml(s.session_id) + '"';
  html += ">";

  html += '<div class="sb-card-header">';
  html += '<span class="dot ' + dotClass + '"></span>';
  html += '<span class="sb-project">' + escapeHtml(s.project) + "</span>";
  if (s.node_name) html += '<span class="sb-node">' + escapeHtml(s.node_name) + "</span>";
  html += "</div>";

  // Detail line: summary or notification for active, timestamp for recent
  if (isActive && !isOffline) {
    const detail = s.notify_message || s.plan_summary || s.topic;
    if (detail) {
      html +=
        '<div class="sb-detail' +
        (hasNotification ? " sb-detail-notify" : "") +
        '">' +
        escapeHtml(detail) +
        "</div>";
    }
  } else if (!isActive) {
    html += '<div class="sb-detail">Stopped ' + timeAgo(s.stopped_at || "") + "</div>";
  } else {
    // offline
    html += '<div class="sb-detail sb-detail-offline">Agent offline</div>';
  }

  html += "</" + tag + ">";
  return html;
}

function refreshSessions(): void {
  fetch("/api/sessions")
    .then((r) => r.json())
    .then((data: SessionsResponse) => {
      const el = document.getElementById("sb-sessions");
      if (!el) return;

      let html = "";
      const active = (data.active || []).sort((a, b) => {
        // Notifications first, then online, then offline
        const aWeight = a.notify_message ? 0 : a.agent_online !== false ? 1 : 2;
        const bWeight = b.notify_message ? 0 : b.agent_online !== false ? 1 : 2;
        return aWeight - bWeight;
      });

      if (active.length > 0) {
        html += '<div class="sb-section">Active</div>';
        html += active.map((s) => renderSidebarCard(s, true)).join("");
      }

      const recent = data.recent || [];
      if (recent.length > 0) {
        html += '<div class="sb-section">Recent</div>';
        html += recent.map((s) => renderSidebarCard(s, false)).join("");
      }

      if (active.length === 0 && recent.length === 0) {
        html = '<div class="sb-empty">No sessions</div>';
      }

      el.innerHTML = html;
    })
    .catch(() => {});
}

export function setSelected(id: string): void {
  selectedSessionId = id;
  document.querySelectorAll(".sb-card").forEach((card) => {
    card.classList.remove("selected");
  });
  if (id) {
    const link = document.querySelector('a.sb-card[href="/respond/' + id + '"]');
    link?.classList.add("selected");
  }
}

export function mount(sse: SSEManager): void {
  const sidebar = document.getElementById("sidebar")!;
  sidebar.innerHTML =
    '<div class="sb-header"><span class="sb-title">sophon</span></div>' +
    '<div id="notif-pill-slot"></div>' +
    '<div class="sb-scroll" id="sb-sessions">' +
    '<div class="sb-empty">Loading\u2026</div>' +
    "</div>";

  refreshSessions();

  const debouncedRefresh = debounce(refreshSessions, 1000);
  sse.on("notification", () => refreshSessions());
  sse.on("session_start", () => refreshSessions());
  sse.on("session_end", () => refreshSessions());
  sse.on("activity", () => refreshSessions());
  sse.on("response", () => refreshSessions());
  sse.on("tool_activity", () => debouncedRefresh());
}
