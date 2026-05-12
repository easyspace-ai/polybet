import { useState, useEffect, useCallback } from 'react';

interface SoundSettings {
  enabled: boolean;
  buyEnabled: boolean;
  sellEnabled: boolean;
  alertEnabled: boolean;
}

const STORAGE_KEY = 'polybet-sound-settings';

const defaultSettings: SoundSettings = {
  enabled: true,
  buyEnabled: true,
  sellEnabled: true,
  alertEnabled: true,
};

function isBrowser(): boolean {
  return typeof window !== 'undefined' && typeof localStorage !== 'undefined';
}

function loadSettings(): SoundSettings {
  if (!isBrowser()) return defaultSettings;
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored) {
      return { ...defaultSettings, ...JSON.parse(stored) };
    }
  } catch {}
  return defaultSettings;
}

function saveSettings(settings: SoundSettings): void {
  if (!isBrowser()) return;
  localStorage.setItem(STORAGE_KEY, JSON.stringify(settings));
}

export function useSoundSettings() {
  const [settings, setSettings] = useState<SoundSettings>(() => loadSettings());

  const updateSettings = useCallback((partial: Partial<SoundSettings>) => {
    setSettings((prev) => {
      const next = { ...prev, ...partial };
      saveSettings(next);
      return next;
    });
  }, []);

  return {
    settings,
    setEnabled: (enabled: boolean) => updateSettings({ enabled }),
    setBuyEnabled: (buyEnabled: boolean) => updateSettings({ buyEnabled }),
    setSellEnabled: (sellEnabled: boolean) => updateSettings({ sellEnabled }),
    setAlertEnabled: (alertEnabled: boolean) => updateSettings({ alertEnabled }),
  };
}