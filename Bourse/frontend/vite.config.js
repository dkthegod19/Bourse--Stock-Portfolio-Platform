import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// During `npm run dev` the API runs separately on :8080, so proxy /v1 and
// /healthz to it. In production the Go server serves the built `dist` and the
// API from the same origin, so relative paths just work.
export default defineConfig({
  plugins: [react()],
  base: "./",
  build: { outDir: "dist" },
  server: {
    proxy: {
      "/v1": "http://localhost:8080",
      "/healthz": "http://localhost:8080",
    },
  },
});
