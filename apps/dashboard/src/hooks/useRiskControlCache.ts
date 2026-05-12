import { useEffect, useState, useRef, useCallback } from 'react';
import {
  getRiskPositions,
  getRiskTasks,
  type RiskPositionRow,
  type RiskPositionsMeta,
  type RiskTaskRow,
} from '@/lib/api';
import { wsBus, type PositionUpdateMessage, type PolyBookFrame } from '@/lib/wsBus';

interface RiskState {
  positions: RiskPositionRow[];
  meta: RiskPositionsMeta | null;
  tasks: RiskTaskRow[];
  loading: boolean;
  error: string | null;
  lastRefresh: Date | null;
  wsConnected: boolean;
}

const cache: RiskState = {
  positions: [],
  meta: null,
  tasks: [],
  loading: true,
  error: null,
  lastRefresh: null,
  wsConnected: false,
};

const subscribers = new Set<(state: RiskState) => void>();
const tokenBookMap = new Map<string, PolyBookFrame>();

function notifySubscribers() {
  subscribers.forEach(fn => fn({ ...cache }));
}

function getBestBidCents(frame: PolyBookFrame | undefined): number | null {
  if (!frame || frame.levels.length === 0) return null;
  let bestBid = 0;
  for (const lvl of frame.levels) {
    if (lvl.size > 0 && lvl.odds > bestBid) {
      bestBid = lvl.odds;
    }
  }
  return bestBid > 0 ? bestBid * 100 : null;
}

function updatePositionsFromBook() {
  if (cache.positions.length === 0) return;
  let changed = false;
  for (const pos of cache.positions) {
    if (pos.status !== 'open' || !pos.tokenId) continue;
    const frame = tokenBookMap.get(pos.tokenId);
    const newBid = getBestBidCents(frame);
    if (newBid !== null && newBid !== pos.currentCents) {
      pos.currentCents = newBid;
      const hw = pos.highWaterCents;
      const trail = hw * (1 - pos.stopLossPct / 100);
      pos.trailingStopCents = trail;
      changed = true;
    }
  }
  if (changed) {
    cache.lastRefresh = new Date();
    notifySubscribers();
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
      cache.positions = p.positions;
      cache.meta = p.meta ?? null;
      cache.tasks = t.tasks;
      cache.lastRefresh = new Date();
      cache.loading = false;
      cache.error = null;

      const tokenIds = p.positions
        .filter(pos => pos.status === 'open' && pos.tokenId)
        .map(pos => pos.tokenId!);
      for (const tid of tokenIds) {
        wsBus.subscribePolyBook(tid);
      }
    })
    .catch((err) => {
      cache.loading = false;
      cache.error = err instanceof Error ? err.message : '加载风控数据失败';
    })
    .finally(() => {
      notifySubscribers();
    });
}

function handlePositionUpdate(msg: PositionUpdateMessage) {
  cache.positions = msg.data as RiskPositionRow[];
  cache.lastRefresh = new Date();

  const tokenIds = cache.positions
    .filter(pos => pos.status === 'open' && pos.tokenId)
    .map(pos => pos.tokenId!);
  for (const tid of tokenIds) {
    wsBus.subscribePolyBook(tid);
  }

  notifySubscribers();
}

function handlePolyBook(frame: PolyBookFrame) {
  tokenBookMap.set(frame.tokenId, frame);
  updatePositionsFromBook();
}

function handleWsStatus(connected: boolean) {
  cache.wsConnected = connected;
  notifySubscribers();
}

if (typeof window !== 'undefined') {
  fetchRiskData(true);
  wsBus.onPositionUpdate(handlePositionUpdate);
  wsBus.onPolyBook(handlePolyBook);
  wsBus.onStatus(handleWsStatus);
}

export function useRiskControlCache() {
  const [state, setState] = useState<RiskState>({ ...cache });

  useEffect(() => {
    subscribers.add(setState);
    setState({ ...cache });
    return () => {
      subscribers.delete(setState);
    };
  }, []);

  const refresh = useCallback(() => {
    fetchRiskData(false);
  }, []);

  return {
    positions: state.positions,
    meta: state.meta,
    tasks: state.tasks,
    loading: state.loading,
    error: state.error,
    lastRefresh: state.lastRefresh,
    wsConnected: state.wsConnected,
    refresh,
  };
}