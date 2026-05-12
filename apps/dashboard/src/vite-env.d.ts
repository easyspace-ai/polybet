/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_WS_URL?: string;
  readonly VITE_API_BASE_URL?: string;
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
    updater?: {
      checkForUpdates: () => Promise<{ ok: true; currentVersion: string; latestVersion: string; available: boolean } | { ok: false; error: string }>;
      downloadUpdate: () => Promise<void>;
      installUpdate: () => Promise<void>;
      onUpdateAvailable: (callback: (info: { version: string; releaseNotes?: string }) => void) => () => void;
      onDownloadProgress: (callback: (progress: { percent: number }) => void) => () => void;
      onUpdateDownloaded: (callback: () => void) => () => void;
    };
  }
}

export {};