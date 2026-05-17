/** Defaults mirror server/internal/wsconfig seed values (seconds). */
export interface WSClientConfig {
  wsDashPingIntervalSec: number;
  wsDashPongTimeoutSec: number;
  wsDashBackoffBaseSec: number;
  wsDashBackoffMaxSec: number;
  wsDashBackoffJitterPct: number;
  wsDashSleepThresholdSec: number;
  wsRiskPollIntervalSec: number;
  wsAutoReconnectOnDisconnect: boolean;
  wsAutoRequestUpstreamReconnect: boolean;
}

export const DEFAULT_WS_CLIENT_CONFIG: WSClientConfig = {
  wsDashPingIntervalSec: 20,
  wsDashPongTimeoutSec: 10,
  wsDashBackoffBaseSec: 1,
  wsDashBackoffMaxSec: 60,
  wsDashBackoffJitterPct: 30,
  wsDashSleepThresholdSec: 5,
  wsRiskPollIntervalSec: 30,
  wsAutoReconnectOnDisconnect: true,
  wsAutoRequestUpstreamReconnect: true,
};

export function parseWSClientConfig(rows: { key: string; value: string }[]): WSClientConfig {
  const m = new Map(rows.map((r) => [r.key, r.value]));
  const int = (k: keyof WSClientConfig, def: number) => {
    const v = m.get(k);
    if (v == null || v === "") return def;
    const n = parseInt(v, 10);
    return Number.isFinite(n) ? n : def;
  };
  const bool = (k: "wsAutoReconnectOnDisconnect" | "wsAutoRequestUpstreamReconnect", def: boolean) => {
    const v = (m.get(k) ?? "").toLowerCase();
    if (v === "true" || v === "1") return true;
    if (v === "false" || v === "0") return false;
    return def;
  };
  return {
    wsDashPingIntervalSec: int("wsDashPingIntervalSec", DEFAULT_WS_CLIENT_CONFIG.wsDashPingIntervalSec),
    wsDashPongTimeoutSec: int("wsDashPongTimeoutSec", DEFAULT_WS_CLIENT_CONFIG.wsDashPongTimeoutSec),
    wsDashBackoffBaseSec: int("wsDashBackoffBaseSec", DEFAULT_WS_CLIENT_CONFIG.wsDashBackoffBaseSec),
    wsDashBackoffMaxSec: int("wsDashBackoffMaxSec", DEFAULT_WS_CLIENT_CONFIG.wsDashBackoffMaxSec),
    wsDashBackoffJitterPct: int("wsDashBackoffJitterPct", DEFAULT_WS_CLIENT_CONFIG.wsDashBackoffJitterPct),
    wsDashSleepThresholdSec: int("wsDashSleepThresholdSec", DEFAULT_WS_CLIENT_CONFIG.wsDashSleepThresholdSec),
    wsRiskPollIntervalSec: int("wsRiskPollIntervalSec", DEFAULT_WS_CLIENT_CONFIG.wsRiskPollIntervalSec),
    wsAutoReconnectOnDisconnect: bool("wsAutoReconnectOnDisconnect", true),
    wsAutoRequestUpstreamReconnect: bool("wsAutoRequestUpstreamReconnect", true),
  };
}
