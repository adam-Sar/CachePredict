export function methodPathFromCall(call: string): { method: string; path: string; query: string } {
  const space = call.indexOf(" ");
  if (space < 0) return { method: call, path: "", query: "" };
  const method = call.slice(0, space);
  const rest = call.slice(space + 1);
  const q = rest.indexOf("?");
  if (q < 0) return { method, path: rest, query: "" };
  return { method, path: rest.slice(0, q), query: rest.slice(q + 1) };
}

export function shortKey(key: string, n = 12): string {
  return key ? key.slice(0, n) : "";
}

export function relativeTime(ts: number, now: number): string {
  const ms = Math.max(0, now - ts);
  if (ms < 1000) return "just now";
  if (ms < 60_000) return `${Math.floor(ms / 1000)}s ago`;
  return `${Math.floor(ms / 60_000)}m ago`;
}

export function probText(p: number): string {
  return (p * 100).toFixed(1) + "%";
}