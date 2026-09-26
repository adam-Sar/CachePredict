import { useCallback, useMemo, useState } from "react";
import { ActivityPanel } from "./components/ActivityPanel";
import { CacheBanner, buildEntries } from "./components/CacheBanner";
import { CartView } from "./components/CartView";
import { CategoryView } from "./components/CategoryView";
import { CheckoutView } from "./components/CheckoutView";
import { HomeView } from "./components/HomeView";
import { Header } from "./components/Header";
import { ProductView } from "./components/ProductView";
import { ProductsView } from "./components/ProductsView";
import { ReviewsView } from "./components/ReviewsView";
import { SearchView } from "./components/SearchView";
import { TopNav } from "./components/TopNav";
import { WishlistView } from "./components/WishlistView";
import { useEventStream } from "./hooks/useEventStream";
import type { CachedEntry } from "./components/CacheBanner";
import type { RequestEvent } from "./lib/types";
import type { Navigate, ViewState } from "./lib/view";
import { PRODUCTS } from "./lib/view";

export default function App() {
  const [view, setView] = useState<ViewState>(PRODUCTS);
  const [cartVersion, setCartVersion] = useState(0);

  const { connected, items, requests } = useEventStream();
  const [cachedEntries, setCachedEntries] = useState<CachedEntry[]>([]);

  const navigate: Navigate = useCallback((next) => {
    setView(next);
    window.scrollTo({ top: 0, behavior: "smooth" });
  }, []);

  const onPrefetch = useCallback((e: import("./lib/types").PrefetchEvent) => {
    const next = buildEntries(e);
    setCachedEntries(next);
  }, []);

  const { hitCount, missCount } = useMemo(() => countStats(requests), [requests]);
  const sessionShort = useMemo(() => sessionFromCookie(), []);

  const bumpCart = useCallback(() => setCartVersion((v) => v + 1), []);

  return (
    <div className="min-h-full flex flex-col">
      <Header
        connected={connected}
        sessionShort={sessionShort}
        requestCount={requests.length}
        hitCount={hitCount}
        missCount={missCount}
      />

      <TopNav current={view} navigate={navigate} onSearch={(q) => navigate({ view: "search", query: q })} />

      <CacheBanner entries={cachedEntries} />

      <main className="flex-1 grid grid-cols-1 lg:grid-cols-[1fr_360px]">
        <section className="mx-auto max-w-[1100px] w-full px-6 lg:px-10 py-10">
          <ViewRouter view={view} navigate={navigate} cartVersion={cartVersion} bumpCart={bumpCart} />
        </section>
        <ActivityPanel items={items} />
      </main>

      <footer className="border-t border-rule">
        <div className="mx-auto max-w-[1400px] px-6 lg:px-10 py-4 flex items-center justify-between font-mono text-[11px] text-taupe">
          <span>
            LSTM · ONNX · Ristretto · <span className="text-ink">cachepredict</span>
          </span>
          <span>
            proxy → <span className="text-ink">localhost:1234</span>
          </span>
        </div>
      </footer>
    </div>
  );
}

interface ViewRouterProps {
  view: ViewState;
  navigate: Navigate;
  cartVersion: number;
  bumpCart: () => void;
}

function ViewRouter({ view, navigate, cartVersion, bumpCart }: ViewRouterProps) {
  switch (view.view) {
    case "home":
      return <HomeView navigate={navigate} />;
    case "products":
      return <ProductsView navigate={navigate} />;
    case "search":
      return <SearchView query={view.query ?? ""} navigate={navigate} />;
    case "category":
      return <CategoryView category={view.category ?? ""} navigate={navigate} />;
    case "product":
      return (
        <div className="space-y-10">
          <ProductView
            productId={view.productId ?? 0}
            navigate={navigate}
            bumpCart={bumpCart}
          />
          {view.productId ? (
            <ReviewsView productId={view.productId} />
          ) : null}
        </div>
      );
    case "cart":
      return <CartView navigate={navigate} refreshKey={cartVersion} />;
    case "checkout":
      return <CheckoutView navigate={navigate} />;
    case "wishlist":
      return <WishlistView navigate={navigate} />;
    case "reviews":
      return <ReviewsView productId={view.productId ?? 0} />;
  }
}

function countStats(reqs: RequestEvent[]) {
  let hit = 0;
  let miss = 0;
  for (const r of reqs) {
    if (r.cache_status === "HIT") hit++;
    else if (r.cache_status === "MISS") miss++;
  }
  return { hitCount: hit, missCount: miss };
}

function sessionFromCookie(): string {
  const m = document.cookie.match(/(?:^|;\s*)session_id=([^;]+)/);
  return m ? m[1].slice(0, 8) : "";
}