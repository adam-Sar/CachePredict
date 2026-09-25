export type CacheStatus = "HIT" | "MISS" | undefined;

export interface RequestEvent {
  type: "request";
  sid: string;
  method: string;
  path: string;
  key: string;
  cache_status?: CacheStatus;
  bytes?: number;
  ttl_ms?: number;
}

export interface PrefetchPrediction {
  call: string;
  prob: number;
  stored: boolean;
  reason?: string;
  bytes?: number;
}

export interface PrefetchEvent {
  type: "prefetch";
  sid: string;
  history: string[];
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