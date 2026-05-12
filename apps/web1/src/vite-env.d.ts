/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_WS_URL?: string;
  readonly VITE_PUBLIC_MODE?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}

export interface DesktopAPI {
  playSound: (soundName: string) => Promise<{ ok: true } | { ok: false; error: string }>;
}

declare global {
  interface Window {
    desktopAPI?: DesktopAPI;
  }
}

export {};
