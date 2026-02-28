type Handler = (e: MessageEvent) => void;

export class SSEManager {
  private source: EventSource | null = null;
  private listeners: Map<string, Set<Handler>> = new Map();
  private url: string;

  constructor(url: string) {
    this.url = url;
  }

  connect(): void {
    if (this.source) return;
    this.source = new EventSource(this.url);

    // Re-register all existing event types on new connection
    for (const eventType of this.listeners.keys()) {
      this.source.addEventListener(eventType, (e: MessageEvent) => {
        const handlers = this.listeners.get(eventType);
        if (handlers) {
          for (const h of handlers) h(e);
        }
      });
    }

    window.addEventListener("beforeunload", () => {
      this.source?.close();
    });
  }

  on(eventType: string, handler: Handler): () => void {
    let set = this.listeners.get(eventType);
    if (!set) {
      set = new Set();
      this.listeners.set(eventType, set);
      // Register on existing EventSource if already connected
      this.source?.addEventListener(eventType, (e: MessageEvent) => {
        const handlers = this.listeners.get(eventType);
        if (handlers) {
          for (const h of handlers) h(e);
        }
      });
    }
    set.add(handler);
    return () => { set!.delete(handler); };
  }
}
