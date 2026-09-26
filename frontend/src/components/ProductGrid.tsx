import type { Product } from "../lib/types";
import type { Navigate, ViewState } from "../lib/view";

interface ProductGridProps {
  products: Product[];
  emptyMessage?: string;
  navigate: Navigate;
}

export function ProductGrid({ products, emptyMessage, navigate }: ProductGridProps) {
  if (products.length === 0) {
    return (
      <div className="border border-dashed border-rule p-12 text-center text-taupe font-display-italic text-[18px]">
        {emptyMessage || "Nothing here yet."}
      </div>
    );
  }
  return (
    <ul className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-px bg-rule border border-rule">
      {products.map((p, i) => (
        <ProductCard
          key={p.id}
          product={p}
          index={i}
          onClick={() =>
            navigate({
              view: "product",
              productId: p.id,
            } as ViewState)
          }
        />
      ))}
    </ul>
  );
}

export function ProductCard({
  product,
  index,
  onClick,
}: {
  product: Product;
  index: number;
  onClick: () => void;
}) {
  return (
    <li
      className="bg-paper hover:bg-paperdim transition-colors animate-fade-up"
      style={{ animationDelay: `${Math.min(index * 40, 320)}ms` }}
    >
      <button
        onClick={onClick}
        className="w-full text-left p-5 flex gap-4 items-start focus:outline-none focus-visible:bg-paperdim"
      >
        <span className="font-mono text-[11px] text-taupe w-8 shrink-0 pt-[2px]">
          {String(product.id).padStart(3, "0")}
        </span>
        <span className="flex-1 min-w-0">
          <span className="block font-display-roman text-[18px] text-ink leading-tight break-words">
            {product.name}
          </span>
          <span className="block text-[12px] text-taupe mt-1 font-mono">
            ${product.price.toFixed(2)}
          </span>
        </span>
      </button>
    </li>
  );
}