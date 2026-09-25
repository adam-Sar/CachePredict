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
      "/healthz": "http://localhost:1234",
    },
  },
});