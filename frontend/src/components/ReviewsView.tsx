import { useEffect, useState } from "react";

interface ReviewsViewProps {
  productId: number;
  productName?: string;
}

interface ReviewsResponse {
  product_id: string;
  average_rating: number;
  review_count: number;
  reviews: unknown[];
  would_recommend: number;
}

export function ReviewsView({ productId, productName }: ReviewsViewProps) {
  const [data, setData] = useState<ReviewsResponse | null>(null);
  const [status, setStatus] = useState<"HIT" | "MISS" | null>(null);

  useEffect(() => {
    fetch(`/reviews?product_id=${productId}`, { credentials: "include" })
      .then(async (res) => {
        const s = res.headers.get("X-Cache") === "HIT" ? "HIT" : "MISS";
        setStatus(s);
        setData((await res.json()) as ReviewsResponse);
      });
  }, [productId]);

  if (!data) return null;

  return (
    <div className="border-t border-rule pt-8 mt-8">
      <div className="flex items-baseline justify-between">
        <h3 className="font-display-roman text-[24px] text-ink leading-none">
          Reviews {productName && <span className="font-display-italic text-taupe">· {productName}</span>}
        </h3>
        <div className="font-mono text-[10px] uppercase tracking-[0.18em] text-taupe">
          {data.review_count} reviews · {status === "HIT" ? "cached" : "fresh"}
        </div>
      </div>
      <div className="flex items-baseline gap-3 mt-3 font-mono text-[14px]">
        <span className="font-display-roman text-[40px] text-rust leading-none">
          {data.average_rating.toFixed(1)}
        </span>
        <span className="text-taupe">/ 5.0 · {Math.round(data.would_recommend * 100)}% would recommend</span>
      </div>
    </div>
  );
}