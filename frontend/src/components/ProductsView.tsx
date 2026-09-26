import { useEffect, useState } from "react";
import { filterProducts, listProducts } from "../lib/api";
import type { Product } from "../lib/types";
import type { Navigate, ViewState } from "../lib/view";
import { FilterBar } from "./FilterBar";
import { ProductGrid } from "./ProductGrid";
import { SectionHead } from "./Shared";

interface ProductsViewProps {
  navigate: Navigate;
}

export function ProductsView({ navigate }: ProductsViewProps) {
  const [products, setProducts] = useState<Product[] | null>(null);
  const [listStatus, setListStatus] = useState<"HIT" | "MISS" | null>(null);
  const [filterStatus, setFilterStatus] = useState<"HIT" | "MISS" | null>(null);
  const [filtered, setFiltered] = useState<Product[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    listProducts().then((r) => {
      setProducts(r.data.products);
      setListStatus(r.status);
    }).catch((e) => setError(String(e)));
  }, []);

  async function applyFilter(opts: { name_substr: string; min_price?: number; max_price?: number }) {
    setLoading(true);
    try {
      const r = await filterProducts(opts);
      setFiltered(r.data.products);
      setFilterStatus(r.status);
    } finally {
      setLoading(false);
    }
  }

  function clearFilter() {
    setFiltered(null);
    setFilterStatus(null);
  }

  if (error) {
    return <div className="border border-rust/40 bg-rust/5 p-5 font-mono text-[12px] text-rustdim">{error}</div>;
  }
  if (!products) {
    return <div className="font-display-italic text-taupe">Loading products…</div>;
  }

  const items = filtered ?? products;
  const isFiltered = filtered !== null;

  return (
    <div className="space-y-8">
      <SectionHead
        kicker={isFiltered ? "Filtered" : "Catalogue"}
        title={isFiltered ? "Filtered results" : "All products"}
        meta={
          isFiltered
            ? `${items.length} match`
            : listStatus === "HIT"
              ? "served from cache"
              : "fresh from Supabase"
        }
        accent={listStatus === "HIT" ? "rust" : "ink"}
      />

      <FilterBar
        onSubmit={applyFilter}
        onClear={clearFilter}
        status={filterStatus}
        loading={loading}
      />

      <ProductGrid products={items} emptyMessage="Nothing matches. Loosen the filters." navigate={navigate} />

      <div className="font-mono text-[10px] uppercase tracking-[0.18em] text-taupe text-center pt-4">
        — end of catalogue —
      </div>
    </div>
  );
}