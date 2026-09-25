import { useCallback, useMemo, useState } from "react";
import { ActivityPanel } from "./components/ActivityPanel";
import { CacheBanner, buildEntry } from "./components/CacheBanner";
import { Header } from "./components/Header";
import { ProductsView } from "./components/ProductsView";
import { useEventStream } from "./hooks/useEventStream";
import type { CachedEntry } from "./components/CacheBanner";
import type { RequestEvent } from "./lib/types";

export default function App() {
  const { connected, items, requests } = useEventStream();
  const [cachedEntries, setCachedEntries] = useState<CachedEntry[]>([]);

  const onPrefetch = useCallback((e: import("./lib/types").PrefetchEvent) => {
    const next = buildEntry(e);
    if (next.length === 0) return;
    setCachedEntries((prev) => {
      const merged = [...prev];
      for (const n of next) {
        if (!merged.some((m) => m.call === n.call)) merged.push(n);
      }
      return merged.slice(-12);
    });
  }, []);

  const { hitCount, missCount } = useMemo(() => countStats(requests), [requests]);
  const sessionShort = useMemo(() => sessionFromCookie(), []);

  return (
    <div className="min-h-full flex flex-col">
      <Header
        connected={connected}
        sessionShort={sessionShort}
        requestCount={requests.length}
        hitCount={hitCount}
        missCount={missCount}
      />

      <CacheBanner
        entries={cachedEntries}
        onExpire={(entry) => {
          setCachedEntries((prev) => prev.filter((e) => e.call !== entry.call));
        }}
      />

      <main className="flex-1 grid grid-cols-1 lg:grid-cols-[1fr_360px]">
        <section className="mx-auto max-w-[1100px] w-full px-6 lg:px-10 py-10">
          <ProductsView />
        </section>
        <ActivityPanel items={items} now={Date.now()} />
      </main>

      <footer className="border-t border-rule">
        <div className="mx-auto max-w-[1400px] px-6 lg:px-10 py-4 flex items-center justify-between font-mono text-[11px] text-taupe">
          <span>
            LSTM · ONNX · Ristretto · <span className="text-ink">cachepredict</span>
          </span>
          <span>
            proxy → <span className="text-ink">localhost:1234</span>
          </span>
        </div>
      </footer>
    </div>
  );
}

function countStats(reqs: RequestEvent[]) {
  let hit = 0;
  let miss = 0;
  for (const r of reqs) {
    if (r.cache_status === "HIT") hit++;
    else if (r.cache_status === "MISS") miss++;
  }
  return { hitCount: hit, missCount: miss };
}

function sessionFromCookie(): string {
  const m = document.cookie.match(/(?:^|;\s*)session_id=([^;]+)/);
  return m ? m[1].slice(0, 8) : "";
}