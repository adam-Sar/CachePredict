import { useEffect, useState } from "react";
import type { PrefetchEvent } from "../lib/types";

export interface CachedEntry {
  call: string;
  prob: number;
  ts: number;
  bytes: number;
}

interface CacheBannerProps {
  entries: CachedEntry[];
  onExpire?: (entry: CachedEntry) => void;
}

export function CacheBanner({ entries, onExpire }: CacheBannerProps) {
  const [visible, setVisible] = useState<CachedEntry[]>([]);

  useEffect(() => {
    if (entries.length === 0) {
      setVisible([]);
      return;
    }
    const latest = entries[entries.length - 1];
    setVisible((prev) => {
      const next = [...prev.filter((e) => e.call !== latest.call), latest];
      return next.slice(-3);
    });
    onExpire?.(latest);
  }, [entries, onExpire]);

  if (visible.length === 0) return null;

  return (
    <div className="border-b border-rule bg-paperdim/40">
      <div className="mx-auto max-w-[1400px] px-6 lg:px-10 py-4">
        <div className="flex items-center gap-4 flex-wrap">
          <span className="font-mono text-[10px] uppercase tracking-[0.2em] text-rust whitespace-nowrap">
            cache warmed ↳
          </span>
          <ul className="flex items-center gap-3 flex-wrap">
            {visible.map((e, i) => (
              <li
                key={`${e.call}-${e.ts}`}
                className="font-mono text-[12px] text-ink animate-banner-in"
                style={{ animationDelay: `${i * 60}ms` }}
              >
                <span className="text-taupe mr-1">{e.prob ? `${(e.prob * 100).toFixed(0)}%` : ""}</span>
                <span>{e.call}</span>
              </li>
            ))}
          </ul>
          <span className="ml-auto font-mono text-[10px] uppercase tracking-[0.2em] text-taupe whitespace-nowrap">
            ttl {Math.round(60)}s
          </span>
        </div>
      </div>
    </div>
  );
}

export function buildEntry(e: PrefetchEvent): CachedEntry[] {
  const now = Date.now();
  return e.predictions
    .filter((p) => p.stored)
    .map((p) => ({
      call: p.call,
      prob: p.prob,
      ts: now,
      bytes: p.bytes ?? 0,
    }));
}