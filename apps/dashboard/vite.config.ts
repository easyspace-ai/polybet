import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import tsconfigPaths from "vite-tsconfig-paths";
import tanstackRouter from "@tanstack/router-plugin/vite";

export default defineConfig({
  plugins: [
    tsconfigPaths(),
    tanstackRouter({
      target: "react",
      autoCodeSplitting: true,
    }),
    react(),
    tailwindcss(),
  ],
  server: {
    host: "127.0.0.1",
    proxy: {
      "/api": "http://127.0.0.1:7633",
      "/ws": { target: "ws://127.0.0.1:7633", ws: true },
    },
  },
  build: {
    target: "es2020",
  },
});