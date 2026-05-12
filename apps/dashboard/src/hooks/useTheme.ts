import { useEffect, useState, useCallback } from 'react';

export type Theme = 'light' | 'dark' | 'system';

const STORAGE_KEY = 'polybet-theme';

function getSystemTheme(): 'light' | 'dark' {
  if (typeof window === 'undefined') return 'dark';
  return window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
}

function getStoredTheme(): Theme {
  if (typeof window === 'undefined') return 'system';
  const raw = localStorage.getItem(STORAGE_KEY) as Theme | null;
  if (raw === 'light' || raw === 'dark' || raw === 'system') return raw;
  return 'system';
}

function getResolvedTheme(theme: Theme): 'light' | 'dark' {
  return theme === 'system' ? getSystemTheme() : theme;
}

function applyThemeClass(resolved: 'light' | 'dark') {
  if (typeof document === 'undefined') return;
  const html = document.documentElement;
  html.classList.remove('light', 'dark');
  html.classList.add(resolved);
}

export function useTheme(): { theme: Theme; resolvedTheme: 'light' | 'dark'; setTheme: (t: Theme) => void } {
  const [theme, setThemeState] = useState<Theme>(() => getStoredTheme());
  const [resolvedTheme, setResolvedTheme] = useState<'light' | 'dark'>(() => getResolvedTheme(getStoredTheme()));

  useEffect(() => {
    applyThemeClass(resolvedTheme);
  }, [resolvedTheme]);

  useEffect(() => {
    const mq = window.matchMedia('(prefers-color-scheme: light)');
    const onChange = () => {
      if (theme === 'system') {
        const resolved = getSystemTheme();
        setResolvedTheme(resolved);
        applyThemeClass(resolved);
      }
    };
    mq.addEventListener('change', onChange);
    return () => mq.removeEventListener('change', onChange);
  }, [theme]);

  useEffect(() => {
    const handler = (e: StorageEvent) => {
      if (e.key === STORAGE_KEY && e.newValue) {
        const t = e.newValue as Theme;
        setThemeState(t);
        setResolvedTheme(getResolvedTheme(t));
      }
    };
    window.addEventListener('storage', handler);
    return () => window.removeEventListener('storage', handler);
  }, []);

  const setTheme = useCallback((t: Theme) => {
    localStorage.setItem(STORAGE_KEY, t);
    setThemeState(t);
    const resolved = getResolvedTheme(t);
    setResolvedTheme(resolved);
    applyThemeClass(resolved);
    window.dispatchEvent(new StorageEvent('storage', { key: STORAGE_KEY, newValue: t }));
  }, []);

  return { theme, resolvedTheme, setTheme };
}
