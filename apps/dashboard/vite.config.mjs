import { defineConfig, loadEnv } from 'vite';
import react from '@vitejs/plugin-react';

/** Dev: proxy to bot API. Default 127.0.0.1 (bot binds IPv4 only; localhost→::1 often ECONNREFUSED on macOS). */
/** Electron dev uses sibling `apps/electron/vite.config.mjs` (same proxy defaults — keep in sync). */
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '');
  const apiOrigin = env.VITE_DEV_API_ORIGIN || 'http://127.0.0.1:7633';
  const wsOrigin = apiOrigin.replace(/^http/, 'ws');
  const isProd = mode === 'production';
  const electronPack = env.VITE_ELECTRON === 'true';

  return {
    base: isProd && electronPack ? './' : isProd ? '/' : '/',
    plugins: [react()],
    server: {
      /**
       * Bind IPv4 loopback explicitly. Default `localhost` often listens on ::1 only;
       * Electron dev loads `http://127.0.0.1:5173` — mismatch → refused connection → blank window.
       */
      host: '127.0.0.1',
      strictPort: true,
      proxy: {
        '/api': apiOrigin,
        '/ws': { target: wsOrigin, ws: true },
      },
    },
  };
});
