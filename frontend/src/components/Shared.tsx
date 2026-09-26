interface SectionHeadProps {
  kicker: string;
  title: string;
  meta?: string;
  accent?: "rust" | "ink";
  action?: { label: string; onClick: () => void };
}

export function SectionHead({ kicker, title, meta, accent, action }: SectionHeadProps) {
  return (
    <div className="flex items-end justify-between gap-6 border-b border-rule pb-4">
      <div>
        <div className="font-mono text-[10px] uppercase tracking-[0.2em] text-taupe">{kicker}</div>
        <h2 className="font-display-roman text-[34px] leading-[1.05] text-ink mt-1 text-balance">
          {title}
        </h2>
      </div>
      <div className="flex items-center gap-4 shrink-0">
        {meta && (
          <div
            className={`font-mono text-[11px] uppercase tracking-[0.16em] ${
              accent === "rust" ? "text-rust" : "text-inkmute"
            }`}
          >
            {meta}
          </div>
        )}
        {action && (
          <button
            onClick={action.onClick}
            className="font-mono text-[11px] uppercase tracking-[0.16em] text-ink underline-offset-4 hover:underline"
          >
            {action.label}
          </button>
        )}
      </div>
    </div>
  );
}