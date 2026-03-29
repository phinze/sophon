import { Session, SessionsResponse, GlobalEvent } from "../types";
import { escapeHtml, timeAgo, debounce } from "../util";
import { SSEManager } from "../sse";

let selectedSessionId = "";
let recentCollapsed = true;
const lastToolActivity: Map<string, number> = new Map();
const WORKING_THRESHOLD_MS = 60_000;

function isWorking(sessionId: string): boolean {
  const last = lastToolActivity.get(sessionId);
  if (!last) return false;
  return Date.now() - last < WORKING_THRESHOLD_MS;
}

function renderSidebarCard(s: Session, isActive: boolean): string {
  const isOffline = isActive && s.agent_online === false;
  const hasNotification = isActive && !isOffline && !!s.notify_message;
  const dotClass = !isActive
    ? "dot-stopped"
    : isOffline
      ? "dot-offline"
      : hasNotification
        ? "dot-waiting"
        : isWorking(s.session_id)
          ? "dot-active"
          : "dot-idle";
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
        // Most recent activity first
        const aTime = a.last_activity_at || a.started_at || "";
        const bTime = b.last_activity_at || b.started_at || "";
        return bTime.localeCompare(aTime);
      });

      if (active.length > 0) {
        html += '<div class="sb-section">Active</div>';
        html += active.map((s) => renderSidebarCard(s, true)).join("");
      }

      const recent = data.recent || [];
      if (recent.length > 0) {
        const chevron = recentCollapsed ? "\u25b8" : "\u25be";
        html +=
          '<div class="sb-section sb-section-toggle" id="sb-recent-toggle">' +
          chevron + " Recent (" + recent.length + ")</div>";
        if (!recentCollapsed) {
          html += recent.map((s) => renderSidebarCard(s, false)).join("");
        }
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

  // Toggle recent section (delegated since content re-renders)
  document.getElementById("sb-sessions")!.addEventListener("click", (e) => {
    if ((e.target as HTMLElement).id === "sb-recent-toggle") {
      recentCollapsed = !recentCollapsed;
      refreshSessions();
    }
  });

  const debouncedRefresh = debounce(refreshSessions, 1000);
  sse.on("notification", () => refreshSessions());
  sse.on("session_start", () => refreshSessions());
  sse.on("session_end", () => refreshSessions());
  sse.on("activity", () => refreshSessions());
  sse.on("response", () => refreshSessions());
  let idleTimer: ReturnType<typeof setTimeout> | null = null;
  sse.on("tool_activity", (e: MessageEvent) => {
    try {
      const data = JSON.parse(e.data) as GlobalEvent;
      if (data.session_id) {
        lastToolActivity.set(data.session_id, Date.now());
      }
    } catch { /* ignore parse errors */ }
    debouncedRefresh();
    // Schedule a refresh after the working threshold to transition dot to idle
    if (idleTimer) clearTimeout(idleTimer);
    idleTimer = setTimeout(refreshSessions, WORKING_THRESHOLD_MS + 1000);
  });
}
