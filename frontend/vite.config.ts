import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": "http://localhost:1234",
      "/products": "http://localhost:1234",
      "/products/filter": "http://localhost:1234",
      "/home": "http://localhost:1234",
      "/search": "http://localhost:1234",
      "/category": "http://localhost:1234",
      "/reviews": "http://localhost:1234",
      "/cart": "http://localhost:1234",
      "/checkout": "http://localhost:1234",
      "/payment": "http://localhost:1234",
      "/wishlist": "http://localhost:1234",
      "/healthz": "http://localhost:1234",
    },
  },
});