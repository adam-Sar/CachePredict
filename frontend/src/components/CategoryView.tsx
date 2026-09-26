import { useEffect, useState } from "react";
import { listByCategory } from "../lib/api";
import type { CategoryResponse } from "../lib/types";
import type { Navigate, ViewState } from "../lib/view";
import { ProductGrid } from "./ProductGrid";
import { SectionHead } from "./Shared";

interface CategoryViewProps {
  category: string;
  navigate: Navigate;
}

export function CategoryView({ category, navigate }: CategoryViewProps) {
  const [data, setData] = useState<CategoryResponse | null>(null);
  const [status, setStatus] = useState<"HIT" | "MISS" | null>(null);

  useEffect(() => {
    if (!category) return;
    listByCategory(category).then((r) => {
      setData(r.data);
      setStatus(r.status);
    });
  }, [category]);

  if (!category) {
    return <div className="font-display-italic text-taupe">No category.</div>;
  }
  if (!data) {
    return <div className="font-display-italic text-taupe">Loading category…</div>;
  }

  return (
    <div className="space-y-8">
      <SectionHead
        kicker="Category"
        title={data.category}
        meta={
          status === "HIT"
            ? `${data.count} items · from cache`
            : `${data.count} items`
        }
        accent={status === "HIT" ? "rust" : "ink"}
      />
      <ProductGrid products={data.products} emptyMessage="Nothing in this category." navigate={navigate} />
    </div>
  );
}