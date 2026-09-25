import { useState } from "react";
import type { Product } from "../lib/types";

interface FilterBarProps {
  onSubmit: (opts: { name_substr: string; min_price?: number; max_price?: number }) => void;
  onClear: () => void;
  status: "HIT" | "MISS" | null;
  loading: boolean;
}

export function FilterBar({ onSubmit, onClear, status, loading }: FilterBarProps) {
  const [q, setQ] = useState("");
  const [min, setMin] = useState("");
  const [max, setMax] = useState("");

  function submit(e: React.FormEvent) {
    e.preventDefault();
    const opts: { name_substr: string; min_price?: number; max_price?: number } = {
      name_substr: q.trim(),
    };
    const mn = parseFloat(min);
    if (!Number.isNaN(mn)) opts.min_price = mn;
    const mx = parseFloat(max);
    if (!Number.isNaN(mx)) opts.max_price = mx;
    onSubmit(opts);
  }

  return (
    <form
      onSubmit={submit}
      className="border border-rule bg-paper/60 p-5 grid grid-cols-1 md:grid-cols-[1fr_120px_120px_auto_auto] gap-3 items-end"
    >
      <Field label="name contains" htmlFor="q">
        <input
          id="q"
          type="text"
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="e.g. leather"
          className="w-full bg-transparent border-b border-rule focus:border-rust focus:outline-none py-1 text-[15px] font-display-italic placeholder:text-taupe/60"
        />
      </Field>
      <Field label="min $" htmlFor="min">
        <input
          id="min"
          type="number"
          step="0.01"
          value={min}
          onChange={(e) => setMin(e.target.value)}
          placeholder="0"
          className="w-full bg-transparent border-b border-rule focus:border-rust focus:outline-none py-1 text-[15px] font-mono placeholder:text-taupe/60"
        />
      </Field>
      <Field label="max $" htmlFor="max">
        <input
          id="max"
          type="number"
          step="0.01"
          value={max}
          onChange={(e) => setMax(e.target.value)}
          placeholder="999"
          className="w-full bg-transparent border-b border-rule focus:border-rust focus:outline-none py-1 text-[15px] font-mono placeholder:text-taupe/60"
        />
      </Field>
      <button
        type="submit"
        disabled={loading}
        className="font-mono text-[12px] uppercase tracking-[0.16em] bg-ink text-paper px-5 py-2.5 hover:bg-rust transition-colors disabled:opacity-40"
      >
        {loading ? "searching…" : "filter"}
      </button>
      <button
        type="button"
        onClick={() => {
          setQ("");
          setMin("");
          setMax("");
          onClear();
        }}
        className="font-mono text-[12px] uppercase tracking-[0.16em] text-taupe hover:text-ink px-2 py-2.5"
      >
        clear
      </button>
      {status && (
        <div className="md:col-span-5 -mt-1 flex items-center gap-2 text-[10px] font-mono uppercase tracking-[0.16em] text-taupe">
          <span>filter result</span>
          <span className={status === "HIT" ? "text-rust" : "text-inkmute"}>
            served from {status === "HIT" ? "cache" : "supabase"}
          </span>
        </div>
      )}
    </form>
  );
}

function Field({
  label,
  htmlFor,
  children,
}: {
  label: string;
  htmlFor: string;
  children: React.ReactNode;
}) {
  return (
    <label htmlFor={htmlFor} className="flex flex-col gap-1.5">
      <span className="font-mono text-[10px] uppercase tracking-[0.16em] text-taupe">
        {label}
      </span>
      {children}
    </label>
  );
}

export function filterMatches(product: Product, opts: { name_substr: string; min_price?: number; max_price?: number }): boolean {
  if (opts.name_substr && !product.name.toLowerCase().includes(opts.name_substr.toLowerCase())) {
    return false;
  }
  if (opts.min_price != null && product.price < opts.min_price) return false;
  if (opts.max_price != null && product.price > opts.max_price) return false;
  return true;
}