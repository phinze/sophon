export function escapeHtml(s: string): string {
  const d = document.createElement("div");
  d.textContent = s;
  return d.innerHTML;
}

export function debounce(fn: () => void, delayMs: number): () => void {
  let timer: ReturnType<typeof setTimeout> | undefined;
  return () => {
    clearTimeout(timer);
    timer = setTimeout(fn, delayMs);
  };
}

export function timeAgo(iso: string): string {
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
