import { useEffect, useState, useCallback } from "react";
import { getConfig } from "@/lib/api";
import { DEFAULT_WS_CLIENT_CONFIG, parseWSClientConfig, type WSClientConfig } from "@/lib/wsConfigDefaults";

const subscribers = new Set<(cfg: WSClientConfig) => void>();
let cached: WSClientConfig = { ...DEFAULT_WS_CLIENT_CONFIG };

function notify() {
  subscribers.forEach((fn) => fn({ ...cached }));
}

export function getWSConfig(): WSClientConfig {
  return { ...cached };
}

export function subscribeWSConfig(fn: (cfg: WSClientConfig) => void): () => void {
  subscribers.add(fn);
  fn({ ...cached });
  return () => subscribers.delete(fn);
}

export async function refreshWSConfigFromServer(): Promise<WSClientConfig> {
  const rows = await getConfig();
  cached = parseWSClientConfig(Array.isArray(rows) ? rows : []);
  notify();
  return { ...cached };
}

/** Call after putConfig saves a ws* key. */
export function applyWSConfigPatch(_key: string, _value: string) {
  void refreshWSConfigFromServer();
}

export function useWSConfig() {
  const [config, setConfig] = useState<WSClientConfig>(() => ({ ...cached }));

  useEffect(() => subscribeWSConfig(setConfig), []);

  const refresh = useCallback(async () => {
    await refreshWSConfigFromServer();
  }, []);

  return { config, refresh };
}

// bootstrap
if (typeof window !== "undefined") {
  void refreshWSConfigFromServer();
}
