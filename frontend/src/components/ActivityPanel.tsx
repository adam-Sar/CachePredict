import { useMemo } from "react";
import type { ActivityItem } from "../hooks/useEventStream";
import { probText } from "../lib/format";

interface ActivityPanelProps {
  items: ActivityItem[];
}

export function ActivityPanel({ items }: ActivityPanelProps) {
  const lastRequest = useMemo(() => {
    for (let i = items.length - 1; i >= 0; i--) {
      if (items[i].kind === "request") return items[i];
    }
    return null;
  }, [items]);

  const lastPrefetch = useMemo(() => {
    for (let i = items.length - 1; i >= 0; i--) {
      if (items[i].kind === "prefetch") return items[i];
    }
    return null;
  }, [items]);

  return (
    <aside className="border-l border-rule bg-paper/60 h-full overflow-y-auto flex flex-col">
      <div className="px-6 py-5 border-b border-rule">
        <div className="font-mono text-[10px] uppercase tracking-[0.2em] text-taupe">
          Live activity
        </div>
        <h3 className="font-display-roman text-[22px] leading-tight text-ink mt-1">
          What the oracle sees
        </h3>
        <p className="text-[12px] text-inkmute mt-2 leading-relaxed">
          Each request feeds the LSTM. Predicted next-calls are prefetched
          into Ristretto so the next click resolves from RAM.
        </p>
      </div>

      <div className="p-4 space-y-4">
        {lastRequest && <RequestCard item={lastRequest} />}
        {lastPrefetch && <PrefetchCard item={lastPrefetch} />}
        {!lastRequest && !lastPrefetch && (
          <div className="px-3 py-12 text-taupe text-center font-display-italic text-[15px]">
            No activity yet. Click a product.
          </div>
        )}
      </div>
    </aside>
  );
}

function timeOf(ts: number): string {
  const d = new Date(ts);
  return (
    String(d.getHours()).padStart(2, "0") +
    ":" +
    String(d.getMinutes()).padStart(2, "0") +
    ":" +
    String(d.getSeconds()).padStart(2, "0")
  );
}

function RequestCard({ item }: { item: ActivityItem }) {
  const req = item.request!;
  const isHit = req.cache_status === "HIT";
  const isMiss = req.cache_status === "MISS";
  const label = req.label ?? fallbackLabel(req);
  return (
    <div className="border border-rule bg-paper p-4">
      <div className="flex items-baseline justify-between gap-3">
        <div className="font-mono text-[10px] uppercase tracking-[0.18em] text-taupe">
          last request
        </div>
        <div className="font-mono text-[10px] uppercase tracking-[0.14em] text-taupe whitespace-nowrap shrink-0">
          {timeOf(item.ts)}
        </div>
      </div>
      <div className="font-display-roman text-[18px] text-ink leading-snug mt-2 break-words">
        {label}
      </div>
      {isHit && (
        <div className="mt-2 font-mono text-[11px] text-rust">⚡ served from cache</div>
      )}
      {isMiss && (
        <div className="mt-2 font-mono text-[11px] text-inkmute">
          ↳ fresh fetch
          {req.bytes != null ? ` · ${req.bytes}b` : ""}
          {req.ttl_ms ? ` · cached for ${Math.round(req.ttl_ms / 1000)}s` : ""}
        </div>
      )}
    </div>
  );
}

function PrefetchCard({ item }: { item: ActivityItem }) {
  const ev = item.prefetch!;
  const stored = ev.predictions.filter((p) => p.stored);
  const skipped = ev.predictions.filter((p) => !p.stored && p.call !== "END" && p.call !== "");
  return (
    <div className="border border-rust/40 bg-paper p-4">
      <div className="flex items-baseline justify-between gap-3">
        <div className="font-mono text-[10px] uppercase tracking-[0.18em] text-rust">
          oracle predicts next
        </div>
        <div className="font-mono text-[10px] uppercase tracking-[0.14em] text-taupe">
          {timeOf(item.ts)}
        </div>
      </div>

      {stored.length > 0 ? (
        <ol className="mt-3 space-y-2">
          {stored.slice(0, 3).map((p, i) => (
            <li key={i} className="flex items-baseline justify-between gap-3 text-[13px]">
              <span className="flex items-baseline gap-2 min-w-0 flex-1">
                <span className="font-mono text-[10px] text-taupe w-4 shrink-0">
                  {i + 1}.
                </span>
                <span className="font-display-roman text-ink break-words">
                  {p.resource || p.label || p.call}
                </span>
              </span>
              <span className="flex items-baseline gap-2 shrink-0">
                <span className="font-mono text-[10px] text-rust">{probText(p.prob)}</span>
                <span className="font-mono text-[10px] text-taupe">↳ cached</span>
              </span>
            </li>
          ))}
        </ol>
      ) : (
        <div className="mt-3 font-display-italic text-[13px] text-taupe">
          No prefetchable predictions.
        </div>
      )}

      {skipped.length > 0 && (
        <details className="mt-3 group">
          <summary className="font-mono text-[10px] uppercase tracking-[0.14em] text-taupe cursor-pointer hover:text-ink list-none">
            {skipped.length} skipped
          </summary>
          <ul className="mt-2 space-y-1 text-[11px] text-taupe font-mono break-words">
            {skipped.slice(0, 6).map((p, i) => (
              <li key={i}>
                {p.call}
                {p.reason ? <span className="ml-1 text-taupe/70">· {p.reason}</span> : null}
              </li>
            ))}
          </ul>
        </details>
      )}
    </div>
  );
}

function fallbackLabel(req: { method?: string; path?: string }): string {
  if (!req.method || !req.path) return "request";
  return `${req.method} ${req.path}`;
}