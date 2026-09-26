import { useEffect, useState } from "react";
import { addToCart, getCart } from "../lib/api";
import type { CartResponse } from "../lib/types";
import type { Navigate, ViewState } from "../lib/view";
import { CHECKOUT } from "../lib/view";
import { SectionHead } from "./Shared";

interface CartViewProps {
  navigate: Navigate;
  refreshKey: number;
}

export function CartView({ navigate, refreshKey }: CartViewProps) {
  const [data, setData] = useState<CartResponse | null>(null);
  const [status, setStatus] = useState<"HIT" | "MISS" | null>(null);
  const [adding, setAdding] = useState(false);

  useEffect(() => {
    getCart().then((r) => {
      setData(r.data);
      setStatus(r.status);
    });
  }, [refreshKey]);

  async function bump(id: number) {
    setAdding(true);
    await addToCart(id, 1);
    const r = await getCart();
    setData(r.data);
    setStatus(r.status);
    setAdding(false);
  }

  if (!data) {
    return <div className="font-display-italic text-taupe">Reading cart…</div>;
  }

  const goCheckout: ViewState = CHECKOUT;

  return (
    <div className="space-y-8">
      <SectionHead
        kicker="Bag"
        title="Your cart"
        meta={status === "HIT" ? "from cache" : "live"}
        accent={status === "HIT" ? "rust" : "ink"}
        action={
          data.count > 0
            ? { label: "Checkout →", onClick: () => navigate(goCheckout) }
            : undefined
        }
      />

      {data.count === 0 ? (
        <div className="border border-dashed border-rule p-12 text-center text-taupe font-display-italic text-[18px]">
          Cart is empty. Browse products and add to bag.
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
                <div className="font-mono text-[12px] text-taupe mt-1">
                  ${it.price.toFixed(2)} × {it.quantity} = ${(it.price * it.quantity).toFixed(2)}
                </div>
              </div>
              <button
                disabled={adding}
                onClick={() => bump(it.product_id)}
                className="font-mono text-[10px] uppercase tracking-[0.16em] text-taupe hover:text-rust disabled:opacity-40"
              >
                +1
              </button>
            </li>
          ))}
          <li className="p-5 flex items-center justify-between bg-paperdim/40">
            <span className="font-mono text-[10px] uppercase tracking-[0.18em] text-taupe">
              Total
            </span>
            <span className="font-display-roman text-[28px] text-ink leading-none">
              ${data.total.toFixed(2)}
            </span>
          </li>
        </ul>
      )}
    </div>
  );
}