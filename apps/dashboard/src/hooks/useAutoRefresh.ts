import { useEffect, useRef } from "react";
import { useAutoRefreshSettings } from "./useAutoRefreshSettings";

export function useAutoRefresh() {
  const { settings } = useAutoRefreshSettings();
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    if (timerRef.current) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }

    if (!settings.enabled) return;

    const ms = settings.intervalMinutes * 60 * 1000;
    if (ms <= 0) return;

    timerRef.current = setTimeout(() => {
      window.location.reload();
    }, ms);

    return () => {
      if (timerRef.current) {
        clearTimeout(timerRef.current);
        timerRef.current = null;
      }
    };
  }, [settings.enabled, settings.intervalMinutes]);
}
