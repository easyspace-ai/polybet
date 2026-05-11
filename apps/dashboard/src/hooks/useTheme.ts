import { useEffect, useState, useCallback } from 'react';

export type Theme = 'dark' | 'light';

const STORAGE_KEY = 'theme';

function getStoredTheme(): Theme {
  if (typeof window === 'undefined') return 'light';
  return (localStorage.getItem(STORAGE_KEY) as Theme) || 'light';
}

export { getStoredTheme, applyTheme };

function applyTheme(theme: Theme) {
  if (typeof document === 'undefined') return;
  const root = document.documentElement;
  root.classList.remove('light', 'dark');
  root.classList.add(theme);
}

function subscribeTheme(callback: (theme: Theme) => void): () => void {
  const handler = (e: StorageEvent) => {
    if (e.key === STORAGE_KEY && e.newValue) {
      callback(e.newValue as Theme);
    }
  };
  window.addEventListener('storage', handler);
  return () => window.removeEventListener('storage', handler);
}

export function useTheme(): [Theme, (theme: Theme) => void] {
  const [theme, setThemeState] = useState<Theme>(getStoredTheme);

  useEffect(() => {
    applyTheme(theme);
  }, [theme]);

  useEffect(() => {
    const unsub = subscribeTheme((t) => {
      setThemeState(t);
      applyTheme(t);
    });
    return unsub;
  }, []);

  const setTheme = useCallback((t: Theme) => {
    localStorage.setItem(STORAGE_KEY, t);
    setThemeState(t);
    applyTheme(t);
    window.dispatchEvent(new StorageEvent('storage', { key: STORAGE_KEY, newValue: t }));
  }, []);

  return [theme, setTheme];
}

export function useIsLight(): boolean {
  const [theme] = useTheme();
  return theme === 'light';
}