import { useEffect, useState } from "react";
import { searchProducts } from "../lib/api";
import type { SearchResponse } from "../lib/types";
import type { Navigate, ViewState } from "../lib/view";
import { ProductGrid } from "./ProductGrid";
import { SectionHead } from "./Shared";

interface SearchViewProps {
  query: string;
  navigate: Navigate;
}

export function SearchView({ query, navigate }: SearchViewProps) {
  const [data, setData] = useState<SearchResponse | null>(null);
  const [status, setStatus] = useState<"HIT" | "MISS" | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!query) return;
    setError(null);
    searchProducts(query)
      .then((r) => {
        setData(r.data);
        setStatus(r.status);
      })
      .catch((e) => setError(String(e?.message ?? e)));
  }, [query]);

  if (!query) {
    return (
      <div className="border border-dashed border-rule p-12 text-center text-taupe font-display-italic text-[18px]">
        Type a query in the bar above to search.
      </div>
    );
  }
  if (error) {
    return (
      <div className="border border-rust/40 bg-rust/5 p-5 font-mono text-[12px] text-rustdim">
        Search error: {error}
      </div>
    );
  }
  if (!data) {
    return <div className="font-display-italic text-taupe">Searching…</div>;
  }

  return (
    <div className="space-y-8">
      <SectionHead
        kicker="Search"
        title={`Results for "${data.query}"`}
        meta={
          status === "HIT"
            ? `${data.count} match · from cache`
            : `${data.count} match`
        }
        accent={status === "HIT" ? "rust" : "ink"}
      />
      <ProductGrid products={data.products} emptyMessage="No matches." navigate={navigate} />
    </div>
  );
}