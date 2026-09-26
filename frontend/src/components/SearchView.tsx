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

  useEffect(() => {
    if (!query) return;
    searchProducts(query).then((r) => {
      setData(r.data);
      setStatus(r.status);
    });
  }, [query]);

  if (!query) {
    return (
      <div className="border border-dashed border-rule p-12 text-center text-taupe font-display-italic text-[18px]">
        Type a query in the bar above to search.
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