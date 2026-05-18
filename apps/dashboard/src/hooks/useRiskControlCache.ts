import { useEffect, useState, useCallback } from "react";
import {
  getRiskBookSubscriptions,
  getRiskPositions,
  getRiskTasks,
  postRiskClosePosition,
  type RiskBookSubscriptionStatus,
  type RiskPositionRow,
  type RiskPositionsMeta,
  type RiskTaskRow,
} from "@/lib/api";
import { floorCents1, isTrailingStopActive, trailingStopCentsFromHW } from "@/lib/cents";
import { bestBidCentsFromBookFrame, topOfBookMarkCents } from "@/lib/riskBook";
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

const STOP_LOSS_CLOSE_COOLDOWN_MS = 30_000;

type StopLossCloseGuard = {
  inflight: Promise<unknown> | null;
  lastAttemptMs: number;
  lastTriggerKey: string;
};

const stopLossCloseGuard = new Map<string, StopLossCloseGuard>();

function stopLossTriggerKey(pos: RiskPositionRow, triggerPx: number, trail: number): string {
  return `${pos.status}|${triggerPx}|${trail}|${floorCents1(pos.highWaterCents)}`;
}

function hasPendingCloseTask(positionId: string): boolean {
  return cache.tasks.some(
    (t) =>
      t.positionId === positionId &&
      t.type === "close_position" &&
      (t.status === "pending" || t.status === "running"),
  );
}

function pruneStopLossCloseGuards(positions: RiskPositionRow[]) {
  const openIds = new Set(positions.filter((p) => p.status === "open").map((p) => p.id));
  for (const id of stopLossCloseGuard.keys()) {
    if (!openIds.has(id)) stopLossCloseGuard.delete(id);
  }
}

function maybeTriggerStopLossClose(pos: RiskPositionRow, triggerPx: number, trail: number): void {
  const triggerFloored = floorCents1(triggerPx);
  const trailFloored = floorCents1(trail);
  if (pos.status !== "open" || triggerFloored <= 0 || triggerFloored > trailFloored) return;
  if (hasPendingCloseTask(pos.id)) return;

  const key = stopLossTriggerKey(pos, triggerFloored, trailFloored);
  let guard = stopLossCloseGuard.get(pos.id);
  if (!guard) {
    guard = { inflight: null, lastAttemptMs: 0, lastTriggerKey: "" };
    stopLossCloseGuard.set(pos.id, guard);
  }
  if (guard.inflight) return;

  const now = Date.now();
  if (now - guard.lastAttemptMs < STOP_LOSS_CLOSE_COOLDOWN_MS && guard.lastTriggerKey === key) {
    return;
  }

  guard.lastAttemptMs = now;
  guard.lastTriggerKey = key;
  console.warn(
    `[Risk Insurance] Frontend detected stop-loss trigger for ${pos.title}: ref ${triggerFloored} <= trail ${trailFloored}. Triggering close...`,
  );
  guard.inflight = postRiskClosePosition(pos.id)
    .catch((err) => {
      console.error(`[Risk Insurance] Frontend stop-loss trigger failed for ${pos.id}:`, err);
    })
    .finally(() => {
      guard!.inflight = null;
    });
}

function resubscribeTokenBook(tid: string) {
  const subs = tokenSubs.get(tid);
  subs?.unsubDash();
  tokenSubs.delete(tid);
  const unsubDash = riskWsBus.subscribePolyBook(tid, handlePolyBook);
  tokenSubs.set(tid, { unsubDash });
  markTokenSubscribed(tid);
  console.warn(`[Risk Guardian] Re-subscribed book WS for ${tid}`);
}

function reconcileTokenBookSub(tid: string) {
  if (!cache.polyOrderbookConnected) return;
  const sub = bookSubByToken.get(tid);
  const local = riskWsBus.getPolyBookLocalState(tid);
  const lastMs = Math.max(sub?.lastFrameMs ?? 0, local.lastFrameMs ?? 0);
  const needsReconnect =
    !local.subscribed ||
    sub?.stale === true ||
    lastMs === 0 ||
    Date.now() - lastMs > BOOK_SUB_STALE_MS;
  if (!needsReconnect) return;

  const lastReconnect = bookReconnectLastAt.get(tid) ?? 0;
  if (Date.now() - lastReconnect < BOOK_SUB_RECONNECT_DEBOUNCE_MS) return;
  bookReconnectLastAt.set(tid, Date.now());

  if (local.subscribed) {
    riskWsBus.resendPolyBookSubscribe(tid);
    console.warn(`[Risk Guardian] Re-sent subscribePolyBook for stale ${tid}`);
    return;
  }
  resubscribeTokenBook(tid);
}

function reconcileOpenBookSubs() {
  if (!cache.polyOrderbookConnected) return;
  const openTokens = Array.from(
    new Set(
      cache.positions
        .filter((p) => p.status === "open" && p.tokenId)
        .map((p) => normalizeTokenId(p.tokenId)),
    ),
  );
  for (const tid of openTokens) {
    reconcileTokenBookSub(tid);
  }
}

let bookSubPollInterval: ReturnType<typeof setInterval> | null = null;
let bookSubPollRefCount = 0;
let refreshBookSubsInflight: Promise<void> | null = null;

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

/** Aligns with server `normalizeTokenID` (poly_ws / httpserver): decimal CLOB ids → 0x + 64 hex. */
function normalizeTokenId(id: string | undefined | null): string {
  if (!id) return "";
  const raw = id.trim();
  if (!raw) return "";
  const lower = raw.toLowerCase();
  if (lower.startsWith("0x")) {
    let hex = lower.slice(2);
    if (!/^[0-9a-f]+$/.test(hex)) {
      return lower.length >= 66 ? lower.slice(0, 66) : "0x" + hex.padStart(64, "0");
    }
    hex = hex.padStart(64, "0");
    if (hex.length > 64) hex = hex.slice(-64);
    return "0x" + hex;
  }
  try {
    const n = BigInt(raw);
    let hex = n.toString(16);
    hex = hex.padStart(64, "0");
    if (hex.length > 64) hex = hex.slice(-64);
    return "0x" + hex;
  } catch {
    const h = lower.replace(/^0x/, "");
    return "0x" + h.padStart(64, "0");
  }
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

      const triggerPx = floorCents1(bid != null && bid > 0 ? bid : (mark ?? 0));
      const trailFloored = floorCents1(trail);
      if (triggerPx > 0 && triggerPx <= trailFloored && pos.status === "open") {
        maybeTriggerStopLossClose(pos, triggerPx, trailFloored);
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

function applyPositionSnapshot(rows: RiskPositionRow[], meta?: RiskPositionsMeta | null) {
  cache.positions = mergePositionRows(rows);
  pruneStopLossCloseGuards(cache.positions);
  if (meta !== undefined) {
    cache.meta = meta;
  }
  cache.lastRefresh = new Date();
  cache.loading = false;
  cache.error = null;
  updatePositionsFromBook();
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
      const p = await getRiskPositions();
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
    .then(() => getRiskTasks(50))
    .then((t) => {
      cache.tasks = Array.isArray(t.tasks) ? t.tasks : [];
    })
    .catch((err) => {
      cache.loading = false;
      cache.error = err instanceof Error ? err.message : "加载风控数据失败";
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

async function refreshBookSubscriptions() {
  if (refreshBookSubsInflight) return refreshBookSubsInflight;
  refreshBookSubsInflight = (async () => {
    const openTokens = Array.from(
      new Set(
        cache.positions
          .filter((p) => p.status === "open" && p.tokenId)
          .map((p) => normalizeTokenId(p.tokenId)),
      ),
    );
    if (openTokens.length === 0) {
      if (bookSubByToken.size > 0) {
        bookSubByToken.clear();
        cache.bookSubByToken = {};
        notifySubscribers();
      }
      return;
    }
    try {
      const resp = await getRiskBookSubscriptions(openTokens);
      const seen = new Set<string>();
      for (const sub of resp.subscriptions ?? []) {
        const tid = sub.tokenId ? normalizeTokenId(sub.tokenId) : "";
        if (!tid) continue;
        seen.add(tid);
        const local = riskWsBus.getPolyBookLocalState(tid);
        const lastFrameMs = Math.max(sub.lastFrameMs ?? 0, local.lastFrameMs ?? 0) || undefined;
        const stale =
          !local.subscribed ||
          !sub.upstreamSubscribed ||
          sub.stale ||
          lastFrameMs == null ||
          Date.now() - lastFrameMs > BOOK_SUB_STALE_MS;
        bookSubByToken.set(tid, {
          ...sub,
          tokenId: tid,
          clientSubscribed: local.subscribed || sub.clientSubscribed,
          clientRefs: Math.max(sub.clientRefs, local.subscribed ? 1 : 0),
          lastFrameMs,
          stale,
        });
      }
      for (const tid of bookSubByToken.keys()) {
        if (!seen.has(tid)) bookSubByToken.delete(tid);
      }
      for (const pos of cache.positions) {
        if (!pos.tokenId) continue;
        const tid = normalizeTokenId(pos.tokenId);
        const row = bookSubByToken.get(tid);
        if (row) pos.bookSub = row;
      }
      cache.bookSubByToken = Object.fromEntries(bookSubByToken.entries());
      reconcileOpenBookSubs();
      notifySubscribers();
    } catch (err) {
      console.warn("[Risk Guardian] book-subscriptions poll failed:", err);
    }
  })().finally(() => {
    refreshBookSubsInflight = null;
  });
  return refreshBookSubsInflight;
}

function startBookSubPoll() {
  bookSubPollRefCount += 1;
  if (bookSubPollInterval) return;
  void refreshBookSubscriptions();
  bookSubPollInterval = setInterval(() => {
    if (positionsPollHidden) return;
    void refreshBookSubscriptions();
  }, BOOK_SUB_POLL_MS);
}

function stopBookSubPoll() {
  bookSubPollRefCount = Math.max(0, bookSubPollRefCount - 1);
  if (bookSubPollRefCount > 0 || !bookSubPollInterval) return;
  clearInterval(bookSubPollInterval);
  bookSubPollInterval = null;
}

function handlePolyBook(frame: PolyBookFrame) {
  const tid = normalizeTokenId(frame.tokenId);
  const existing = tokenBookMap.get(tid);

  const bids = mergeBookSide(frame.bids, existing?.bids, true, frame.bestBid);
  const asks = mergeBookSide(frame.asks, existing?.asks, false, frame.bestAsk);

  const merged: PolyBookFrame = {
    ...existing,
    ...frame,
    tokenId: tid,
    bids,
    asks,
  };
  tokenBookMap.set(tid, merged);
  onWsBookFrame(tid, merged);
  updatePositionsFromBook();
}

function ensureRiskDataBootstrapped() {
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
export function refreshRiskData(silent = false) {
  void fetchRiskData(silent);
}

/** Apply PATCH response without full-page loading spinner. */
export function applyRiskPositionPatch(row: RiskPositionRow) {
  mergePatchedPosition(row);
}

export function getOpenRiskPositionCount(): number {
  return cache.positions.filter((p) => p.status === "open" && p.tokenId).length;
}

/** @deprecated Use useMonitorCache — risk page redirects to /monitor */
export function useRiskControlCache() {
  const [, setTick] = useState(0);

  useEffect(() => {
    ensureRiskDataBootstrapped();
    // 当收到仓位更新通知时，必须重新拉取数据，而不仅仅是刷新 UI
    const unsubPosRisk = riskWsBus.onPositionUpdate(handlePositionUpdateMessage);
    const unsubPosDash = wsBus.onPositionUpdate(handlePositionUpdateMessage);
    const unsubRuntime = riskWsBus.onRuntimeLog(handleRiskRuntimePositionHint);
    const unsubStatus = riskWsBus.onPolyStatus((msg) => {
      if (msg.polyOrderbookConnected !== undefined) {
        cache.polyOrderbookConnected = msg.polyOrderbookConnected;
      }
      if (msg.polyUserConnected !== undefined) {
        cache.polyUserConnected = msg.polyUserConnected;
      }
      setTick((t) => t + 1);
    });

    const unsubDashStatus = riskWsBus.onStatusChange(() => {
      setTick((t) => t + 1);
    });

    const sub = subscribe(() => setTick((t) => t + 1));

    const onVisibility = () => {
      positionsPollHidden = document.visibilityState === "hidden";
      if (!positionsPollHidden) {
        void refreshPositions();
      }
    };
    document.addEventListener("visibilitychange", onVisibility);
    positionsPollHidden = document.visibilityState === "hidden";

    startPositionsPoll();
    startBookSubPoll();
    const stopBookFallback = startBookFallbackPoller({
      getOpenTokenIds: () =>
        cache.positions
          .filter((p) => p.status === "open" && p.tokenId)
          .map((p) => normalizeTokenId(p.tokenId)),
      isObConnected: () => cache.polyOrderbookConnected,
      applyRestBook: applyRestBookFrame,
    });

    return () => {
      unsubPosRisk();
      unsubPosDash();
      unsubRuntime();
      unsubStatus();
      unsubDashStatus();
      sub();
      document.removeEventListener("visibilitychange", onVisibility);
      stopPositionsPoll();
      stopBookSubPoll();
      stopBookFallback();
    };
  }, []);

  useEffect(() => {
    // 订阅逻辑应基于当前有效的仓位列表
    const openTokens = Array.from(
      new Set(
        cache.positions
          .filter((p) => p.status === "open" && p.tokenId)
          .map((p) => normalizeTokenId(p.tokenId)),
      ),
    );

    for (const tid of openTokens) {
      if (!tokenSubs.has(tid)) {
        const unsubDash = riskWsBus.subscribePolyBook(tid, handlePolyBook);
        tokenSubs.set(tid, { unsubDash });
        markTokenSubscribed(tid);
        console.log(`[Risk Guardian] Subscribed to ${tid} via server WS`);
      }
    }

    for (const tid of tokenSubs.keys()) {
      if (!openTokens.includes(tid)) {
        const subs = tokenSubs.get(tid);
        subs?.unsubDash();
        tokenSubs.delete(tid);
        tokenBookMap.delete(tid);
        clearTokenBookMeta(tid);
        bookSubByToken.delete(tid);
        notifySubscribers();
        console.log(`[Risk Guardian] Unsubscribed from ${tid}`);
      }
    }
    void refreshBookSubscriptions();
  }, [cache.positions]);

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
