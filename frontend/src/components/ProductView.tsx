import { useEffect, useState } from "react";
import { addToCart, getProduct, addToWishlist } from "../lib/api";
import type { ProductDetailResponse, Product } from "../lib/types";
import type { Navigate, ViewState } from "../lib/view";
import { CART, WISHLIST } from "../lib/view";
import { SectionHead } from "./Shared";

interface ProductViewProps {
  productId: number;
  navigate: Navigate;
  bumpCart: () => void;
}

export function ProductView({ productId, navigate, bumpCart }: ProductViewProps) {
  const [data, setData] = useState<ProductDetailResponse | null>(null);
  const [status, setStatus] = useState<"HIT" | "MISS" | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!productId) return;
    setError(null);
    getProduct(productId)
      .then((r) => {
        setData(r.data);
        setStatus(r.status);
      })
      .catch((e) => setError(String(e?.message ?? e)));
  }, [productId]);

  if (error) {
    return (
      <div className="border border-rust/40 bg-rust/5 p-5 font-mono text-[12px] text-rustdim">
        Product error: {error}
      </div>
    );
  }
  if (!data) {
    return <div className="font-display-italic text-taupe">Loading product…</div>;
  }

  const p: Product = data.product;
  const goCart: ViewState = CART;
  const goWish: ViewState = WISHLIST;

  async function buy() {
    setBusy(true);
    setError(null);
    try {
      await addToCart(p.id, 1);
      bumpCart();
      navigate(goCart);
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  async function save() {
    setBusy(true);
    setError(null);
    try {
      await addToWishlist(p.id);
    } catch (e: any) {
      setError(String(e?.message ?? e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="space-y-8">
      <SectionHead
        kicker="Product"
        title={p.name}
        meta={status === "HIT" ? "served from cache" : "fresh"}
        accent={status === "HIT" ? "rust" : "ink"}
      />
      <div className="grid grid-cols-1 md:grid-cols-[200px_1fr] gap-8">
        <div className="border border-rule aspect-square overflow-hidden bg-paperdim flex items-center justify-center">
          <img
            src={p.image_url}
            alt={p.name}
            className="w-full h-full object-cover"
            onError={(e) => {
              (e.currentTarget as HTMLImageElement).style.display = "none";
            }}
          />
        </div>
        <div className="space-y-6">
          <div className="font-display-roman text-[40px] text-ink leading-none">
            ${p.price.toFixed(2)}
          </div>
          <p className="font-display-italic text-[16px] leading-relaxed text-inkmute max-w-prose">
            {p.description}
          </p>
          <dl className="grid grid-cols-[auto_1fr] gap-y-2 gap-x-6 text-[12px] font-mono text-inkmute">
            <Row label="id" value={String(p.id)} />
            <Row label="sku" value={`SKU-${String(p.id).padStart(4, "0")}`} />
            <Row label="image" value={p.image_path} />
            <Row label="url" value={p.image_url} />
          </dl>
          <div className="flex flex-wrap gap-3 pt-4 border-t border-rule">
            <button
              disabled={busy}
              onClick={buy}
              className="font-mono text-[12px] uppercase tracking-[0.16em] bg-ink text-paper px-5 py-2.5 hover:bg-rust transition-colors disabled:opacity-40"
            >
              Add to cart
            </button>
            <button
              disabled={busy}
              onClick={save}
              className="font-mono text-[12px] uppercase tracking-[0.16em] text-ink border border-ink px-5 py-2.5 hover:bg-ink hover:text-paper transition-colors disabled:opacity-40"
            >
              Save to wishlist
            </button>
            <button
              onClick={() => navigate(goWish)}
              className="font-mono text-[11px] uppercase tracking-[0.16em] text-taupe hover:text-ink underline-offset-4 hover:underline ml-auto"
            >
              View wishlist →
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <>
      <dt className="text-taupe uppercase tracking-[0.12em] text-[10px]">{label}</dt>
      <dd className="text-ink break-all">{value}</dd>
    </>
  );
}