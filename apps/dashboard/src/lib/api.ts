export interface MarketOutcome {
  id: string;
  label: string;
  platform: string;
  externalId?: string;
  impliedOdds: number;
  availableSize: number;
  lastUpdated: string;
  canonicalKey?: string | null;
}

export interface Market {
  id: string;
  platform: string;
  externalId: string;
  sport: string;
  league: string;
  name: string;
  startTime: string;
  status: string;
  betType?: string;
  line?: number | null;
  mainLine?: boolean;
  polySlug?: string;
  iconUrl?: string;
  outcomes: MarketOutcome[];
}

export interface Allocation {
  platform: string;
  outcomeId: string;
  externalMarketId: string;
  externalOutcomeId: string;
  size: number;
  expectedOdds: number;
  estimatedSlippage: number;
}

export interface AllocationPlan {
  allocations: Allocation[];
  totalSize: number;
  weightedOdds: number;
  totalSlippage: number;
}

export interface TradeResult {
  tradeId: string;
  status: string;
  platform: string;
  txHash?: string;
  failureReason?: string;
}

export interface TradeResponse {
  status: string;
  message?: string;
  trades: TradeResult[];
  plan: AllocationPlan;
}

export interface Trade {
  id: string;
  createdAt: string;
  marketName: string;
  outcomeLabel: string;
  platform: string;
  side: string;
  requestedSize: number;
  executedSize: number | null;
  requestedOdds: number;
  fillOdds: number | null;
  status: string;
  txHash: string | null;
  failureReason: string | null;
  officialUrl?: string | null;
  sport?: string | null;
  iconUrl?: string;
}

export interface TradesResponse {
  total: number;
  page: number;
  limit: number;
  trades: Trade[];
}

export interface OrderBookLevel {
  odds: number;
  size: number;
  platform: "polymarket";
}

export interface OrderBookResponse {
  levels: OrderBookLevel[];
  polyTokenId?: string;
}

export interface ConfigRow {
  key: string;
  value: string;
}

export interface PolymarketAccountBalanceRow {
  id: string;
  name: string;
  isActive: boolean;
  polymarket: number | null;
}

export interface BalanceSummary {
  polymarket: number | null;
  polymarketAccounts: PolymarketAccountBalanceRow[];
}

export interface PolymarketAccountListItem {
  id: string;
  name: string;
  funderAddress: string;
  isActive: boolean;
  createdAt: string;
}

export interface PolymarketAccountCreateBody {
  name: string;
  privateKey: string;
}

const BASE = (() => {
  const env = import.meta.env.VITE_API_BASE_URL as string | undefined;
  if (env) return env;
  if (typeof window !== "undefined" && window.location.protocol === "file:") {
    return "http://127.0.0.1:7633";
  }
  return "";
})();

/** Short API error tokens that should not be shown alone as user-facing text. */
const OPAQUE_API_ERROR_TOKENS = new Set(["db", "ok", "risk"]);

function formatApiErrorBody(body: unknown, status: number): string {
  if (!body || typeof body !== "object") {
    return `HTTP ${status}`;
  }
  const o = body as { detail?: unknown; message?: unknown; error?: unknown };
  const detail = typeof o.detail === "string" ? o.detail.trim() : "";
  const message = typeof o.message === "string" ? o.message.trim() : "";
  const errTok = typeof o.error === "string" ? o.error.trim() : "";
  if (message) {
    return message;
  }
  if (detail) {
    return detail;
  }
  if (errTok) {
    const low = errTok.toLowerCase();
    if (!OPAQUE_API_ERROR_TOKENS.has(low) && errTok.length > 3) {
      return errTok;
    }
  }
  return `HTTP ${status}`;
}

const DEFAULT_FETCH_TIMEOUT_MS = 20_000;
const WS_STATUS_FETCH_TIMEOUT_MS = 3_000;

type ApiFetchOptions = RequestInit & { timeoutMs?: number };

async function apiFetch<T>(url: string, options?: ApiFetchOptions): Promise<T> {
  const { timeoutMs, ...fetchOptions } = options ?? {};
  const controller = new AbortController();
  const limitMs = timeoutMs ?? DEFAULT_FETCH_TIMEOUT_MS;
  const timeout = setTimeout(() => controller.abort(), limitMs);
  const signal = fetchOptions.signal ?? controller.signal;
  try {
    const res = await fetch(`${BASE}${url}`, { ...fetchOptions, signal });
    if (!res.ok) {
      const body = await res.json().catch(() => ({}));
      throw new Error(formatApiErrorBody(body, res.status));
    }
    return res.json() as Promise<T>;
  } catch (err) {
    if (err instanceof DOMException && err.name === "AbortError") {
      throw new Error(`请求超时（${limitMs / 1000}s）: ${url}`);
    }
    throw err;
  } finally {
    clearTimeout(timeout);
  }
}

export const getMarkets = () => apiFetch<Market[]>("/api/markets");

export const getOrderBook = (outcomeId: string) =>
  apiFetch<OrderBookResponse>(`/api/trade/orderbook?outcomeId=${encodeURIComponent(outcomeId)}`);

export const getTradePreview = (outcomeId: string, side: string, size: number) =>
  apiFetch<AllocationPlan>(
    `/api/trade/preview?outcomeId=${encodeURIComponent(outcomeId)}&side=${encodeURIComponent(side)}&size=${size}`,
  );

export const postTrade = (outcomeId: string, side: string, size: number) =>
  apiFetch<TradeResponse>("/api/trade", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ outcomeId, side, size }),
  });

export const getTrades = (page = 1, limit = 20) =>
  apiFetch<TradesResponse>(`/api/trades?page=${page}&limit=${limit}`);

export const getConfig = () => apiFetch<ConfigRow[]>("/api/config");

export interface WSStatusResponse {
  dashConnected?: boolean;
  dashClients?: number;
  polyOrderbookConnected?: boolean;
  polyOrderbookConnecting?: boolean;
  polyUserConnected?: boolean;
  polyUserConnecting?: boolean;
  openPositionsCount?: number;
  orderbookNextRetryAt?: number;
  orderbookReconnectAttempt?: number;
  userNextRetryAt?: number;
  userReconnectAttempt?: number;
  userWsLastIssue?: string;
  lastBookUpdateMs?: number;
  wsEvents?: { channel?: string; at?: string; level?: string; message?: string }[];
}

let wsStatusApiInflight: Promise<WSStatusResponse> | null = null;

export const getWSStatus = (opts?: { signal?: AbortSignal }) => {
  if (wsStatusApiInflight) return wsStatusApiInflight;
  wsStatusApiInflight = apiFetch<WSStatusResponse>("/api/ws/status", {
    timeoutMs: WS_STATUS_FETCH_TIMEOUT_MS,
    signal: opts?.signal,
  }).finally(() => {
    wsStatusApiInflight = null;
  });
  return wsStatusApiInflight;
};

export const postWSReconnect = async (
  channel: "orderbook" | "user" | "all" = "all",
): Promise<{ ok: boolean; accepted?: boolean }> => {
  const controller = new AbortController();
  const timeoutMs = 5_000;
  const timeout = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const res = await fetch(`${BASE}/api/ws/reconnect`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ channel }),
      signal: controller.signal,
    });
    const body = (await res.json().catch(() => ({}))) as {
      ok?: boolean;
      accepted?: boolean;
      detail?: string;
      message?: string;
      error?: string;
    };
    // Server returns 202 Accepted with { ok, accepted }; treat 2xx explicitly.
    if (res.status === 202 || res.ok) {
      return { ok: body.ok ?? true, accepted: body.accepted };
    }
    throw new Error(formatApiErrorBody(body, res.status));
  } catch (err) {
    if (err instanceof DOMException && err.name === "AbortError") {
      throw new Error(`请求超时（${timeoutMs / 1000}s）: /api/ws/reconnect`);
    }
    throw err;
  } finally {
    clearTimeout(timeout);
  }
};

export const getBalances = () => apiFetch<BalanceSummary>("/api/balances");

export const listPolymarketAccounts = () =>
  apiFetch<PolymarketAccountListItem[]>("/api/polymarket/accounts");

export const createPolymarketAccount = (body: PolymarketAccountCreateBody) =>
  apiFetch<{ id: string; name: string; funderAddress: string; isActive: boolean }>(
    "/api/polymarket/accounts",
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    },
  );

export const activatePolymarketAccount = (id: string) =>
  apiFetch<{ ok: boolean; id: string }>(
    `/api/polymarket/accounts/${encodeURIComponent(id)}/activate`,
    {
      method: "PATCH",
    },
  );

export const deletePolymarketAccount = async (id: string): Promise<void> => {
  const res = await fetch(`${BASE}/api/polymarket/accounts/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(formatApiErrorBody(body, res.status));
  }
};

export interface BestOddsCount {
  poly: number;
  total: number;
}

export interface WinnerEdgeDepth {
  venue: "poly";
  avgSize: number;
  sampleCount: number;
}

export interface MarketStatsResponse {
  bestOddsMatched24h: BestOddsCount;
  bestOddsAllMatched24h: BestOddsCount;
  edgeMatched24h: WinnerEdgeDepth | null;
  edgeAllMatched24h: WinnerEdgeDepth | null;
}

export const getMarketStats = () => apiFetch<MarketStatsResponse>("/api/stats/markets");

export interface RiskPositionRow {
  id: string;
  /** Monotonic display ID assigned when the position enters monitoring. */
  positionSeq?: number;
  title: string;
  displayTitle?: string;
  sport?: string;
  officialUrl?: string;
  officialSearchUrl?: string;
  polySlug?: string;
  imageUrl?: string;
  iconUrl?: string;
  sideLabel: string;
  tokenId: string;
  avgEntryCents: number;
  currentCents: number | null;
  sizeShares: number;
  costUsd: number;
  highWaterCents: number;
  stopLossPct: number;
  trailingStopCents: number;
  valueUsd: number | null;
  pnlUsd: number | null;
  maxPayoffUsd: number;
  potentialProfitUsd: number;
  status: string;
  source?: string;
  bids?: OrderBookLevel[];
  asks?: OrderBookLevel[];
  bookSub?: RiskBookSubscriptionStatus;
}

export interface RiskBookSubscriptionStatus {
  tokenId: string;
  clientSubscribed: boolean;
  clientRefs: number;
  upstreamSubscribed: boolean;
  lastFrameMs?: number;
  stale: boolean;
}

export interface RiskPositionsMeta {
  userWsConnected: boolean;
  userWsConnecting?: boolean;
  userWsLastMessageAt: string | null;
  restTradesSyncLastAt: string | null;
  userWsLastIssue?: string | null;
  outboundProxyConfigured?: boolean;
  minOpenRiskShares?: number;
  /** fok_sell | fak_sell | hedge_fok_buy */
  riskCloseExecutionMode?: string;
  riskCloseFakWorstPrice?: number;
  /** notional | shares (hedge mode) */
  riskHedgeBuySizing?: string;
}

export interface RiskTaskRow {
  id: string;
  type: string;
  positionId: string | null;
  status: string;
  attempts: number;
  lastError: string | null;
  /** server: stop_loss | manual | null */
  reason?: string | null;
  /** RFC3339nano */
  createdAt?: string;
  nextRunAt: string;
  updatedAt: string;
  /** JSON string: last FOK submit / abort snapshot (limit price, shares, book, trail). */
  lastAttemptDetail?: string | null;
}

type RiskPositionsResponse = {
  positions: RiskPositionRow[];
  meta?: RiskPositionsMeta;
  cached?: boolean;
  stale?: boolean;
};

let riskPositionsApiInflight: Promise<RiskPositionsResponse> | null = null;

export const getRiskPositions = () => {
  if (riskPositionsApiInflight) return riskPositionsApiInflight;
  riskPositionsApiInflight = apiFetch<RiskPositionsResponse>("/api/risk/positions").finally(() => {
    riskPositionsApiInflight = null;
  });
  return riskPositionsApiInflight;
};

export interface RiskBookResponse {
  tokenId: string;
  bids?: OrderBookLevel[];
  asks?: OrderBookLevel[];
  bestBid?: number;
  bestAsk?: number;
  source?: "cache" | "rest" | "rest_error" | string;
  updatedAtMs?: number;
  bookAgeMs?: number;
  subscription?: RiskBookSubscriptionStatus;
}

export const getRiskBook = (tokenId: string, opts?: { refresh?: boolean; reason?: string }) => {
  const q = new URLSearchParams({ tokenId });
  if (opts?.refresh) q.set("refresh", "1");
  if (opts?.reason) q.set("reason", opts.reason);
  return apiFetch<RiskBookResponse>(`/api/risk/book?${q.toString()}`, { timeoutMs: 8_000 });
};

export const getRiskBookSubscriptions = (tokenIds?: string[]) => {
  const q = new URLSearchParams();
  if (tokenIds && tokenIds.length > 0) {
    q.set("tokenIds", tokenIds.join(","));
  }
  const suffix = q.toString() ? `?${q.toString()}` : "";
  return apiFetch<{ subscriptions: RiskBookSubscriptionStatus[] }>(
    `/api/risk/book-subscriptions${suffix}`,
    { timeoutMs: 8_000 },
  );
};

export const getRiskTasks = (limit = 40) =>
  apiFetch<{ tasks: RiskTaskRow[] }>(`/api/risk/tasks?limit=${limit}`);

/** Removes terminal task rows (succeeded / failed / cancelled); pending & running are kept. */
export const postRiskTasksClear = () =>
  apiFetch<{ ok: boolean; deleted: number }>("/api/risk/tasks/clear", { method: "POST" });

export const postRiskOfficialRefresh = () =>
  apiFetch<{ ok: boolean; accepted?: boolean; alreadyRunning?: boolean; message?: string }>(
    "/api/risk/refresh",
    { method: "POST" },
  );

export const patchRiskPosition = (
  id: string,
  body: { stopLossPct?: number; highWaterCents?: number },
) =>
  apiFetch<{ ok: boolean; position: RiskPositionRow }>(
    `/api/risk/positions/${encodeURIComponent(id)}`,
    {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    },
  );

export const postRiskClosePosition = (id: string) =>
  apiFetch<{ ok: boolean; positionId: string }>(
    `/api/risk/positions/${encodeURIComponent(id)}/close`,
    { method: "POST" },
  );

export interface StopLossHistoryTask {
  id: string;
  type: string;
  positionId: string | null;
  status: string;
  attempts: number;
  lastError: string | null;
  reason: string | null;
  createdAt: string;
  nextRunAt: string;
  updatedAt: string;
  title?: string;
  officialUrl?: string;
}

export interface OfficialTrade {
  id: string;
  side: string;
  title: string;
  outcome: string;
  size: number;
  price: number;
  priceCents: number;
  timestamp: string;
  icon: string;
  polySlug?: string;
  officialUrl?: string;
}

export const getStopLossHistory = (limit = 50, refresh = false) =>
  apiFetch<{ tasks: StopLossHistoryTask[] }>(
    `/api/risk/stop-loss-history?limit=${limit}${refresh ? "&refresh=true" : ""}`,
  );

export const getTradeHistory = (limit = 50, refresh = false) =>
  apiFetch<{ trades: OfficialTrade[] }>(
    `/api/risk/trade-history?limit=${limit}${refresh ? "&refresh=true" : ""}`,
  );

export const postRiskCloseAll = () =>
  apiFetch<{ ok: boolean }>("/api/risk/close-all", { method: "POST" });

export interface RiskHiddenRow {
  tokenId: string;
  sideLabel: string;
  createdAt: string;
}

export const getRiskHiddenPositions = () =>
  apiFetch<{ hidden: RiskHiddenRow[] }>("/api/risk/hidden-positions");

export const postRiskHidePosition = (body: { tokenId: string; sideLabel: string }) =>
  apiFetch<{ ok: boolean }>("/api/risk/hidden-positions", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });

export const deleteRiskUnhidePosition = (body: { tokenId: string; sideLabel: string }) =>
  apiFetch<{ ok: boolean }>("/api/risk/hidden-positions", {
    method: "DELETE",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });

export const putConfig = (key: string, value: string) =>
  apiFetch<ConfigRow>(`/api/config/${encodeURIComponent(key)}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ value }),
  });

export const testTelegram = () =>
  apiFetch<{ ok: boolean; message: string }>("/api/telegram/test", {
    method: "POST",
  });

export interface SetupStatus {
  needsOnboarding: boolean;
  proxyConfigured: boolean;
  polymarketConfigured: boolean;
}

export const getSetupStatus = () => apiFetch<SetupStatus>("/api/setup/status");

export const postSetupComplete = () =>
  apiFetch<{ ok: boolean }>("/api/setup/complete", { method: "POST" });

export const postCacheRefresh = () =>
  apiFetch<{ ok: boolean; message: string }>("/api/cache/refresh", { method: "POST" });

export const postMarketsRefresh = () =>
  apiFetch<{ ok: boolean; message: string }>("/api/markets/refresh?force=1", { method: "POST" });

export const postMarketsRefreshFull = () =>
  apiFetch<{
    ok: boolean;
    accepted?: boolean;
    alreadyRunning?: boolean;
    message?: string;
    cache?: string;
  }>("/api/markets/refresh-full", { method: "POST" });

export interface GammaSport {
  id: number;
  sport: string;
  image: string;
  resolution: string;
  ordering: string;
  tags: string;
  series: string;
  createdAt: string;
}

export const getSports = () => apiFetch<GammaSport[]>("/api/sports");

export interface RiskRuntimeLogEnvelope {
  seq: number;
  ts: string;
  type: string;
  category: string;
  severity: string;
  accountId: string | null;
  marketId: string | null;
  tokenId: string | null;
  correlationId: string;
  detail: Record<string, unknown>;
}

export const getRiskRuntimeLogs = (limit = 100) =>
  apiFetch<{ logs: RiskRuntimeLogEnvelope[] }>(`/api/risk/runtime-logs?limit=${limit}`);

// --- Monitor / connectivity (next-gen risk UI) ---

export interface ConnectivitySnapshotResponse extends WSStatusResponse {
  connectivityOwner?: string;
  userDisplay?: string;
  orderbookDisplay?: string;
  subscribedTokenCount?: number;
  lastClientHeartbeat?: string;
}

export const getConnectivitySnapshot = (opts?: { signal?: AbortSignal }) =>
  apiFetch<ConnectivitySnapshotResponse>("/api/connectivity/snapshot", {
    timeoutMs: WS_STATUS_FETCH_TIMEOUT_MS,
    signal: opts?.signal,
  });

export interface MonitorClobSession {
  apiKey: string;
  apiSecret: string;
  apiPassphrase: string;
  marketWsUrl: string;
  userWsUrl: string;
  accountId?: string;
  proxyUrl?: string;
}

export const getMonitorClobSession = () =>
  apiFetch<MonitorClobSession>("/api/monitor/clob-session", { timeoutMs: 10_000 });

export const postMonitorHeartbeat = (body: {
  userConnected: boolean;
  orderbookConnected: boolean;
  subscribedTokens: string[];
}) =>
  apiFetch<{ ok: boolean }>("/api/monitor/heartbeat", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });

export const postMonitorStopLossTrigger = (body: {
  positionId: string;
  tokenId?: string;
  triggerCents: number;
  trailCents: number;
}) =>
  apiFetch<{ ok: boolean; positionId: string; taskId?: string }>(
    "/api/monitor/stop-loss/trigger",
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    },
  );

export const postMonitorPositionsSync = () =>
  apiFetch<{ ok: boolean }>("/api/monitor/positions/sync", { method: "POST" });

const monitorPath = (p: string) => `/api/monitor${p}`;

export const getMonitorPositions = () => {
  if (riskPositionsApiInflight) return riskPositionsApiInflight;
  riskPositionsApiInflight = apiFetch<RiskPositionsResponse>(monitorPath("/positions")).finally(
    () => {
      riskPositionsApiInflight = null;
    },
  );
  return riskPositionsApiInflight;
};

export const getMonitorBook = (tokenId: string, opts?: { refresh?: boolean; reason?: string }) => {
  const q = new URLSearchParams({ tokenId });
  if (opts?.refresh) q.set("refresh", "1");
  if (opts?.reason) q.set("reason", opts.reason);
  return apiFetch<RiskBookResponse>(`${monitorPath("/book")}?${q.toString()}`, { timeoutMs: 8_000 });
};

export const getMonitorTasks = (limit = 40) =>
  apiFetch<{ tasks: RiskTaskRow[] }>(`${monitorPath("/tasks")}?limit=${limit}`);

export const postMonitorTasksClear = () =>
  apiFetch<{ ok: boolean; deleted: number }>(monitorPath("/tasks/clear"), { method: "POST" });

export const postMonitorOfficialRefresh = () =>
  apiFetch<{ ok: boolean; accepted?: boolean; alreadyRunning?: boolean; message?: string }>(
    monitorPath("/refresh"),
    { method: "POST" },
  );

export const patchMonitorPosition = (
  id: string,
  body: { stopLossPct?: number; highWaterCents?: number },
) =>
  apiFetch<{ ok: boolean; position: RiskPositionRow }>(
    monitorPath(`/positions/${encodeURIComponent(id)}`),
    {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    },
  );

export const postMonitorClosePosition = (id: string) =>
  apiFetch<{ ok: boolean; positionId: string }>(
    monitorPath(`/positions/${encodeURIComponent(id)}/close`),
    { method: "POST" },
  );

export const postMonitorCloseAll = () =>
  apiFetch<{ ok: boolean }>(monitorPath("/close-all"), { method: "POST" });

export const postMonitorHidePosition = (body: { tokenId: string; sideLabel: string }) =>
  apiFetch<{ ok: boolean }>(monitorPath("/hidden-positions"), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });

export const getMonitorRuntimeLogs = (limit = 100) =>
  apiFetch<{ logs: RiskRuntimeLogEnvelope[] }>(`${monitorPath("/runtime-logs")}?limit=${limit}`);
