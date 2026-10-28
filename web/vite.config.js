import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const api = process.env.UNISON_API || "http://localhost:4600";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "components/graphical": path.resolve(__dirname, "../components/graphical"),
      "@base-ui/react": path.resolve(__dirname, "node_modules/@base-ui/react"),
      "motion/react": path.resolve(__dirname, "node_modules/motion/react"),
      "motion": path.resolve(__dirname, "node_modules/motion"),
      "framer-motion": path.resolve(__dirname, "node_modules/framer-motion"),
      "motion-dom": path.resolve(__dirname, "node_modules/motion-dom"),
      "motion-utils": path.resolve(__dirname, "node_modules/motion-utils"),
      "lucide-react": path.resolve(__dirname, "node_modules/lucide-react"),
    },
  },
  build: {
    outDir: "../internal/webui/dist",
    emptyOutDir: true,
  },
  server: {
    proxy: {
      "/api": {
        target: api,
        changeOrigin: true,
      },
      "/agent.md": {
        target: api,
        changeOrigin: true,
      },
      "/download": {
        target: api,
        changeOrigin: true,
      },
      "^/w/[^/]+/agent\\.md$": {
        target: api,
        changeOrigin: true,
      },
    },
  },
});
