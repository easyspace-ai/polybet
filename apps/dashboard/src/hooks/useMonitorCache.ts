import { useEffect, useState, useCallback } from "react";
import {
  getMonitorPositions,
  getMonitorTasks,
  type RiskBookSubscriptionStatus,
  type RiskPositionRow,
  type RiskPositionsMeta,
  type RiskTaskRow,
} from "@/lib/api";
import { runUnifiedMarketsRefresh } from "@/lib/unifiedRefresh";
import { normalizeTokenId } from "@/lib/clobTokenId";
import { monitorCoordinator } from "@/lib/monitor/coordinator";
import { floorCents1, isTrailingStopActive, trailingStopCentsFromHW } from "@/lib/cents";
import { bestBidCentsFromBookFrame, mergePolyBookFrame, topOfBookMarkCents } from "@/lib/riskBook";
import {
  riskWsBus,
  wsBus,
  type BookLevel,
  type PositionUpdateMessage,
  type PolyBookFrame,
  type RiskRuntimeLogMessage,
} from "@/lib/wsBus";
import { getWSConfig } from "@/hooks/useWSConfig";
import {
  clearTokenBookMeta,
  markTokenSubscribed,
  onWsBookFrame,
  startBookFallbackPoller,
} from "@/lib/riskBookFallback";
import {
  reconcileAvgEntryFallback,
  setAvgEntryFallbackHidden,
  startAvgEntryFallbackPoller,
} from "@/lib/avgEntryFallback";
import type { RiskBookResponse } from "@/lib/api";

interface RiskState {
  positions: RiskPositionRow[];
  meta: RiskPositionsMeta | null;
  tasks: RiskTaskRow[];
  loading: boolean;
  error: string | null;
  lastRefresh: Date | null;
  polyOrderbookConnected: boolean;
  polyUserConnected: boolean;
  bookSubByToken: Record<string, RiskBookSubscriptionStatus>;
}

const cache: RiskState = {
  positions: [],
  meta: null,
  tasks: [],
  loading: true,
  error: null,
  lastRefresh: null,
  polyOrderbookConnected: false,
  polyUserConnected: false,
  bookSubByToken: {},
};

const subscribers = new Set<(state: RiskState) => void>();
const tokenBookMap = new Map<string, PolyBookFrame>();
const tokenSubs = new Map<string, { unsubDash: () => void }>();
const bookSubByToken = new Map<string, RiskBookSubscriptionStatus>();
const bookReconnectLastAt = new Map<string, number>();

const BOOK_SUB_STALE_MS = 15_000;
const BOOK_SUB_RECONNECT_DEBOUNCE_MS = 10_000;
const BOOK_SUB_POLL_MS = 8_000;

function bookSubRowForToken(tid: string, lastFrameMs?: number): RiskBookSubscriptionStatus {
  const now = Date.now();
  const lastMs = lastFrameMs ?? bookSubByToken.get(tid)?.lastFrameMs ?? 0;
  const obLive = monitorCoordinator.isOrderbookConnected();
  const stale = !obLive || lastMs <= 0 || now - lastMs > BOOK_SUB_STALE_MS;
  return {
    tokenId: tid,
    clientSubscribed: true,
    upstreamSubscribed: obLive,
    clientRefs: 1,
    stale,
    lastFrameMs: lastMs > 0 ? lastMs : undefined,
  };
}

function syncConnectionFromCoordinator() {
  const ob = monitorCoordinator.isOrderbookConnected();
  const obWas = cache.polyOrderbookConnected;
  cache.polyOrderbookConnected = ob;
  cache.polyUserConnected = monitorCoordinator.isUserConnected();
  if (ob !== obWas) {
    if (ob) syncBookSubMeta();
    else {
      bookSubByToken.clear();
      attachBookSubToPositions();
      cache.bookSubByToken = {};
      notifySubscribers();
    }
  } else if (ob) {
    refreshBookSubUpstreamFlags();
  }
}

/** Keep row-level bookSub in sync when OB is live (avoids stale upstreamSubscribed). */
function refreshBookSubUpstreamFlags() {
  const obLive = monitorCoordinator.isOrderbookConnected();
  if (!obLive) return;
  let changed = false;
  for (const [tid, row] of bookSubByToken) {
    if (!row.upstreamSubscribed || row.stale) {
      const lastMs = row.lastFrameMs ?? Date.now();
      bookSubByToken.set(tid, bookSubRowForToken(tid, lastMs));
      changed = true;
    }
  }
  if (changed) {
    attachBookSubToPositions();
    cache.bookSubByToken = Object.fromEntries(bookSubByToken.entries());
    notifySubscribers();
  }
}

function attachBookSubToPositions() {
  for (const pos of cache.positions) {
    if (!pos.tokenId) continue;
    const tid = normalizeTokenId(pos.tokenId);
    const row = bookSubByToken.get(tid);
    if (row) {
      pos.bookSub = row;
    } else {
      delete pos.bookSub;
    }
  }
}

function syncBookSubMeta() {
  const now = Date.now();
  const subscribed = new Set(
    monitorCoordinator.getSubscribedTokens().map((t) => normalizeTokenId(t)),
  );
  const seen = new Set<string>();

  for (const tid of subscribed) {
    seen.add(tid);
    const lastMs = Math.max(bookSubByToken.get(tid)?.lastFrameMs ?? 0, now);
    bookSubByToken.set(tid, bookSubRowForToken(tid, lastMs));
  }

  for (const pos of cache.positions) {
    if (pos.status !== "open" || !pos.tokenId) continue;
    const tid = normalizeTokenId(pos.tokenId);
    if (!tokenBookMap.has(tid)) continue;
    seen.add(tid);
    const lastMs = Math.max(bookSubByToken.get(tid)?.lastFrameMs ?? 0, now);
    bookSubByToken.set(tid, bookSubRowForToken(tid, lastMs));
  }

  for (const tid of bookSubByToken.keys()) {
    if (!seen.has(tid)) bookSubByToken.delete(tid);
  }

  attachBookSubToPositions();
  cache.bookSubByToken = Object.fromEntries(bookSubByToken.entries());
  notifySubscribers();
}

function notifySubscribers() {
  cache.bookSubByToken = Object.fromEntries(bookSubByToken.entries());
  subscribers.forEach((fn) => fn({ ...cache }));
}

function subscribe(fn: (state: RiskState) => void) {
  subscribers.add(fn);
  return () => subscribers.delete(fn);
}

/** Best bid in cents (what you can sell at now). */
function getBestBidCentsFromFrame(frame: PolyBookFrame | undefined): number | null {
  return bestBidCentsFromBookFrame(frame);
}

/** Top of book “high” for trailing watermark — matches server max(bid, ask). */
function getTopOfBookMarkCents(frame: PolyBookFrame | undefined): number | null {
  return topOfBookMarkCents(frame);
}

const polyPlatform = "polymarket" as const;

function mergeBookSide(
  incoming: BookLevel[] | null | undefined,
  existing: BookLevel[] | undefined,
  sortBids: boolean,
  bestCents: number | undefined,
): BookLevel[] | undefined {
  if (incoming && incoming.length > 0) {
    const sorted = [...incoming].sort((a, b) => (sortBids ? b.odds - a.odds : a.odds - b.odds));
    return sorted.slice(0, 5);
  }
  if (incoming === undefined || incoming === null) {
    return existing;
  }
  // Empty ladder in payload (e.g. best_bid_ask-only WS tick) — do not wipe cached depth.
  if (incoming.length === 0) {
    const hasTop = typeof bestCents === "number" && bestCents > 0;
    if (hasTop) {
      if (existing && existing.length > 0) return existing;
      return [{ odds: bestCents / 100, size: 0, platform: polyPlatform }];
    }
    return existing ?? [];
  }
  return existing;
}

function updatePositionsFromBook() {
  if (cache.positions.length === 0) return;
  let changed = false;
  for (const pos of cache.positions) {
    if (pos.status !== "open" || !pos.tokenId) continue;
    const tid = normalizeTokenId(pos.tokenId);
    const frame = tokenBookMap.get(tid);
    if (!frame) continue;

    // 更新 bids/asks 盘口数据
    // 注意：这里需要浅拷贝以触发 React 更新（如果 UI 依赖引用变化）
    if (frame.bids && JSON.stringify(frame.bids) !== JSON.stringify(pos.bids)) {
      pos.bids = [...frame.bids];
      changed = true;
    }
    if (frame.asks && JSON.stringify(frame.asks) !== JSON.stringify(pos.asks)) {
      pos.asks = [...frame.asks];
      changed = true;
    }

    const bid = getBestBidCentsFromFrame(frame);
    const mark = getTopOfBookMarkCents(frame);
    // 「当前价」= 买一（best bid），与 Bid 列同源
    if (bid != null && bid !== pos.currentCents) {
      pos.currentCents = bid;
      changed = true;
    }
    if (isTrailingStopActive(pos)) {
      const effHw = floorCents1(Math.max(floorCents1(pos.highWaterCents), mark ?? 0));
      const trail = trailingStopCentsFromHW(effHw, pos.stopLossPct);
      if (pos.trailingStopCents !== trail) {
        pos.trailingStopCents = trail;
        changed = true;
      }
    } else if (pos.trailingStopCents != null && pos.trailingStopCents !== 0) {
      pos.trailingStopCents = 0;
      changed = true;
    }

  }
  if (changed) {
    cache.lastRefresh = new Date();
    notifySubscribers();
  }
}

function mergePositionRows(rows: RiskPositionRow[]): RiskPositionRow[] {
  const posMap = new Map<string, RiskPositionRow>();
  for (const pos of rows) {
    if (!pos.tokenId) continue;
    const tid = normalizeTokenId(pos.tokenId);
    const key = `${tid}_${pos.sideLabel || "default"}`;
    const existing = posMap.get(key);
    if (!existing || pos.id > existing.id) {
      posMap.set(key, { ...pos, tokenId: tid });
    }
  }
  return Array.from(posMap.values());
}

const avgEntryFallbackDeps = {
  getOpenPositions: () => cache.positions.filter((p) => p.status === "open"),
  onPositionUpdated: (row: RiskPositionRow) => {
    mergePatchedPosition(row);
    monitorCoordinator.refreshPositionsNow();
  },
};

function applyPositionSnapshot(rows: RiskPositionRow[], meta?: RiskPositionsMeta | null) {
  cache.positions = mergePositionRows(rows);
  if (meta !== undefined) {
    cache.meta = meta;
  }
  cache.lastRefresh = new Date();
  cache.loading = false;
  cache.error = null;
  updatePositionsFromBook();
  attachBookSubToPositions();
  reconcileAvgEntryFallback(avgEntryFallbackDeps, cache.positions);
  notifySubscribers();
}

function parsePositionRows(data: unknown): RiskPositionRow[] {
  if (!Array.isArray(data)) return [];
  return data.filter((row): row is RiskPositionRow => {
    return (
      row != null && typeof row === "object" && typeof (row as RiskPositionRow).id === "string"
    );
  });
}

let refreshPositionsTimer: ReturnType<typeof setTimeout> | null = null;
let refreshPositionsInflight: Promise<void> | null = null;
let fetchRiskDataInflight: Promise<void> | null = null;
let positionsPollInterval: ReturnType<typeof setInterval> | null = null;
let positionsPollRefCount = 0;
let positionsPollHidden = false;
let riskDataBootstrapped = false;

const POSITIONS_WS_DEBOUNCE_MS = 900;
const POSITIONS_RUNTIME_DEBOUNCE_MS = 600;

function scheduleRefreshPositions(delayMs = 0) {
  if (refreshPositionsTimer) clearTimeout(refreshPositionsTimer);
  refreshPositionsTimer = setTimeout(() => {
    refreshPositionsTimer = null;
    void refreshPositions();
  }, delayMs);
}

async function refreshPositions() {
  if (refreshPositionsInflight) return refreshPositionsInflight;
  if (fetchRiskDataInflight) {
    refreshPositionsInflight = fetchRiskDataInflight.then(() => undefined);
    return refreshPositionsInflight;
  }
  refreshPositionsInflight = (async () => {
    try {
      const p = await getMonitorPositions();
      const rows = p.positions ?? [];
      if (p.stale && rows.length === 0 && cache.positions.length > 0) {
        return;
      }
      if (p.stale && rows.length === 0) {
        cache.loading = true;
        cache.error = null;
        if (p.meta) cache.meta = p.meta;
        notifySubscribers();
        return;
      }
      applyPositionSnapshot(rows, p.meta ?? null);
      syncBookSubMeta();
    } catch (err) {
      console.error("Failed to refresh positions:", err);
    }
  })().finally(() => {
    refreshPositionsInflight = null;
  });
  return refreshPositionsInflight;
}

function handlePositionUpdateMessage(msg: PositionUpdateMessage) {
  const rows = parsePositionRows(msg.data);
  if (rows.length > 0 || (Array.isArray(msg.data) && msg.data.length === 0)) {
    applyPositionSnapshot(rows);
  }
  scheduleRefreshPositions(POSITIONS_WS_DEBOUNCE_MS);
}

function handleRiskRuntimePositionHint(msg: RiskRuntimeLogMessage) {
  if (msg.type === "risk_runtime_log_snapshot") return;
  const ty = msg.data.type ?? "";
  if (
    ty === "order.execution_summary" ||
    ty === "position.snapshot_changed" ||
    ty.includes("close_queued") ||
    ty.includes("closed")
  ) {
    scheduleRefreshPositions(POSITIONS_RUNTIME_DEBOUNCE_MS);
  }
}

function fetchRiskData(silent = false) {
  if (fetchRiskDataInflight) return fetchRiskDataInflight;
  if (!silent) {
    cache.loading = true;
    cache.error = null;
    notifySubscribers();
  }

  fetchRiskDataInflight = refreshPositions()
    .then(() => getMonitorTasks(50))
    .then((t) => {
      cache.tasks = Array.isArray(t.tasks) ? t.tasks : [];
    })
    .catch((err) => {
      cache.loading = false;
      cache.error = err instanceof Error ? err.message : "加载监控数据失败";
    })
    .finally(() => {
      fetchRiskDataInflight = null;
      notifySubscribers();
    });
  return fetchRiskDataInflight;
}

function mergePatchedPosition(row: RiskPositionRow) {
  const idx = cache.positions.findIndex((p) => p.id === row.id);
  if (idx < 0) return;
  cache.positions[idx] = { ...cache.positions[idx], ...row };
  updatePositionsFromBook();
  notifySubscribers();
}

// 移除模块级别的监听，全部交给 Hook 内部处理，防止多次触发 refresh
// riskWsBus.onPositionUpdate(() => {
//   void refreshPositions();
// });

function applyRestBookFrame(tokenId: string, frame: PolyBookFrame, _resp: RiskBookResponse) {
  const existing = tokenBookMap.get(tokenId);
  const bids = mergeBookSide(frame.bids, existing?.bids, true, frame.bestBid);
  const asks = mergeBookSide(frame.asks, existing?.asks, false, frame.bestAsk);
  tokenBookMap.set(tokenId, {
    ...existing,
    ...frame,
    tokenId,
    bids,
    asks,
  });
  updatePositionsFromBook();
}

function handlePolyBook(frame: PolyBookFrame) {
  const tid = normalizeTokenId(frame.tokenId);
  const merged = mergePolyBookFrame({ ...frame, tokenId: tid }, tokenBookMap.get(tid));
  tokenBookMap.set(tid, merged);
  onWsBookFrame(tid, merged);
  const now = Date.now();
  bookSubByToken.set(tid, bookSubRowForToken(tid, now));
  attachBookSubToPositions();
  cache.bookSubByToken = Object.fromEntries(bookSubByToken.entries());
  updatePositionsFromBook();
  notifySubscribers();
}

function ensureMonitorDataBootstrapped() {
  if (riskDataBootstrapped) return;
  riskDataBootstrapped = true;
  void fetchRiskData(true);
}

function startPositionsPoll() {
  positionsPollRefCount += 1;
  if (positionsPollInterval) return;

  const pollSec = Math.min(getWSConfig().wsRiskPollIntervalSec, 12);
  positionsPollInterval = setInterval(() => {
    if (positionsPollHidden) return;
    void refreshPositions();
  }, pollSec * 1000);
}

function stopPositionsPoll() {
  positionsPollRefCount = Math.max(0, positionsPollRefCount - 1);
  if (positionsPollRefCount > 0 || !positionsPollInterval) return;
  clearInterval(positionsPollInterval);
  positionsPollInterval = null;
}

/** Module-level refresh for external triggers (e.g. account switch). */
export function refreshMonitorData(silent = false) {
  void fetchRiskData(silent);
}

/** Top-bar refresh: full market cache rebuild, then reload monitor rows (no stale UI). */
export async function refreshMonitorWithUnifiedMarketsSync(): Promise<void> {
  cache.loading = true;
  cache.positions = [];
  cache.tasks = [];
  cache.error = null;
  notifySubscribers();
  try {
    await runUnifiedMarketsRefresh();
    await fetchRiskData(true);
  } catch (err) {
    cache.loading = false;
    cache.error = err instanceof Error ? err.message : "刷新失败";
    notifySubscribers();
    throw err;
  }
}

/** Apply PATCH response without full-page loading spinner. */
export function applyMonitorPositionPatch(row: RiskPositionRow) {
  mergePatchedPosition(row);
}

export function getOpenMonitorPositionCount(): number {
  return cache.positions.filter((p) => p.status === "open" && p.tokenId).length;
}

let monitorCacheBootstrapInstalled = false;

/** App-lifetime monitor cache + CLOB book wiring (not tied to /monitor route). */
export function installMonitorCacheBootstrap() {
  if (monitorCacheBootstrapInstalled || typeof window === "undefined") return;
  monitorCacheBootstrapInstalled = true;

  ensureMonitorDataBootstrapped();
  monitorCoordinator.subscribeBooks((_tid, frame) => handlePolyBook(frame));
  monitorCoordinator.subscribePositions((rows) => {
    cache.positions = mergePositionRows(rows);
    syncBookSubMeta();
    updatePositionsFromBook();
    reconcileAvgEntryFallback(avgEntryFallbackDeps, cache.positions);
    notifySubscribers();
  });
  riskWsBus.onPositionUpdate(handlePositionUpdateMessage);
  wsBus.onPositionUpdate(handlePositionUpdateMessage);
  riskWsBus.onRuntimeLog(handleRiskRuntimePositionHint);

  const syncConn = () => syncConnectionFromCoordinator();
  syncConn();
  monitorCoordinator.subscribeConnection(syncConn);
  setInterval(syncConn, 2000);

  const onVisibility = () => {
    positionsPollHidden = document.visibilityState === "hidden";
    setAvgEntryFallbackHidden(positionsPollHidden);
    if (!positionsPollHidden) void refreshPositions();
  };
  document.addEventListener("visibilitychange", onVisibility);
  positionsPollHidden = document.visibilityState === "hidden";

  startPositionsPoll();
  startBookFallbackPoller({
    getOpenTokenIds: () =>
      cache.positions
        .filter((p) => p.status === "open" && p.tokenId)
        .map((p) => normalizeTokenId(p.tokenId)),
    isObConnected: () => cache.polyOrderbookConnected,
    applyRestBook: applyRestBookFrame,
  });
  startAvgEntryFallbackPoller(avgEntryFallbackDeps);
}

export function useMonitorCache() {
  const [, setTick] = useState(0);

  useEffect(() => {
    installMonitorCacheBootstrap();
    const sub = subscribe(() => setTick((t) => t + 1));
    return () => sub();
  }, []);

  const refresh = useCallback((silent = false) => {
    void fetchRiskData(silent);
  }, []);

  return {
    positions: cache.positions,
    meta: cache.meta,
    tasks: cache.tasks,
    loading: cache.loading,
    error: cache.error,
    lastRefresh: cache.lastRefresh,
    polyOrderbookConnected: cache.polyOrderbookConnected,
    polyUserConnected: cache.polyUserConnected,
    bookSubByToken: cache.bookSubByToken,
    refresh,
  };
}
