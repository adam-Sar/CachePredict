import type { ProductDetailResponse, ProductListResponse } from "./types";

export async function listProducts(): Promise<ProductListResponse> {
  const res = await fetch("/products", { credentials: "include" });
  if (!res.ok) throw new Error(`listProducts failed: ${res.status}`);
  return res.json();
}

export async function getProduct(id: number): Promise<ProductDetailResponse> {
  const res = await fetch(`/products?id=${id}`, { credentials: "include" });
  if (!res.ok) throw new Error(`getProduct failed: ${res.status}`);
  return res.json();
}

export function cacheStatusOf(res: Response): "HIT" | "MISS" {
  return res.headers.get("X-Cache") === "HIT" ? "HIT" : "MISS";
}