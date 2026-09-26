import { useEffect, useState } from "react";
import { getCheckout, submitPayment } from "../lib/api";
import type { CheckoutResponse, PaymentResponse } from "../lib/types";
import type { Navigate, ViewState } from "../lib/view";
import { HOME } from "../lib/view";
import { SectionHead } from "./Shared";

interface CheckoutViewProps {
  navigate: Navigate;
}

export function CheckoutView({ navigate }: CheckoutViewProps) {
  const [data, setData] = useState<CheckoutResponse | null>(null);
  const [status, setStatus] = useState<"HIT" | "MISS" | null>(null);
  const [confirmed, setConfirmed] = useState<PaymentResponse | null>(null);
  const [method, setMethod] = useState("card");
  const [paying, setPaying] = useState(false);

  useEffect(() => {
    getCheckout().then((r) => {
      setData(r.data);
      setStatus(r.status);
    });
  }, []);

  async function pay() {
    if (!data) return;
    setPaying(true);
    const r = await submitPayment(data.total, method);
    setConfirmed(r.data);
    setPaying(false);
  }

  if (confirmed) {
    return (
      <div className="space-y-6">
        <div className="font-mono text-[10px] uppercase tracking-[0.2em] text-rust">
          confirmed
        </div>
        <h1 className="font-display-roman text-[44px] leading-[1.05] text-ink text-balance">
          {confirmed.order_id}
        </h1>
        <p className="font-display-italic text-[18px] text-inkmute">
          ${confirmed.amount.toFixed(2)} charged to {confirmed.method}.
        </p>
        <button
          onClick={() => navigate(HOME)}
          className="font-mono text-[12px] uppercase tracking-[0.16em] bg-ink text-paper px-5 py-2.5 hover:bg-rust transition-colors mt-4"
        >
          Back home
        </button>
      </div>
    );
  }

  if (!data) {
    return <div className="font-display-italic text-taupe">Building summary…</div>;
  }

  if (data.lines.length === 0) {
    return (
      <div className="space-y-6">
        <SectionHead kicker="Checkout" title="Nothing to pay for" />
        <p className="font-display-italic text-taupe text-[18px]">
          Add something to your cart first.
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-8">
      <SectionHead
        kicker="Checkout"
        title="Review and pay"
        meta={status === "HIT" ? "from cache" : "live"}
        accent={status === "HIT" ? "rust" : "ink"}
      />
      <ul className="border border-rule bg-paper divide-y divide-rule">
        {data.lines.map((l, i) => (
          <li key={i} className="p-4 flex items-baseline justify-between">
            <span className="font-display-roman text-[16px] text-ink break-words">
              {l.name} <span className="text-taupe font-mono text-[12px]">× {l.quantity}</span>
            </span>
            <span className="font-mono text-[14px] text-ink">${l.subtotal.toFixed(2)}</span>
          </li>
        ))}
      </ul>
      <dl className="space-y-2 font-mono text-[13px] text-inkmute">
        <Row label="Subtotal" value={`$${data.subtotal.toFixed(2)}`} />
        <Row label="Shipping" value={data.shipping === 0 ? "Free" : `$${data.shipping.toFixed(2)}`} />
        <Row label="Total" value={`$${data.total.toFixed(2)}`} bold />
      </dl>
      <div className="border-t border-rule pt-6">
        <label className="block mb-3">
          <span className="font-mono text-[10px] uppercase tracking-[0.18em] text-taupe block mb-1.5">
            Payment method
          </span>
          <select
            value={method}
            onChange={(e) => setMethod(e.target.value)}
            className="bg-transparent border-b border-rule focus:border-rust focus:outline-none py-1 text-[15px] font-display-italic"
          >
            <option value="card">Credit card</option>
            <option value="paypal">PayPal</option>
            <option value="apple">Apple Pay</option>
          </select>
        </label>
        <button
          disabled={paying}
          onClick={pay}
          className="font-mono text-[12px] uppercase tracking-[0.16em] bg-ink text-paper px-6 py-3 hover:bg-rust transition-colors disabled:opacity-40"
        >
          {paying ? "processing…" : `Pay $${data.total.toFixed(2)}`}
        </button>
      </div>
    </div>
  );
}

function Row({ label, value, bold }: { label: string; value: string; bold?: boolean }) {
  return (
    <div className={`flex items-baseline justify-between ${bold ? "text-ink" : ""}`}>
      <dt className="text-taupe uppercase tracking-[0.12em] text-[10px]">{label}</dt>
      <dd className={`text-[18px] font-display-roman ${bold ? "text-rust" : "text-ink"}`}>
        {value}
      </dd>
    </div>
  );
}