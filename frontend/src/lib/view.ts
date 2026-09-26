export type View =
  | "home"
  | "products"
  | "search"
  | "category"
  | "product"
  | "cart"
  | "checkout"
  | "wishlist"
  | "reviews";

export interface ViewState {
  view: View;
  productId?: number;
  query?: string;
  category?: string;
}

export type Navigate = (state: ViewState) => void;

export const HOME: ViewState = { view: "home" };
export const PRODUCTS: ViewState = { view: "products" };
export const CART: ViewState = { view: "cart" };
export const WISHLIST: ViewState = { view: "wishlist" };
export const CHECKOUT: ViewState = { view: "checkout" };

export function viewLabel(v: ViewState): string {
  switch (v.view) {
    case "home":
      return "Home";
    case "products":
      return "Products";
    case "search":
      return v.query ? `Search · ${v.query}` : "Search";
    case "category":
      return v.category ? `Category · ${v.category}` : "Category";
    case "product":
      return v.productId ? `Product #${v.productId}` : "Product";
    case "cart":
      return "Cart";
    case "checkout":
      return "Checkout";
    case "wishlist":
      return "Wishlist";
    case "reviews":
      return v.productId ? `Reviews · #${v.productId}` : "Reviews";
  }
}