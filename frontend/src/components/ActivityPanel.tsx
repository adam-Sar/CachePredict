import { useMemo } from "react";
import type { ActivityItem } from "../hooks/useEventStream";
import { methodPathFromCall, probText, relativeTime, shortKey } from "../lib/format";

interface ActivityPanelProps {
  items: ActivityItem[];
  now: number;
}

export function ActivityPanel({ items, now }: ActivityPanelProps) {
  const reversed = useMemo(() => [...items].reverse(), [items]);
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
          Every request is fed into the LSTM. Predicted next-calls are
          prefetched into Ristretto so the next click resolves from RAM.
        </p>
      </div>

      <div className="flex-1 overflow-y-auto px-2 py-2 font-mono text-[12px]">
        {reversed.length === 0 && (
          <div className="px-4 py-8 text-taupe text-center text-[12px]">
            No activity yet. Hit a product card.
          </div>
        )}
        <ul className="space-y-1">
          {reversed.map((it) => (
            <Item key={it.id} item={it} now={now} />
          ))}
        </ul>
      </div>
    </aside>
  );
}

function Item({ item, now }: { item: ActivityItem; now: number }) {
  if (item.kind === "request" && item.request) {
    return <RequestRow req={item.request} ts={item.ts} now={now} />;
  }
  if (item.kind === "prefetch" && item.prefetch) {
    return <PrefetchRow ev={item.prefetch} ts={item.ts} now={now} />;
  }
  return null;
}

function RequestRow({
  req,
  ts,
  now,
}: {
  req: import("../lib/types").RequestEvent;
  ts: number;
  now: number;
}) {
  const isHit = req.cache_status === "HIT";
  return (
    <li className="px-3 py-2 border-l-2 border-transparent hover:border-rule hover:bg-paperdim/60 transition-colors">
      <div className="flex items-center justify-between gap-2">
        <span className="text-ink truncate">
          <span className="text-taupe mr-1.5">{req.method}</span>
          <span>{req.path}</span>
        </span>
        <span className={`text-[10px] uppercase tracking-[0.14em] whitespace-nowrap ${isHit ? "text-rust" : "text-taupe"}`}>
          {req.cache_status ?? "—"}
        </span>
      </div>
      <div className="flex items-center justify-between mt-1 text-[10px] text-taupe">
        <span>key {shortKey(req.key)}</span>
        <span>{relativeTime(ts, now)}</span>
      </div>
    </li>
  );
}

function PrefetchRow({
  ev,
  ts,
  now,
}: {
  ev: import("../lib/types").PrefetchEvent;
  ts: number;
  now: number;
}) {
  const stored = ev.predictions.filter((p) => p.stored);
  const skipped = ev.predictions.filter((p) => !p.stored);
  return (
    <li className="px-3 py-2.5 border-l-2 border-rust/40 bg-rust/[0.03] hover:bg-rust/[0.06] transition-colors">
      <div className="flex items-center justify-between text-[10px] uppercase tracking-[0.14em] text-rust">
        <span>prefetch → {stored.length} stored</span>
        <span className="text-taupe normal-case tracking-normal">{relativeTime(ts, now)}</span>
      </div>
      {stored.length > 0 && (
        <ul className="mt-1.5 space-y-0.5">
          {stored.map((p, i) => {
            const { method, path, query } = methodPathFromCall(p.call);
            return (
              <li key={i} className="flex items-center justify-between text-ink">
                <span className="truncate">
                  <span className="text-taupe mr-1.5">{method}</span>
                  <span>{path}</span>
                  {query && <span className="text-taupe">?{query}</span>}
                </span>
                <span className="text-rust text-[10px] ml-2 whitespace-nowrap">{probText(p.prob)}</span>
              </li>
            );
          })}
        </ul>
      )}
      {skipped.length > 0 && (
        <details className="mt-1">
          <summary className="text-[10px] text-taupe cursor-pointer hover:text-ink list-none">
            {skipped.length} skipped
          </summary>
          <ul className="mt-1 space-y-0.5 text-[11px] text-taupe">
            {skipped.map((p, i) => (
              <li key={i} className="truncate">
                {p.call}
                {p.reason ? <span className="ml-1.5">· {p.reason}</span> : null}
              </li>
            ))}
          </ul>
        </details>
      )}
    </li>
  );
}