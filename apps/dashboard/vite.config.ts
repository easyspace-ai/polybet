// Dev tip: browser HTTP/1.1 limits concurrent connections per host (~6). This app opens
// two WebSockets (/ws + /ws/risk) via the Vite proxy; bursts of /api/* can queue as "Pending".
// Set VITE_API_BASE_URL=http://127.0.0.1:7633 in apps/dashboard/.env.development so REST (+WS
// derived from that base in src/lib/api.ts / wsBus.ts) hit Go directly and leave :6688 slots free.
//
// If you see ECONNREFUSED 127.0.0.1:7633 or "ws proxy socket" EPIPE: the Go server was not
// listening on that port (not started yet, wrong PORT, or process restarted). Start backend first.
import tailwindcss from "@tailwindcss/vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";
import tsConfigPaths from "vite-tsconfig-paths";

const apiOrigin = process.env.VITE_DEV_API_ORIGIN || "http://127.0.0.1:7633";
const wsOrigin = apiOrigin.replace(/^http/, "ws");

const BACKEND_HINT_MS = 15_000;
let lastBackendHintAt = 0;

function warnBackendOnce() {
  const now = Date.now();
  if (now - lastBackendHintAt < BACKEND_HINT_MS) return;
  lastBackendHintAt = now;
  console.warn(
    `\n[polybet] Backend not reachable at ${apiOrigin}. ` +
      `Start Go first: cd server && go run ./cmd/server\n` +
      `(ECONNREFUSED / EPIPE while proxying is expected if 7633 is down or restarting.)\n`,
  );
}

/** Benign when the dashboard proxy runs before the API or after a server restart. */
function isBenignProxyErr(err: unknown): boolean {
  const code =
    err && typeof err === "object" && "code" in err
      ? String((err as NodeJS.ErrnoException).code)
      : "";
  return (
    code === "ECONNREFUSED" || code === "ECONNRESET" || code === "EPIPE" || code === "ETIMEDOUT"
  );
}

/** http-proxy instance from Vite / http-proxy-middleware */
function attachQuietProxyHandlers(proxy: {
  on: (ev: string, fn: (...args: unknown[]) => void) => void;
}) {
  proxy.on("error", (err: Error) => {
    if (isBenignProxyErr(err)) {
      warnBackendOnce();
      return;
    }
    console.warn("[vite proxy]", err.message);
  });
  proxy.on(
    "proxyReqWs",
    (
      _proxyReq: unknown,
      _req: unknown,
      socket: { on?: (ev: string, fn: (e: Error) => void) => void },
    ) => {
      socket?.on?.("error", (e: Error) => {
        if (isBenignProxyErr(e)) {
          warnBackendOnce();
          return;
        }
        console.warn("[vite ws proxy socket]", e.message);
      });
    },
  );
}

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
        configure: (proxy) => attachQuietProxyHandlers(proxy),
      },
      "/ws": {
        target: wsOrigin,
        ws: true,
        changeOrigin: true,
        configure: (proxy) => attachQuietProxyHandlers(proxy),
      },
    },
  },
  preview: {
    host: "127.0.0.1",
    port: 6688,
    strictPort: true,
  },
});
