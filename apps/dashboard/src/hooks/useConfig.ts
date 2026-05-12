import { useEffect, useState, useCallback } from 'react';
import { getConfig, putConfig, type ConfigRow } from '@/lib/api';

export function useConfig() {
  const [rows, setRows] = useState<ConfigRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState<string | null>(null);

  const refresh = useCallback(() => {
    setLoading(true);
    setError(null);
    getConfig()
      .then(setRows)
      .catch((err) => {
        setError(err instanceof Error ? err.message : '加载配置失败');
      })
      .finally(() => setLoading(false));
  }, []);

  const save = useCallback(async (key: string, value: string): Promise<void> => {
    setSaving(key);
    try {
      await putConfig(key, value);
      // After save, refresh to get updated data
      await getConfig().then(setRows);
    } catch (err) {
      throw err; // Re-throw so caller can handle
    } finally {
      setSaving(null);
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  return { rows, loading, error, saving, refresh, save };
}