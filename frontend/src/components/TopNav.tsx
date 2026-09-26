import type { Navigate, View, ViewState } from "../lib/view";
import { CART, HOME, PRODUCTS, WISHLIST, CHECKOUT, viewLabel } from "../lib/view";

interface TopNavProps {
  current: ViewState;
  navigate: Navigate;
  onSearch: (query: string) => void;
}

const TABS: View[] = ["home", "products", "cart", "wishlist", "checkout"];

export function TopNav({ current, navigate, onSearch }: TopNavProps) {
  return (
    <nav className="border-b border-rule bg-paper">
      <div className="mx-auto max-w-[1400px] px-6 lg:px-10 flex items-center gap-x-6 gap-y-2 flex-wrap py-3">
        {TABS.map((view) => {
          const target: ViewState =
            view === "home" ? HOME : view === "products" ? PRODUCTS : view === "cart" ? CART : view === "wishlist" ? WISHLIST : CHECKOUT;
          const active = current.view === view;
          return (
            <button
              key={view}
              onClick={() => navigate(target)}
              className={`font-mono text-[11px] uppercase tracking-[0.18em] transition-colors ${
                active ? "text-rust" : "text-inkmute hover:text-ink"
              }`}
            >
              {viewLabel(target)}
            </button>
          );
        })}
        <form
          onSubmit={(e) => {
            e.preventDefault();
            const fd = new FormData(e.currentTarget);
            const q = String(fd.get("q") ?? "").trim();
            if (q) onSearch(q);
            e.currentTarget.reset();
          }}
          className="ml-auto flex items-center gap-2"
        >
          <input
            name="q"
            placeholder="search…"
            className="bg-transparent border-b border-rule focus:border-rust focus:outline-none py-1 text-[13px] font-display-italic placeholder:text-taupe/60 w-32 sm:w-48"
          />
          <button className="font-mono text-[10px] uppercase tracking-[0.16em] text-taupe hover:text-ink">
            search
          </button>
        </form>
      </div>
    </nav>
  );
}