import { useEffect, useRef, useState } from "react";
import type { LiveEvent, PrefetchEvent, RequestEvent } from "../lib/types";

const POLL_MS = 1000;
const MAX_LOG = 200;

export interface ActivityItem {
  id: number;
  ts: number;
  kind: "request" | "prefetch";
  request?: RequestEvent;
  prefetch?: PrefetchEvent;
}

interface RecentResponse {
  events: LiveEvent[];
}

export function useEventStream(onPrefetch?: (e: PrefetchEvent) => void): {
  connected: boolean;
  items: ActivityItem[];
  requests: RequestEvent[];
} {
  const [connected, setConnected] = useState(false);
  const [items, setItems] = useState<ActivityItem[]>([]);
  const [requests, setRequests] = useState<RequestEvent[]>([]);
  const seq = useRef(0);
  const seenIds = useRef<Set<number>>(new Set());
  const prefetchRef = useRef(onPrefetch);
  prefetchRef.current = onPrefetch;

  useEffect(() => {
    let alive = true;
    const seen = seenIds.current;

    async function poll() {
      try {
        const res = await fetch("/api/activity", { credentials: "include" });
        if (!alive) return;
        if (!res.ok) {
          setConnected(false);
          return;
        }
        const data = (await res.json()) as RecentResponse;
        setConnected(true);

        const newRequests: RequestEvent[] = [];
        const newItems: ActivityItem[] = [];

        for (const ev of data.events) {
          if (ev.type === "request") {
            const r = ev;
            const id = hashId(r);
            if (seen.has(id)) continue;
            seen.add(id);
            seq.current += 1;
            newRequests.push(r);
            newItems.push({ id: seq.current, ts: nowApprox(ev), kind: "request", request: r });
          } else if (ev.type === "prefetch") {
            const p = ev;
            const id = hashId(p);
            if (seen.has(id)) continue;
            seen.add(id);
            seq.current += 1;
            newItems.push({ id: seq.current, ts: nowApprox(p), kind: "prefetch", prefetch: p });
            prefetchRef.current?.(p);
          }
        }

        if (newItems.length > 0) {
          setItems((prev) => [...prev, ...newItems].slice(-MAX_LOG));
          setRequests((prev) => [...newRequests, ...prev].slice(0, MAX_LOG));
        }
      } catch {
        setConnected(false);
      }
    }

    poll();
    const t = setInterval(poll, POLL_MS);
    return () => {
      alive = false;
      clearInterval(t);
    };
  }, []);

  return { connected, items, requests };
}

function hashId(e: LiveEvent): number {
  if (e.type === "request") {
    return hash(`${e.method}|${e.path}|${e.cache_status ?? ""}|${e.bytes ?? 0}`);
  }
  return hash(
    e.predictions
      .map((p) => `${p.call}:${p.stored ? 1 : 0}`)
      .join(",")
  );
}

function hash(s: string): number {
  let h = 0;
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) | 0;
  return h;
}

function nowApprox(_e: LiveEvent): number {
  return Date.now();
}