import { useMemo, useState } from "react";
import type { ActivityItem } from "../hooks/useEventStream";
import { probText, relativeTime } from "../lib/format";

interface ActivityPanelProps {
  items: ActivityItem[];
  now: number;
}

export function ActivityPanel({ items, now }: ActivityPanelProps) {
  const ordered = useMemo(() => [...items].reverse(), [items]);
  return (
    <aside className="border-l border-rule bg-paper/60 h-full overflow-hidden flex flex-col">
      <div className="px-6 py-5 border-b border-rule">
        <div className="font-mono text-[10px] uppercase tracking-[0.2em] text-taupe">
          Live activity
        </div>
        <h3 className="font-display-roman text-[22px] leading-tight text-ink mt-1">
          What the oracle sees
        </h3>
        <p className="text-[12px] text-inkmute mt-2 leading-relaxed">
          Every request feeds the LSTM. Predicted next-calls are prefetched
          into Ristretto so the next click resolves from RAM.
        </p>
      </div>

      <div className="flex-1 overflow-y-auto px-4 py-4">
        {ordered.length === 0 && (
          <div className="px-3 py-12 text-taupe text-center font-display-italic text-[15px]">
            No activity yet. Click a product.
          </div>
        )}
        <ol className="space-y-3">
          {ordered.map((it) => (
            <li key={it.id}>
              {it.kind === "request" && it.request && <RequestCard req={it.request} ts={it.ts} now={now} />}
              {it.kind === "prefetch" && it.prefetch && <PrefetchCard ev={it.prefetch} ts={it.ts} now={now} />}
            </li>
          ))}
        </ol>
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

function RequestCard({
  req,
  ts,
  now,
}: {
  req: import("../lib/types").RequestEvent;
  ts: number;
  now: number;
}) {
  const isHit = req.cache_status === "HIT";
  const isMiss = req.cache_status === "MISS";
  const label = req.label ?? fallbackLabel(req);
  return (
    <div className="border border-rule bg-paper p-3.5">
      <div className="flex items-baseline justify-between gap-3">
        <div className="font-display-roman text-[15px] text-ink leading-snug truncate">
          {label}
        </div>
        <div className="font-mono text-[10px] uppercase tracking-[0.14em] text-taupe whitespace-nowrap shrink-0">
          {timeOf(ts)}
        </div>
      </div>
      {isHit && (
        <div className="mt-1.5 font-mono text-[11px] text-rust">⚡ served from cache</div>
      )}
      {isMiss && (
        <div className="mt-1.5 font-mono text-[11px] text-inkmute">
          ↳ fresh fetch · {req.bytes != null ? `${req.bytes}b` : ""}
          {req.ttl_ms ? ` · cached for ${Math.round(req.ttl_ms / 1000)}s` : ""}
        </div>
      )}
    </div>
  );
}

function PrefetchCard({
  ev,
  ts,
  now,
}: {
  ev: import("../lib/types").PrefetchEvent;
  ts: number;
  now: number;
}) {
  const stored = ev.predictions.filter((p) => p.stored);
  const skipped = ev.predictions.filter((p) => !p.stored && p.call !== "END" && p.call !== "");
  return (
    <div className="border border-rust/40 bg-paper p-3.5">
      <div className="flex items-baseline justify-between gap-3">
        <div className="font-mono text-[10px] uppercase tracking-[0.18em] text-rust">
          oracle predicts next
        </div>
        <div className="font-mono text-[10px] uppercase tracking-[0.14em] text-taupe">
          {timeOf(ts)}
        </div>
      </div>

      {stored.length > 0 && (
        <ol className="mt-2.5 space-y-1.5">
          {stored.slice(0, 5).map((p, i) => (
            <li key={i} className="flex items-baseline justify-between gap-3 text-[13px]">
              <span className="flex items-baseline gap-2 min-w-0">
                <span className="font-mono text-[10px] text-taupe w-4 shrink-0">
                  {i + 1}.
                </span>
                <span className="font-display-roman text-ink truncate">
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
      )}

      {skipped.length > 0 && (
        <details className="mt-2 group">
          <summary className="font-mono text-[10px] uppercase tracking-[0.14em] text-taupe cursor-pointer hover:text-ink list-none">
            {skipped.length} skipped (no handler)
          </summary>
          <ul className="mt-2 space-y-1 text-[12px] text-taupe font-mono">
            {skipped.map((p, i) => (
              <li key={i} className="truncate">
                {p.call}
              </li>
            ))}
          </ul>
        </details>
      )}
    </div>
  );
}

function fallbackLabel(req: import("../lib/types").RequestEvent): string {
  if (!req.method || !req.path) return "request";
  return `${req.method} ${req.path}`;
}