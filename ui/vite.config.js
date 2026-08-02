import path from "node:path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// The built UI is committed to ../web, which main.go embeds with `go:embed web`.
// That keeps `go build` a single, Node-free step for anyone who is not editing
// the front-end.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { "@": path.resolve(import.meta.dirname, "./src") },
  },
  build: {
    outDir: path.resolve(import.meta.dirname, "../web"),
    emptyOutDir: true,
    // One chunk on purpose: the app is served from localhost by the same binary,
    // so splitting would only trade an instant load for extra loading states.
    chunkSizeWarningLimit: 700,
  },
  server: {
    // `npm run dev` proxies the API to a locally running restic-web.
    proxy: {
      "/api": { target: "http://127.0.0.1:8080", changeOrigin: true },
    },
  },
});
