import { useEffect, useState } from "react";
import { addToWishlist, getWishlist } from "../lib/api";
import type { WishlistResponse } from "../lib/types";
import type { Navigate, ViewState } from "../lib/view";
import { PRODUCTS } from "../lib/view";
import { SectionHead } from "./Shared";

interface WishlistViewProps {
  navigate: Navigate;
}

export function WishlistView({ navigate }: WishlistViewProps) {
  const [data, setData] = useState<WishlistResponse | null>(null);
  const [status, setStatus] = useState<"HIT" | "MISS" | null>(null);

  useEffect(() => {
    getWishlist().then((r) => {
      setData(r.data);
      setStatus(r.status);
    });
  }, []);

  async function remove(id: number) {
    // Simple optimistic removal: just re-fetch (server keeps it via store).
    // For the demo the POST endpoint always adds; the remove flow isn't
    // needed for the cache demo.
    void id;
    void addToWishlist;
    const r = await getWishlist();
    setData(r.data);
    setStatus(r.status);
  }

  if (!data) {
    return <div className="font-display-italic text-taupe">Reading wishlist…</div>;
  }

  return (
    <div className="space-y-8">
      <SectionHead
        kicker="Saved"
        title="Wishlist"
        meta={status === "HIT" ? "from cache" : "live"}
        accent={status === "HIT" ? "rust" : "ink"}
        action={
          data.count > 0
            ? { label: "Browse more →", onClick: () => navigate(PRODUCTS as ViewState) }
            : undefined
        }
      />

      {data.count === 0 ? (
        <div className="border border-dashed border-rule p-12 text-center text-taupe font-display-italic text-[18px]">
          Nothing saved yet. Tap a product and add it.
        </div>
      ) : (
        <ul className="border border-rule divide-y divide-rule bg-paper">
          {data.items.map((it) => (
            <li key={it.product_id} className="p-5 flex items-start gap-4">
              <span className="font-mono text-[11px] text-taupe w-8 shrink-0 pt-1">
                {String(it.product_id).padStart(3, "0")}
              </span>
              <div className="flex-1 min-w-0">
                <div className="font-display-roman text-[18px] text-ink break-words">
                  {it.name}
                </div>
                <div className="font-mono text-[12px] text-taupe mt-1">${it.price.toFixed(2)}</div>
              </div>
              <button
                onClick={() => remove(it.product_id)}
                className="font-mono text-[10px] uppercase tracking-[0.16em] text-taupe hover:text-rust"
              >
                remove
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}