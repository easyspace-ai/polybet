import { useEffect, useState, useRef, useCallback } from 'react';
import {
  getRiskPositions,
  getRiskTasks,
  postRiskClosePosition,
  type RiskPositionRow,
  type RiskPositionsMeta,
  type RiskTaskRow,
} from '@/lib/api';
import { riskWsBus, type BookLevel, type PositionUpdateMessage, type PolyBookFrame } from '@/lib/wsBus';
import { getWSConfig } from '@/hooks/useWSConfig';
import { getWSStatus } from '@/lib/api';

interface RiskState {
  positions: RiskPositionRow[];
  meta: RiskPositionsMeta | null;
  tasks: RiskTaskRow[];
  loading: boolean;
  error: string | null;
  lastRefresh: Date | null;
  polyOrderbookConnected: boolean;
  polyUserConnected: boolean;
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
};

const subscribers = new Set<(state: RiskState) => void>();
const tokenBookMap = new Map<string, PolyBookFrame>();
const tokenSubs = new Map<string, { unsubDash: () => void }>();

function notifySubscribers() {
  subscribers.forEach(fn => fn({ ...cache }));
}

function subscribe(fn: (state: RiskState) => void) {
  subscribers.add(fn);
  return () => subscribers.delete(fn);
}

/** Best bid in cents (what you can sell at now). */
function getBestBidCentsFromFrame(frame: PolyBookFrame | undefined): number | null {
  if (!frame) return null;
  if (typeof frame.bestBid === "number" && frame.bestBid > 0) {
    return frame.bestBid;
  }
  if (frame.bids && frame.bids.length > 0) {
    return frame.bids[0].odds * 100;
  }
  return null;
}

/** Best ask in cents (lowest offer). */
function getBestAskCentsFromFrame(frame: PolyBookFrame | undefined): number | null {
  if (!frame) return null;
  if (typeof frame.bestAsk === "number" && frame.bestAsk > 0) {
    return frame.bestAsk;
  }
  if (frame.asks && frame.asks.length > 0) {
    return frame.asks[0].odds * 100;
  }
  return null;
}

/** Top of book “high” for trailing watermark — matches server max(bid, ask). */
function getTopOfBookMarkCents(frame: PolyBookFrame | undefined): number | null {
  const b = getBestBidCentsFromFrame(frame);
  const a = getBestAskCentsFromFrame(frame);
  if (b == null && a == null) return null;
  return Math.max(b ?? 0, a ?? 0);
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
    const nextCurrent = bid ?? mark;
    if (nextCurrent != null && nextCurrent !== pos.currentCents) {
      pos.currentCents = nextCurrent;
      changed = true;
    }
    const effHw = Math.max(pos.highWaterCents, mark ?? 0);
    const trail = effHw * (1 - pos.stopLossPct / 100);
    if (pos.trailingStopCents !== trail) {
      pos.trailingStopCents = trail;
      changed = true;
    }

    const triggerPx = bid != null && bid > 0 ? bid : (mark ?? 0);
    if (triggerPx > 0 && triggerPx <= trail && pos.status === "open") {
      console.warn(`[Risk Insurance] Frontend detected stop-loss trigger for ${pos.title}: ref ${triggerPx} <= trail ${trail}. Triggering close...`);
      void postRiskClosePosition(pos.id).catch((err) => {
        console.error(`[Risk Insurance] Frontend stop-loss trigger failed for ${pos.id}:`, err);
      });
    }
  }
  if (changed) {
    cache.lastRefresh = new Date();
    notifySubscribers();
  }
}

async function refreshPositions() {
  try {
    const p = await getRiskPositions();
    const rows = p.positions ?? [];
    // 使用 tokenId + sideLabel 确保业务逻辑上的仓位唯一性，防止显示重复
    const posMap = new Map<string, RiskPositionRow>();
    for (const pos of rows) {
      if (!pos.tokenId) continue;
      // 必须标准化 Token ID，防止大小写或前缀不一致导致的重复
      const tid = normalizeTokenId(pos.tokenId);
      const key = `${tid}_${pos.sideLabel || 'default'}`;
      const existing = posMap.get(key);
      // 如果同一个 Token 有多个仓位记录，只保留最新的 (UUID 比较不可靠，但业务上不应有重复)
      if (!existing || pos.id > existing.id) {
        posMap.set(key, { ...pos, tokenId: tid });
      }
    }
    cache.positions = Array.from(posMap.values());
    cache.meta = p.meta ?? null;
    cache.lastRefresh = new Date();
    updatePositionsFromBook();
    notifySubscribers();
  } catch (err) {
    console.error("Failed to refresh positions:", err);
  }
}

function fetchRiskData(silent = false) {
  if (!silent) {
    cache.loading = true;
    cache.error = null;
    notifySubscribers();
  }

  Promise.all([getRiskPositions(), getRiskTasks(50)])
    .then(([p, t]) => {
      const posMap = new Map<string, RiskPositionRow>();
      const positionRows = p.positions ?? [];
      for (const pos of positionRows) {
        if (!pos.tokenId) continue;
        const tid = normalizeTokenId(pos.tokenId);
        const key = `${tid}_${pos.sideLabel || 'default'}`;
        const existing = posMap.get(key);
        if (!existing || pos.id > existing.id) {
          posMap.set(key, { ...pos, tokenId: tid });
        }
      }
      cache.positions = Array.from(posMap.values());
      cache.meta = p.meta ?? null;
      cache.tasks = Array.isArray(t.tasks) ? t.tasks : [];
      cache.lastRefresh = new Date();
      cache.loading = false;
      cache.error = null;

      // 用本地已缓存 of orderbook 填充 currentCents
      updatePositionsFromBook();
    })
    .catch((err) => {
      cache.loading = false;
      cache.error = err instanceof Error ? err.message : "加载风控数据失败";
    })
    .finally(() => {
      notifySubscribers();
    });
}

// 移除模块级别的监听，全部交给 Hook 内部处理，防止多次触发 refresh
// riskWsBus.onPositionUpdate(() => {
//   void refreshPositions();
// });

function handlePolyBook(frame: PolyBookFrame) {
  const tid = normalizeTokenId(frame.tokenId);
  // 合并逻辑：如果新 frame 有数据，更新本地 Map
  const existing = tokenBookMap.get(tid);

  const bids = mergeBookSide(frame.bids, existing?.bids, true, frame.bestBid);
  const asks = mergeBookSide(frame.asks, existing?.asks, false, frame.bestAsk);

  tokenBookMap.set(tid, { 
    ...existing, 
    ...frame,
    tokenId: tid,
    bids,
    asks
  });
  updatePositionsFromBook();
}

if (typeof window !== 'undefined') {
  fetchRiskData(true);
}

/** Module-level refresh for external triggers (e.g. account switch). */
export function refreshRiskData() {
  fetchRiskData(false);
}

export function getOpenRiskPositionCount(): number {
  return cache.positions.filter((p) => p.status === "open" && p.tokenId).length;
}

export function useRiskControlCache() {
  const [, setTick] = useState(0);

  useEffect(() => {
    // 当收到仓位更新通知时，必须重新拉取数据，而不仅仅是刷新 UI
    const unsubPos = riskWsBus.onPositionUpdate(() => {
      void refreshPositions();
    });
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

    const poll = setInterval(async () => {
      try {
        const st = await getWSStatus();
        if (st.polyOrderbookConnected !== undefined) cache.polyOrderbookConnected = st.polyOrderbookConnected;
        if (st.polyUserConnected !== undefined) cache.polyUserConnected = st.polyUserConnected;
        const relayBad = riskWsBus.getConnectionState() !== "CONNECTED";
        if (relayBad || !cache.polyOrderbookConnected || !cache.polyUserConnected) {
          await refreshPositions();
        }
        setTick((t) => t + 1);
      } catch {
        /* ignore */
      }
    }, getWSConfig().wsRiskPollIntervalSec * 1000);

    return () => {
      unsubPos();
      unsubStatus();
      unsubDashStatus();
      sub();
      clearInterval(poll);
    };
  }, []);

  useEffect(() => {
    // 订阅逻辑应基于当前有效的仓位列表
    const openTokens = Array.from(new Set(
      cache.positions
        .filter((p) => p.status === 'open' && p.tokenId)
        .map((p) => normalizeTokenId(p.tokenId))
    ));

    for (const tid of openTokens) {
      if (!tokenSubs.has(tid)) {
        const unsubDash = riskWsBus.subscribePolyBook(tid, handlePolyBook);
        tokenSubs.set(tid, { unsubDash });
        console.log(`[Risk Guardian] Subscribed to ${tid} via server WS`);
      }
    }

    for (const tid of tokenSubs.keys()) {
      if (!openTokens.includes(tid)) {
        const subs = tokenSubs.get(tid);
        subs?.unsubDash();
        tokenSubs.delete(tid);
        tokenBookMap.delete(tid);
        console.log(`[Risk Guardian] Unsubscribed from ${tid}`);
      }
    }
  }, [cache.positions]);

  const refresh = useCallback(() => {
    fetchRiskData(false);
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
    refresh,
  };
}