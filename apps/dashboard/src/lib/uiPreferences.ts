import { useCallback, useEffect, useState } from 'react';

export type UiFontScale = 'compact' | 'normal' | 'comfortable';
export type UiTextContrast = 'normal' | 'strong';

export interface UiPreferences {
  fontScale: UiFontScale;
  textContrast: UiTextContrast;
}

const STORAGE_KEY = 'polybet-ui-prefs';

const DEFAULTS: UiPreferences = {
  fontScale: 'normal',
  textContrast: 'normal',
};

function readPrefs(): UiPreferences {
  if (typeof window === 'undefined') return DEFAULTS;
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return DEFAULTS;
    const o = JSON.parse(raw) as Partial<UiPreferences>;
    const fontScale =
      o.fontScale === 'compact' || o.fontScale === 'comfortable' || o.fontScale === 'normal'
        ? o.fontScale
        : DEFAULTS.fontScale;
    const textContrast =
      o.textContrast === 'strong' || o.textContrast === 'normal'
        ? o.textContrast
        : DEFAULTS.textContrast;
    return { fontScale, textContrast };
  } catch {
    return DEFAULTS;
  }
}

function applyPrefsToDocument(prefs: UiPreferences) {
  if (typeof document === 'undefined') return;
  const el = document.documentElement;
  el.dataset.uiFont = prefs.fontScale;
  el.dataset.uiContrast = prefs.textContrast;
}

export function useUiPreferences() {
  const [prefs, setPrefsState] = useState<UiPreferences>(() => readPrefs());

  useEffect(() => {
    applyPrefsToDocument(prefs);
  }, [prefs]);

  const setPrefs = useCallback((next: Partial<UiPreferences>) => {
    setPrefsState((prev) => {
      const merged = { ...prev, ...next };
      try {
        localStorage.setItem(STORAGE_KEY, JSON.stringify(merged));
      } catch {
        /* ignore */
      }
      applyPrefsToDocument(merged);
      return merged;
    });
  }, []);

  return { prefs, setPrefs };
}
