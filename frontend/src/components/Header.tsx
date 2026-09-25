interface HeaderProps {
  connected: boolean;
  sessionShort: string;
  requestCount: number;
  hitCount: number;
  missCount: number;
}

export function Header({ connected, sessionShort, requestCount, hitCount, missCount }: HeaderProps) {
  const hitRate = requestCount === 0 ? 0 : Math.round((hitCount / requestCount) * 100);
  return (
    <header className="border-b border-rule bg-paper/80 backdrop-blur-sm sticky top-0 z-30">
      <div className="mx-auto max-w-[1400px] px-6 lg:px-10 py-4 flex items-baseline gap-8">
        <div className="flex items-baseline gap-3">
          <span className="font-display-roman text-[26px] leading-none text-ink">
            Cache<span className="font-display-italic text-rust">Predict</span>
          </span>
          <span className="hidden sm:inline text-[11px] uppercase tracking-[0.18em] text-taupe font-mono">
            live / cache oracle
          </span>
        </div>

        <div className="ml-auto flex items-center gap-6 text-[12px] font-mono text-inkmute">
          <Cell label="session" value={sessionShort || "—"} />
          <Cell
            label="link"
            value={connected ? "live" : "offline"}
            accent={connected ? "ok" : "warn"}
          />
          <Cell label="req" value={String(requestCount)} />
          <Cell label="hit" value={`${hitCount} / ${hitRate}%`} accent={hitCount > 0 ? "rust" : undefined} />
          <Cell label="miss" value={String(missCount)} />
        </div>
      </div>
    </header>
  );
}

function Cell({
  label,
  value,
  accent,
}: {
  label: string;
  value: string;
  accent?: "ok" | "warn" | "rust";
}) {
  const valueClass =
    accent === "ok"
      ? "text-ink"
      : accent === "warn"
      ? "text-amber"
      : accent === "rust"
      ? "text-rust"
      : "text-ink";
  return (
    <div className="flex flex-col items-end leading-tight">
      <span className="text-[10px] uppercase tracking-[0.16em] text-taupe">{label}</span>
      <span className={`text-[13px] ${valueClass}`}>{value}</span>
    </div>
  );
}