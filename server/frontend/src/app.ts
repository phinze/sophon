import { SSEManager } from "./sse";
import { GlobalEvent, NotificationEventData } from "./types";
import * as router from "./router";
import * as sidebar from "./views/sidebar";
import * as sessionsView from "./views/sessions";
import * as respondView from "./views/respond";

const sse = new SSEManager("/api/events");

// Web notifications — permission pill
function showNotificationPill(): void {
  if (!("Notification" in window)) return;
  if (Notification.permission === "granted") return;

  const slot = document.getElementById("notif-pill-slot");
  if (!slot) return;

  const pill = document.createElement("div");
  pill.id = "notif-pill";
  pill.className = "notif-pill";

  if (Notification.permission === "default") {
    pill.textContent = "Enable notifications";
    pill.addEventListener("click", async () => {
      const result = await Notification.requestPermission();
      if (result === "granted") {
        pill.remove();
      } else {
        pill.textContent = "Notifications blocked — check browser settings";
        pill.classList.add("notif-pill-denied");
        pill.style.cursor = "default";
      }
    });
  } else {
    // denied
    pill.textContent = "Notifications blocked — check browser settings";
    pill.classList.add("notif-pill-denied");
    pill.style.cursor = "default";
  }

  slot.appendChild(pill);
}

function showWebNotification(sessionId: string, data: NotificationEventData): void {
  if (!("Notification" in window)) return;
  if (Notification.permission !== "granted") return;

  const title = data.title || "sophon";
  const body = data.message || "";
  const notification = new Notification(title, { body, tag: "sophon-" + sessionId });

  notification.onclick = () => {
    window.focus();
    router.navigate("/respond/" + sessionId);
    notification.close();
  };
}

sse.on("notification", (e: MessageEvent) => {
  const evt: GlobalEvent = JSON.parse(e.data);
  const data = (evt.data || {}) as NotificationEventData;
  showWebNotification(evt.session_id, data);
});

// Sync sidebar selection with route changes
router.onNavigate((path) => {
  const match = path.match(/^\/respond\/(.+)$/);
  sidebar.setSelected(match ? match[1] : "");
});

// Routes (main content area only)
router.add("/", (params) => sessionsView.mount(params, sse), sessionsView.unmount);
router.add("/respond/:id", (params) => respondView.mount(params, sse), respondView.unmount);

// Boot
sidebar.mount(sse);
showNotificationPill();
sse.connect();
router.start();
