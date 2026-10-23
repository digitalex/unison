import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// `npm run dev` serves the UI on :5173 and proxies the API to a Unison
// server on :4600 (`go run ./cmd/unison serve`). `npm run build` writes into
// internal/webui/dist, which the Go binary embeds.
const api = "http://localhost:4600";

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "../internal/webui/dist",
    emptyOutDir: true,
  },
  server: {
    proxy: {
      "/api": api,
      "/agent.md": api,
      "/download": api,
      "^/w/[^/]+/agent\\.md$": api,
    },
  },
});
