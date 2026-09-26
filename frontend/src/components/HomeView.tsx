import { useEffect, useState } from "react";
import { getHome } from "../lib/api";
import type { HomeResponse } from "../lib/types";
import type { Navigate, ViewState } from "../lib/view";
import { PRODUCTS, viewLabel } from "../lib/view";
import { ProductGrid } from "./ProductGrid";
import { SectionHead } from "./Shared";

interface HomeViewProps {
  navigate: Navigate;
}

export function HomeView({ navigate }: HomeViewProps) {
  const [data, setData] = useState<HomeResponse | null>(null);
  const [status, setStatus] = useState<"HIT" | "MISS" | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setError(null);
    getHome()
      .then((r) => {
        setData(r.data);
        setStatus(r.status);
      })
      .catch((e) => setError(String(e?.message ?? e)));
  }, []);

  if (error) {
    return (
      <div className="border border-rust/40 bg-rust/5 p-5 font-mono text-[12px] text-rustdim">
        Home error: {error}
      </div>
    );
  }
  if (!data) {
    return <div className="font-display-italic text-taupe">Loading home…</div>;
  }

  const goProducts: ViewState = PRODUCTS;
  const goCategory = (type: string): ViewState => ({ view: "category", category: type });

  return (
    <div className="space-y-12">
      <section>
        <div className="font-mono text-[10px] uppercase tracking-[0.2em] text-taupe mb-2">Season</div>
        <h1 className="font-display-roman text-[56px] leading-[1.0] text-ink text-balance">
          {data.hero}
        </h1>
        <div className="flex items-center gap-4 mt-3">
          <button
            onClick={() => navigate(goProducts)}
            className="font-mono text-[12px] uppercase tracking-[0.16em] bg-ink text-paper px-5 py-2.5 hover:bg-rust transition-colors"
          >
            browse all
          </button>
          <span className="font-mono text-[10px] uppercase tracking-[0.16em] text-taupe">
            home · {status === "HIT" ? "served from cache" : "fresh"}
          </span>
        </div>
      </section>

      <section>
        <SectionHead kicker="Featured" title="Hand-picked" />
        <ProductGrid
          products={data.featured}
          emptyMessage=""
          navigate={navigate}
        />
      </section>

      <section>
        <SectionHead kicker="Browse" title="By category" />
        <div className="grid grid-cols-2 sm:grid-cols-3 gap-px bg-rule border border-rule">
          {data.sections.map((s) => (
            <button
              key={s.id}
              onClick={() => navigate(goCategory(s.id))}
              className="bg-paper hover:bg-paperdim px-5 py-6 text-left transition-colors"
            >
              <div className="font-display-roman text-[18px] text-ink">{s.title}</div>
              <div className="font-mono text-[10px] uppercase tracking-[0.18em] text-taupe mt-1">
                {viewLabel({ view: "category", category: s.id })} →
              </div>
            </button>
          ))}
        </div>
      </section>
    </div>
  );
}