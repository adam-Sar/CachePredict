import { useEffect, useState } from "react";
import type { PrefetchEvent } from "../lib/types";

export interface CachedEntry {
  label: string;
  resource: string;
  prob: number;
  ts: number;
}

interface CacheBannerProps {
  entries: CachedEntry[];
}

export function CacheBanner({ entries }: CacheBannerProps) {
  const [visible, setVisible] = useState<CachedEntry[]>([]);

  useEffect(() => {
    if (entries.length === 0) {
      setVisible([]);
      return;
    }
    setVisible((prev) => {
      const next = [...prev];
      for (const e of entries) {
        const k = e.label + "|" + e.resource;
        if (!next.some((m) => m.label + "|" + m.resource === k)) {
          next.push(e);
        }
      }
      return next.slice(-6);
    });
  }, [entries]);

  if (visible.length === 0) return null;

  return (
    <div className="border-b border-rule bg-paperdim/40">
      <div className="mx-auto max-w-[1400px] px-6 lg:px-10 py-4">
        <div className="flex items-center gap-x-5 gap-y-2 flex-wrap">
          <span className="font-mono text-[10px] uppercase tracking-[0.2em] text-rust whitespace-nowrap">
            cache warmed ↳
          </span>
          <ul className="flex items-center gap-x-4 gap-y-2 flex-wrap">
            {visible.map((e, i) => (
              <li
                key={`${e.label}|${e.resource}|${e.ts}`}
                className="font-display-roman text-[14px] text-ink animate-banner-in"
                style={{ animationDelay: `${i * 50}ms` }}
              >
                <span className="text-rust mr-1.5 font-mono text-[10px] uppercase tracking-[0.14em]">
                  {Math.round(e.prob * 100)}%
                </span>
                <span>{e.resource || e.label}</span>
              </li>
            ))}
          </ul>
          <span className="ml-auto font-mono text-[10px] uppercase tracking-[0.2em] text-taupe whitespace-nowrap">
            live · ttl 60s
          </span>
        </div>
      </div>
    </div>
  );
}

export function buildEntries(e: PrefetchEvent): CachedEntry[] {
  const now = Date.now();
  return e.predictions
    .filter((p) => p.stored)
    .map((p) => ({
      label: p.label ?? p.call,
      resource: p.resource ?? "",
      prob: p.prob,
      ts: now,
    }));
}