interface SophonConfig {
  apiBase: string;
}

interface Session {
  session_id: string;
  project: string;
  node_name?: string;
  started_at: string;
  stopped_at?: string;
  last_activity_at?: string;
  notify_message?: string;
  notified_at?: string;
}

interface SessionsResponse {
  active: Session[] | null;
  recent: Session[] | null;
}

interface GlobalEvent {
  type: string;
  session_id: string;
  data?: unknown;
}

interface NotificationEventData {
  type?: string;
  message?: string;
  title?: string;
}

declare global {
  interface Window {
    __SOPHON__: SophonConfig;
  }
}

const config = window.__SOPHON__;
const apiBase = config.apiBase;

function escapeHtml(s: string): string {
  const d = document.createElement("div");
  d.textContent = s;
  return d.innerHTML;
}

function timeAgo(iso: string): string {
  if (!iso) return "just now";
  const d = Date.now() - new Date(iso).getTime();
  if (d < 60_000) return "just now";
  if (d < 3_600_000) {
    const m = Math.floor(d / 60_000);
    return m === 1 ? "1m ago" : m + "m ago";
  }
  const h = Math.floor(d / 3_600_000);
  return h === 1 ? "1h ago" : h + "h ago";
}

function renderActiveCard(s: Session): string {
  const dotClass = s.notify_message ? "dot-waiting" : "dot-active";
  let detail = "Started " + timeAgo(s.started_at);
  if (s.last_activity_at) detail += " \u00b7 active " + timeAgo(s.last_activity_at);
  if (s.notify_message && s.notified_at) detail += " \u00b7 notified " + timeAgo(s.notified_at);

  let html = '<a class="card" href="/respond/' + escapeHtml(s.session_id) + '">';
  html += '<div class="card-header">';
  html += '<span class="dot ' + dotClass + '"></span>';
  html += '<span class="project-name">' + escapeHtml(s.project) + "</span>";
  if (s.node_name) html += '<span class="node-name">' + escapeHtml(s.node_name) + "</span>";
  html += "</div>";
  html += '<div class="card-detail">' + escapeHtml(detail) + "</div>";
  if (s.notify_message) {
    html += '<div class="card-message">' + escapeHtml(s.notify_message) + "</div>";
  }
  html += "</a>";
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
      const activeEl = document.getElementById("active-sessions")!;
      const recentEl = document.getElementById("recent-sessions")!;

      const active = data.active || [];
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
    .catch(() => {}); // graceful failure
}

function requestNotificationPermission(): void {
  if (!("Notification" in window)) return;
  if (Notification.permission === "default") {
    Notification.requestPermission();
  }
}

function showWebNotification(sessionId: string, data: NotificationEventData): void {
  if (!("Notification" in window)) return;
  if (Notification.permission !== "granted") return;

  const title = data.title || "sophon";
  const body = data.message || "";
  const notification = new Notification(title, { body, tag: "sophon-" + sessionId });

  notification.onclick = () => {
    window.open("/respond/" + sessionId, "_blank");
    notification.close();
  };
}

function connectSSE(): void {
  const evtSource = new EventSource(apiBase + "/api/events");

  evtSource.addEventListener("notification", (e: MessageEvent) => {
    const evt: GlobalEvent = JSON.parse(e.data);
    const data = (evt.data || {}) as NotificationEventData;
    showWebNotification(evt.session_id, data);
    refreshSessions();
  });

  evtSource.addEventListener("session_start", () => {
    refreshSessions();
  });

  evtSource.addEventListener("session_end", () => {
    refreshSessions();
  });

  evtSource.addEventListener("activity", () => {
    refreshSessions();
  });
}

requestNotificationPermission();
connectSSE();
