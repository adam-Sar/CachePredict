import { defineConfig, type ProxyOptions } from "vite";
import react from "@vitejs/plugin-react";

const proxyTarget = "http://localhost:1234";

const proxyPaths = [
  "/api",
  "/products",
  "/products/filter",
  "/home",
  "/search",
  "/category",
  "/reviews",
  "/cart",
  "/checkout",
  "/payment",
  "/wishlist",
  "/healthz",
];

const proxy: Record<string, ProxyOptions> = {};
for (const p of proxyPaths) {
  proxy[p] = {
    target: proxyTarget,
    changeOrigin: true,
    ws: false,
  };
}

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy,
  },
});
