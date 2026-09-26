import { useEffect, useRef, useState } from "react";
import type { LiveEvent, PrefetchEvent, RequestEvent } from "../lib/types";

const POLL_MS = 600;
const MAX_LOG = 80;

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
  const seenSeq = useRef<Set<number>>(new Set());
  const prefetchRef = useRef(onPrefetch);
  prefetchRef.current = onPrefetch;

  useEffect(() => {
    let alive = true;
    const seen = seenSeq.current;

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
          if (typeof ev.seq !== "number") continue;
          if (seen.has(ev.seq)) continue;
          seen.add(ev.seq);
          seq.current += 1;
          const localId = seq.current;

          if (ev.type === "request") {
            const r = ev as RequestEvent;
            newRequests.push(r);
            newItems.push({ id: localId, ts: Date.now(), kind: "request", request: r });
          } else if (ev.type === "prefetch") {
            const p = ev as PrefetchEvent;
            newItems.push({ id: localId, ts: Date.now(), kind: "prefetch", prefetch: p });
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