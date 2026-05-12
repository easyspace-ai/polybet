// @lovable.dev/vite-tanstack-config already includes the following — do NOT add them manually
// or the app will break with duplicate plugins:
//   - tanstackStart, viteReact, tailwindcss, tsConfigPaths, cloudflare (build-only),
//     componentTagger (dev-only), VITE_* env injection, @ path alias, React/TanStack dedupe,
//     error logger plugins, and sandbox detection (port/host/strictPort).
import { defineConfig } from "@lovable.dev/vite-tanstack-config";

const apiOrigin = process.env.VITE_DEV_API_ORIGIN || "http://127.0.0.1:7633";
const wsOrigin = apiOrigin.replace(/^http/, "ws");

export default defineConfig({
  cloudflare: false,
  tanstackStart: {
    server: { entry: "server" },
    spa: {
      enabled: true,
      maskPath: "/",
      prerender: {
        outputPath: "/index",
      },
    },
  },
  vite: {
    // Dev server configuration with proxy
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
  },
});
