export interface RequestEvent {
  seq?: number;
  type: "request";
  sid: string;
  method?: string;
  path?: string;
  query?: string;
  key?: string;
  label?: string;
  resource?: string;
  cache_status?: "HIT" | "MISS";
  bytes?: number;
  ttl_ms?: number;
}

export interface PrefetchPrediction {
  call: string;
  prob: number;
  stored: boolean;
  reason?: string;
  bytes?: number;
  label?: string;
  resource?: string;
}

export interface PrefetchEvent {
  seq?: number;
  type: "prefetch";
  sid: string;
  history?: string[];
  predictions: PrefetchPrediction[];
}

export type LiveEvent = RequestEvent | PrefetchEvent;

export interface Product {
  id: number;
  name: string;
  image_path: string;
  image_url: string;
  price: number;
  description: string;
}

export interface ProductListResponse {
  products: Product[];
  count: number;
}

export interface ProductDetailResponse {
  product: Product;
}

export interface SearchResponse {
  query: string;
  products: Product[];
  count: number;
}

export interface CategoryResponse {
  category: string;
  products: Product[];
  count: number;
}

export interface HomeResponse {
  hero: string;
  featured: Product[];
  sections: { id: string; title: string }[];
}

export interface HomeSection {
  id: string;
  title: string;
}

export interface CartItem {
  product_id: number;
  name: string;
  price: number;
  image_url: string;
  quantity: number;
}

export interface CartResponse {
  items: CartItem[];
  total: number;
  count: number;
}

export interface CheckoutLine {
  name: string;
  price: number;
  quantity: number;
  subtotal: number;
}

export interface CheckoutResponse {
  lines: CheckoutLine[];
  subtotal: number;
  shipping: number;
  total: number;
  currency: string;
}

export interface PaymentResponse {
  status: string;
  order_id: string;
  amount: number;
  method: string;
}

export interface WishlistResponse {
  items: { product_id: number; name: string; price: number; image_url: string }[];
  count: number;
}