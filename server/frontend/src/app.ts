import { SSEManager } from "./sse";
import { GlobalEvent, NotificationEventData } from "./types";
import * as router from "./router";
import * as sessionsView from "./views/sessions";
import * as respondView from "./views/respond";

const sse = new SSEManager("/api/events");

// Web notifications — global, independent of current view
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

// Routes
router.add("/", (params) => sessionsView.mount(params, sse), sessionsView.unmount);
router.add("/respond/:id", (params) => respondView.mount(params, sse), respondView.unmount);

// Boot
requestNotificationPermission();
sse.connect();
router.start();
