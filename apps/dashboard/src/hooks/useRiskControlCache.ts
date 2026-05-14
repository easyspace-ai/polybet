import { useEffect, useState, useRef, useCallback } from 'react';
import {
  getRiskPositions,
  getRiskTasks,
  postRiskClosePosition,
  type RiskPositionRow,
  type RiskPositionsMeta,
  type RiskTaskRow,
} from '@/lib/api';
import { riskWsBus, polyDirectBus, type PositionUpdateMessage, type PolyBookFrame } from '@/lib/wsBus';

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
const tokenSubs = new Map<string, { unsubDash: () => void; unsubDirect: () => void }>();

function notifySubscribers() {
  subscribers.forEach(fn => fn({ ...cache }));
}

function subscribe(fn: (state: RiskState) => void) {
  subscribers.add(fn);
  return () => subscribers.delete(fn);
}

function getBestAskCents(frame: PolyBookFrame | undefined): number | null {
  if (!frame) return null;
  // 优先使用后端传来的 bestBid（持仓卖出价）
  if (typeof frame.bestBid === 'number' && frame.bestBid > 0) {
    return frame.bestBid;
  }
  // fallback：从 asks 中取最小 odds（卖一价）
  if (frame.asks && frame.asks.length > 0) {
    return frame.asks[0].odds * 100;
  }
  return null;
}

function normalizeTokenId(id: string | undefined | null): string {
  if (!id) return "";
  let s = id.toLowerCase().trim();
  if (!s.startsWith("0x")) s = "0x" + s;
  // Ensure it's 66 chars (0x + 64 hex)
  if (s.length < 66) {
    const hex = s.slice(2);
    s = "0x" + hex.padStart(64, "0");
  }
  return s;
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

    const newAsk = getBestAskCents(frame);
    if (newAsk !== null && newAsk !== pos.currentCents) {
      pos.currentCents = newAsk;
      const hw = pos.highWaterCents;
      const trail = hw * (1 - pos.stopLossPct / 100);
      pos.trailingStopCents = trail;
      changed = true;

      // 双保险机制：前端止损冗余触发
      // 如果当前价跌破止损价，且后端尚未执行平仓（status 仍为 open），前端发起平仓请求
      if (newAsk <= trail && pos.status === "open") {
        console.warn(`[Risk Insurance] Frontend detected stop-loss trigger for ${pos.title}: current ${newAsk} <= trail ${trail}. Triggering close...`);
        void postRiskClosePosition(pos.id).catch((err) => {
          console.error(`[Risk Insurance] Frontend stop-loss trigger failed for ${pos.id}:`, err);
        });
      }
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
    // 使用 tokenId + sideLabel 确保业务逻辑上的仓位唯一性，防止显示重复
    const posMap = new Map<string, RiskPositionRow>();
    for (const pos of p.positions) {
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
      for (const pos of p.positions) {
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
      cache.tasks = t.tasks;
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

  // 排序与深度控制：只保留离盘口最近的 5 档数据
  // Bids: 价格从高到低排序 (买一价最高)
  const bids = frame.bids 
    ? [...frame.bids].sort((a, b) => b.odds - a.odds).slice(0, 5) 
    : existing?.bids;
  // Asks: 价格从低到高排序 (卖一价最低)
  const asks = frame.asks 
    ? [...frame.asks].sort((a, b) => a.odds - b.odds).slice(0, 5) 
    : existing?.asks;

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

export function useRiskControlCache() {
  const [, setTick] = useState(0);

  useEffect(() => {
    // 当收到仓位更新通知时，必须重新拉取数据，而不仅仅是刷新 UI
    const unsubPos = riskWsBus.onPositionUpdate(() => {
      void refreshPositions();
    });
    const unsubStatus = riskWsBus.onPolyStatus((msg) => {
      cache.polyOrderbookConnected = msg.polyOrderbookConnected ?? cache.polyOrderbookConnected;
      cache.polyUserConnected = msg.polyUserConnected ?? cache.polyUserConnected;
      setTick((t) => t + 1);
    });

    const unsubDashStatus = riskWsBus.onStatusChange(() => {
      setTick((t) => t + 1);
    });

    const sub = subscribe(() => setTick((t) => t + 1));
    return () => {
      unsubPos();
      unsubStatus();
      unsubDashStatus();
      sub();
    };
  }, []);

  useEffect(() => {
    // 订阅逻辑应基于当前有效的仓位列表
    const openTokens = Array.from(new Set(
      cache.positions
        .filter((p) => p.status === 'open' && p.tokenId)
        .map((p) => normalizeTokenId(p.tokenId))
    ));

    // Subscribe to new tokens (Dual Insurance)
    for (const tid of openTokens) {
      if (!tokenSubs.has(tid)) {
        // 订阅 1: 经过后端中转的专属风控 WS
        const unsubDash = riskWsBus.subscribePolyBook(tid, handlePolyBook);
        // 订阅 2: 前端直接连接 Polymarket 官方 WS (双保险)
        const unsubDirect = polyDirectBus.subscribe(tid, handlePolyBook);
        
        tokenSubs.set(tid, { unsubDash, unsubDirect });
        console.log(`[Risk Guardian] Subscribed to ${tid} via Dual Channels`);
      }
    }

    // Unsubscribe from removed tokens
    for (const tid of tokenSubs.keys()) {
      if (!openTokens.includes(tid)) {
        const subs = tokenSubs.get(tid);
        subs?.unsubDash();
        subs?.unsubDirect();
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