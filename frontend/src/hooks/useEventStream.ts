import { useEffect, useRef, useState } from "react";
import type { LiveEvent, PrefetchEvent, RequestEvent } from "../lib/types";

const MAX_LOG = 200;

export interface ActivityItem {
  id: number;
  ts: number;
  kind: "request" | "prefetch";
  request?: RequestEvent;
  prefetch?: PrefetchEvent;
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
  const prefetchRef = useRef(onPrefetch);
  prefetchRef.current = onPrefetch;

  useEffect(() => {
    const es = new EventSource("/api/events", { withCredentials: true });

    const handle = (kind: ActivityItem["kind"]) => (ev: MessageEvent) => {
      try {
        const data = JSON.parse(ev.data) as LiveEvent;
        seq.current += 1;
        const id = seq.current;
        if (kind === "request") {
          const r = data as RequestEvent;
          setRequests((prev) => [r, ...prev].slice(0, MAX_LOG));
          setItems((prev) => [...prev, { id, ts: Date.now(), kind, request: r }].slice(-MAX_LOG));
        } else {
          const p = data as PrefetchEvent;
          setItems((prev) => [...prev, { id, ts: Date.now(), kind, prefetch: p }].slice(-MAX_LOG));
          prefetchRef.current?.(p);
        }
      } catch {
        // ignore malformed events
      }
    };

    const onOpen = () => setConnected(true);
    const onError = () => setConnected(false);

    es.addEventListener("request", handle("request") as EventListener);
    es.addEventListener("prefetch", handle("prefetch") as EventListener);
    es.addEventListener("open", onOpen);
    es.addEventListener("error", onError);

    return () => {
      es.close();
    };
  }, []);

  return { connected, items, requests };
}