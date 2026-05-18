import { useState, useEffect, useCallback } from "react";

export interface AutoRefreshSettings {
  enabled: boolean;
  intervalMinutes: number;
}

const STORAGE_KEY = "polybet-auto-refresh";

const defaultSettings: AutoRefreshSettings = {
  enabled: true,
  intervalMinutes: 1,
};

const MIN_INTERVAL_MINUTES = 1;
const MAX_INTERVAL_MINUTES = 1440;

const listeners = new Set<() => void>();

function isBrowser(): boolean {
  return typeof window !== "undefined" && typeof localStorage !== "undefined";
}

function clampIntervalMinutes(value: unknown): number {
  const n = typeof value === "number" ? value : Number(value);
  if (!Number.isFinite(n)) return defaultSettings.intervalMinutes;
  return Math.min(MAX_INTERVAL_MINUTES, Math.max(MIN_INTERVAL_MINUTES, Math.round(n)));
}

function normalizeSettings(raw: Partial<AutoRefreshSettings>): AutoRefreshSettings {
  return {
    enabled: typeof raw.enabled === "boolean" ? raw.enabled : defaultSettings.enabled,
    intervalMinutes: clampIntervalMinutes(raw.intervalMinutes),
  };
}

function loadSettings(): AutoRefreshSettings {
  if (!isBrowser()) return defaultSettings;
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored) return normalizeSettings({ ...defaultSettings, ...JSON.parse(stored) });
  } catch {
    /* noop */
  }
  return defaultSettings;
}

function saveSettings(settings: AutoRefreshSettings): void {
  if (!isBrowser()) return;
  localStorage.setItem(STORAGE_KEY, JSON.stringify(settings));
}

function notifyListeners(): void {
  listeners.forEach((fn) => fn());
}

export function useAutoRefreshSettings() {
  const [settings, setSettings] = useState<AutoRefreshSettings>(() => loadSettings());

  useEffect(() => {
    const sync = () => setSettings(loadSettings());
    listeners.add(sync);
    return () => {
      listeners.delete(sync);
    };
  }, []);

  const updateSettings = useCallback((partial: Partial<AutoRefreshSettings>) => {
    setSettings((prev) => {
      const next = normalizeSettings({ ...prev, ...partial });
      saveSettings(next);
      notifyListeners();
      return next;
    });
  }, []);

  return {
    settings,
    setEnabled: (enabled: boolean) => updateSettings({ enabled }),
    setIntervalMinutes: (intervalMinutes: number) => updateSettings({ intervalMinutes }),
  };
}
