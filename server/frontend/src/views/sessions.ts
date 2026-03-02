import { SSEManager } from "../sse";

export function mount(_params: Record<string, string>, _sse: SSEManager): void {
  document.body.dataset.page = "sessions";
  const app = document.getElementById("app")!;
  app.innerHTML =
    '<div class="index-empty">' +
    '<div class="index-empty-hint">Select a session to view details</div>' +
    "</div>";
}

export function unmount(): void {}
