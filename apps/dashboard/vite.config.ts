import tailwindcss from "@tailwindcss/vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";
import tsConfigPaths from "vite-tsconfig-paths";

const apiOrigin = process.env.VITE_DEV_API_ORIGIN || "http://127.0.0.1:7633";
const wsOrigin = apiOrigin.replace(/^http/, "ws");

export default defineConfig({
  plugins: [tanstackRouter(), react(), tailwindcss(), tsConfigPaths()],
  server: {
    host: "127.0.0.1",
    port: 6688,
    strictPort: true,
    proxy: {
      "/api": {
        target: apiOrigin,
        changeOrigin: true,
      },
      "/ws": {
        target: wsOrigin,
        ws: true,
      },
    },
  },
  preview: {
    host: "127.0.0.1",
    port: 6688,
    strictPort: true,
  },
});
