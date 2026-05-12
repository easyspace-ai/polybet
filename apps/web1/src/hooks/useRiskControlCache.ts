import { useEffect, useRef, useState } from 'react';
import {
  getRiskPositions,
  getRiskTasks,
  type RiskPositionRow,
  type RiskPositionsMeta,
  type RiskTaskRow,
} from '../lib/api';
import { wsBus, type PositionUpdateMessage, type PolyBookFrame } from '../lib/wsBus';

interface CacheState {
  positions: RiskPositionRow[];
  meta: RiskPositionsMeta | null;
  tasks: RiskTaskRow[];
  loading: boolean;
  lastRefresh: number;
}

const cache: CacheState = {
  positions: [],
  meta: null,
  tasks: [],
  loading: true,
  lastRefresh: 0,
};

const subscribers = new Set<(s: CacheState) => void>();

const tokenBookMap = new Map<string, PolyBookFrame>();

function notifySubscribers() {
  subscribers.forEach((fn) => fn({ ...cache }));
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
    cache.lastRefresh = Date.now();
    notifySubscribers();
  }
}

function fetchRiskData(silent = false) {
  if (!silent) cache.loading = true;
  notifySubscribers();

  Promise.all([getRiskPositions(), getRiskTasks(50)])
    .then(([p, t]) => {
      cache.positions = p.positions;
      cache.meta = p.meta ?? null;
      cache.tasks = t.tasks;
      cache.lastRefresh = Date.now();

      const tokenIds = p.positions
        .filter(pos => pos.status === 'open' && pos.tokenId)
        .map(pos => pos.tokenId!);
      for (const tid of tokenIds) {
        wsBus.subscribePolyBook(tid);
      }
    })
    .catch(() => {})
    .finally(() => {
      cache.loading = false;
      notifySubscribers();
    });
}

function handlePositionUpdate(msg: PositionUpdateMessage) {
  cache.positions = msg.data as RiskPositionRow[];
  cache.lastRefresh = Date.now();

  const tokenIds = cache.positions
    .filter(pos => pos.status === 'open' && pos.tokenId)
    .map(pos => pos.tokenId!);
  for (const tid of tokenIds) {
    wsBus.subscribePolyBook(tid);
  }

  notifySubscribers();
}

if (typeof window !== 'undefined') {
  fetchRiskData(true);
  wsBus.onPositionUpdate(handlePositionUpdate);

  const handlePolyBook = (frame: PolyBookFrame) => {
    tokenBookMap.set(frame.tokenId, frame);
    updatePositionsFromBook();
  };
  wsBus.onPolyBook(handlePolyBook);

  const pollInterval = setInterval(() => fetchRiskData(true), 5000);
}

export function useRiskControlCache() {
  const [state, setState] = useState<CacheState>({ ...cache });

  useEffect(() => {
    subscribers.add(setState);
    setState({ ...cache });
    return () => {
      subscribers.delete(setState);
    };
  }, []);

  return {
    positions: state.positions,
    meta: state.meta,
    tasks: state.tasks,
    loading: state.loading,
    refresh: () => fetchRiskData(false),
  };
}