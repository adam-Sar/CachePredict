import { useEffect, useState } from "react";
import type { Product } from "../lib/types";

export function ProductsView() {
  const [products, setProducts] = useState<Product[] | null>(null);
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [selected, setSelected] = useState<Product | null>(null);
  const [listStatus, setListStatus] = useState<"HIT" | "MISS" | null>(null);
  const [detailStatus, setDetailStatus] = useState<"HIT" | "MISS" | null>(null);
  const [listError, setListError] = useState<string | null>(null);
  const [detailError, setDetailError] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    setListError(null);
    (async () => {
      try {
        const res = await fetch("/products", { credentials: "include" });
        if (!alive) return;
        const status = res.headers.get("X-Cache") === "HIT" ? "HIT" : "MISS";
        setListStatus(status);
        const data = await res.json();
        if (alive) setProducts(data.products);
      } catch (e) {
        if (alive) setListError(String(e));
      }
    })();
    return () => {
      alive = false;
    };
  }, []);

  async function openDetail(id: number) {
    setSelectedId(id);
    setDetailError(null);
    setSelected(null);
    try {
      const res = await fetch(`/products?id=${id}`, { credentials: "include" });
      const status = res.headers.get("X-Cache") === "HIT" ? "HIT" : "MISS";
      setDetailStatus(status);
      const data = (await res.json()) as { product: Product };
      setSelected(data.product);
    } catch (e) {
      setDetailError(String(e));
    }
  }

  if (listError) {
    return <ErrorBlock message={listError} />;
  }
  if (!products) {
    return <Skeleton />;
  }

  return (
    <div className="space-y-10">
      <SectionHead
        kicker="Index"
        title="Products"
        meta={listStatus === "HIT" ? "served from cache" : "fresh from Supabase"}
        accent={listStatus === "HIT" ? "rust" : "ink"}
      />

      <ul className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-px bg-rule border border-rule">
        {products.map((p, i) => (
          <ProductCard
            key={p.id}
            product={p}
            index={i}
            selected={selectedId === p.id}
            onClick={() => openDetail(p.id)}
          />
        ))}
      </ul>

      {selected && (
        <div className="border-t border-rule pt-10">
          <SectionHead
            kicker="Detail"
            title={selected.name}
            meta={detailStatus === "HIT" ? "cache HIT" : "fresh fetch"}
            accent={detailStatus === "HIT" ? "rust" : "ink"}
          />
          <Detail product={selected} />
        </div>
      )}
      {detailError && <ErrorBlock message={detailError} />}
    </div>
  );
}

function ProductCard({
  product,
  index,
  selected,
  onClick,
}: {
  product: Product;
  index: number;
  selected: boolean;
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
          <span className="block font-display-roman text-[18px] text-ink leading-tight truncate">
            {product.name}
          </span>
          <span className="block text-[12px] text-taupe mt-1 font-mono">
            ${product.price.toFixed(2)}
          </span>
        </span>
        <span
          className={`text-[11px] font-mono uppercase tracking-[0.14em] pt-[2px] shrink-0 ${
            selected ? "text-rust" : "text-taupe"
          }`}
        >
          {selected ? "open" : "view →"}
        </span>
      </button>
    </li>
  );
}

function Detail({ product }: { product: Product }) {
  return (
    <div className="grid grid-cols-1 md:grid-cols-[180px_1fr] gap-8">
      <div className="border border-rule aspect-[4/5] overflow-hidden bg-paperdim flex items-center justify-center">
        <img
          src={product.image_url}
          alt={product.name}
          className="w-full h-full object-cover"
          onError={(e) => {
            (e.currentTarget as HTMLImageElement).style.display = "none";
          }}
        />
      </div>
      <div className="space-y-5">
        <dl className="grid grid-cols-2 gap-y-3 gap-x-6 text-[13px] font-mono">
          <Row label="id" value={String(product.id)} />
          <Row label="price" value={`$${product.price.toFixed(2)}`} />
          <Row label="image" value={product.image_path} />
          <Row label="url" value={product.image_url} copyable />
        </dl>
        <p className="text-[15px] leading-relaxed text-inkmute max-w-prose">{product.description}</p>
      </div>
    </div>
  );
}

function Row({ label, value, copyable }: { label: string; value: string; copyable?: boolean }) {
  return (
    <>
      <dt className="text-taupe uppercase tracking-[0.12em] text-[10px]">{label}</dt>
      <dd
        className={`text-ink break-all ${copyable ? "select-all" : ""}`}
        title={value}
      >
        {value}
      </dd>
    </>
  );
}

function SectionHead({
  kicker,
  title,
  meta,
  accent,
}: {
  kicker: string;
  title: string;
  meta?: string;
  accent?: "rust" | "ink";
}) {
  return (
    <div className="flex items-end justify-between gap-6 border-b border-rule pb-4">
      <div>
        <div className="font-mono text-[10px] uppercase tracking-[0.2em] text-taupe">{kicker}</div>
        <h2 className="font-display-roman text-[34px] leading-[1.05] text-ink mt-1 text-balance">
          {title}
        </h2>
      </div>
      {meta && (
        <div
          className={`font-mono text-[11px] uppercase tracking-[0.16em] ${
            accent === "rust" ? "text-rust" : "text-inkmute"
          }`}
        >
          {meta}
        </div>
      )}
    </div>
  );
}

function Skeleton() {
  return (
    <div className="space-y-6 animate-pulse-soft">
      <div className="h-10 w-48 bg-paperdim rounded-sm" />
      <div className="grid grid-cols-3 gap-px bg-rule border border-rule">
        {Array.from({ length: 9 }).map((_, i) => (
          <div key={i} className="bg-paper h-20" />
        ))}
      </div>
    </div>
  );
}

function ErrorBlock({ message }: { message: string }) {
  return (
    <div className="border border-rust/40 bg-rust/5 p-5 font-mono text-[12px] text-rustdim">
      {message}
    </div>
  );
}