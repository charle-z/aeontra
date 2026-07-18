import { fileURLToPath, URL } from "node:url";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
  base: "/console/assets/",
  plugins: [react()],
  build: {
    outDir: fileURLToPath(new URL("../../internal/console/assets", import.meta.url)),
    emptyOutDir: true,
    assetsInlineLimit: 0,
    cssCodeSplit: false,
    rollupOptions: {
      output: {
        entryFileNames: "app.js",
        chunkFileNames: "chunks/[name]-[hash].js",
        assetFileNames: (assetInfo) =>
          assetInfo.name?.endsWith(".css") ? "app.css" : "assets/[name]-[hash][extname]",
      },
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: "./src/test/setup.ts",
    exclude: ["e2e/**", "node_modules/**"],
    css: true,
  },
});
