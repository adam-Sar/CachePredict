import type {
  CartResponse,
  CategoryResponse,
  CheckoutResponse,
  HomeResponse,
  PaymentResponse,
  ProductDetailResponse,
  ProductListResponse,
  SearchResponse,
  WishlistResponse,
} from "./types";

async function jget<T>(url: string, init?: RequestInit): Promise<{ data: T; status: "HIT" | "MISS" }> {
  const res = await fetch(url, { credentials: "include", ...init });
  const status = res.headers.get("X-Cache") === "HIT" ? "HIT" : "MISS";
  const data = (await res.json()) as T;
  return { data, status };
}

export function listProducts() {
  return jget<ProductListResponse>("/products");
}

export function getProduct(id: number) {
  return jget<ProductDetailResponse>(`/products?id=${id}`);
}

export function getHome() {
  return jget<HomeResponse>("/home");
}

export function searchProducts(q: string) {
  return jget<SearchResponse>(`/search?q=${encodeURIComponent(q)}`);
}

export function listByCategory(category: string) {
  return jget<CategoryResponse>(`/category?type=${encodeURIComponent(category)}`);
}

export function filterProducts(opts: { name_substr: string; min_price?: number; max_price?: number }) {
  return jget<ProductListResponse>("/products/filter", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(opts),
  });
}

export function getCart() {
  return jget<CartResponse>("/cart");
}

export function addToCart(productId: number, quantity = 1) {
  return jget<{ status: string }>("/cart", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ product_id: productId, quantity }),
  });
}

export function getCheckout() {
  return jget<CheckoutResponse>("/checkout");
}

export function submitPayment(amount: number, method = "card") {
  return jget<PaymentResponse>("/payment", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ amount, method }),
  });
}

export function getWishlist() {
  return jget<WishlistResponse>("/wishlist");
}

export function addToWishlist(productId: number) {
  return jget<{ status: string }>("/wishlist?product_id=" + productId, {
    method: "POST",
  });
}