import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// TIDE dashboard SPA build config.
//
// - React 18 via @vitejs/plugin-react.
// - Tailwind v4 via @tailwindcss/vite — design tokens declared in src/index.css `@theme` block.
// - Dev proxy: `/api` → cmd/dashboard backend on :8080, `/healthz` → manager probe on :8081
//   (the dashboard backend exposes process-level liveness at :8080/healthz too, but the
//   informer-cache-gated probe lives on :8081 per plan 04-10 SUMMARY).
// - Build target ES2020 (per RESEARCH §1015) so modern browsers get small output and ts can
//   emit narrow lib types.

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    target: "es2020",
    outDir: "dist",
    sourcemap: false,
  },
  server: {
    port: 5173,
    proxy: {
      "/api": "http://localhost:8080",
      "/healthz": "http://localhost:8081",
    },
  },
});
