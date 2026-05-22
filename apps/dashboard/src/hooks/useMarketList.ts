import { useEffect, useState, useRef, useCallback } from 'react';
import { getMarkets, postMarketsRefreshFull, type Market } from '@/lib/api';
import { wsBus, type MarketLifecycleMessage } from '@/lib/wsBus';

const WS_SNAPSHOT_FALLBACK_MS = 3_000;
const REFRESH_WAIT_MS = 120_000;

function waitForMarketsSnapshot(timeoutMs: number): Promise<Market[] | null> {
  return new Promise((resolve) => {
    let settled = false;
    const finish = (data: Market[] | null) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      off();
      resolve(data);
    };
    const timer = setTimeout(() => finish(null), timeoutMs);
    const off = wsBus.onMarketLifecycle((msg) => {
      if (msg.type === 'marketsSnapshot') {
        finish(msg.data);
      }
    });
  });
}

interface MarketListState {
  markets: Market[];
  loading: boolean;
  error: string | null;
  lastUpdate: Date | null;
  wsConnected: boolean;
}

export function useMarketList(): MarketListState & { refresh: () => Promise<void> } {
  const [state, setState] = useState<MarketListState>({
    markets: [],
    loading: true,
    error: null,
    lastUpdate: null,
    wsConnected: false,
  });

  const cacheRef = useRef(new Map<string, Market>());
  const snapshotReceived = useRef(false);
  const fallbackTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const applyFullSnapshot = useCallback((data: Market[]) => {
    cacheRef.current = new Map(data.map((m) => [m.id, m]));
    setState(prev => ({
      ...prev,
      markets: Array.from(cacheRef.current.values()),
      loading: false,
      lastUpdate: new Date(),
    }));
  }, []);

  const handleMarketMessage = useCallback((msg: MarketLifecycleMessage) => {
    if (msg.type === 'marketsSnapshot') {
      if (msg.data.length === 0) return;
      snapshotReceived.current = true;
      applyFullSnapshot(msg.data);
    } else if (msg.type === 'marketUpsert') {
      cacheRef.current.set(msg.data.id, msg.data);
      setState(prev => ({
        ...prev,
        markets: Array.from(cacheRef.current.values()),
        lastUpdate: new Date(),
      }));
    } else if (msg.type === 'marketRemoved') {
      cacheRef.current.delete(msg.id);
      setState(prev => ({
        ...prev,
        markets: Array.from(cacheRef.current.values()),
        lastUpdate: new Date(),
      }));
    }
  }, [applyFullSnapshot]);

  const handleWsStatus = useCallback((connected: boolean) => {
    setState(prev => ({ ...prev, wsConnected: connected }));
  }, []);

  const doFetchFromREST = useCallback((afterErrorStillStopLoading: boolean) => {
    getMarkets()
      .then((data) => {
        const markets = Array.isArray(data) ? data : [];
        if (markets.length > 0) {
          snapshotReceived.current = true;
          applyFullSnapshot(markets);
        }
        setState(prev => ({ ...prev, loading: false }));
      })
      .catch((err) => {
        if (afterErrorStillStopLoading) {
          setState(prev => ({
            ...prev,
            loading: false,
            error: err instanceof Error ? err.message : '加载市场失败',
          }));
        }
      });
  }, [applyFullSnapshot]);

  useEffect(() => {
    let cancelled = false;

    // 挂载时立即执行一次 REST 获取，确保报价最新
    doFetchFromREST(false);

    // 设置一个回退定时器，如果 3s 内 WS 没发 snapshot，强制再次拉取 REST (双保险)
    fallbackTimerRef.current = setTimeout(() => {
      if (!snapshotReceived.current && !cancelled) {
        doFetchFromREST(true);
      }
    }, WS_SNAPSHOT_FALLBACK_MS);

    const offMarket = wsBus.onMarketLifecycle(handleMarketMessage);
    const offStatus = wsBus.onStatusChange(handleWsStatus);

    return () => {
      cancelled = true;
      if (fallbackTimerRef.current) {
        clearTimeout(fallbackTimerRef.current);
      }
      offMarket();
      offStatus();
    };
  }, [handleMarketMessage, handleWsStatus, doFetchFromREST]);

  const reloadLocal = useCallback(async () => {
    setState(prev => ({ ...prev, loading: true, error: null, markets: [] }));
    const snapshot = await waitForMarketsSnapshot(5_000);
    if (snapshot) {
      snapshotReceived.current = true;
      applyFullSnapshot(snapshot);
      setState(prev => ({ ...prev, loading: false, error: null }));
      return;
    }
    doFetchFromREST(true);
  }, [applyFullSnapshot, doFetchFromREST]);

  const refresh = useCallback(async () => {
    setState(prev => ({ ...prev, loading: true, error: null, markets: [] }));
    cacheRef.current.clear();
    snapshotReceived.current = false;
    try {
      await postMarketsRefreshFull({ wait: true });
    } catch (err) {
      console.warn('[useMarketList] backend refresh failed, falling back to REST', err);
    }
    const snapshot = await waitForMarketsSnapshot(REFRESH_WAIT_MS);
    if (snapshot && snapshot.length >= 0) {
      snapshotReceived.current = true;
      applyFullSnapshot(snapshot);
      setState(prev => ({ ...prev, loading: false, error: null }));
      return;
    }
    doFetchFromREST(true);
  }, [applyFullSnapshot, doFetchFromREST]);

  return { ...state, refresh, reloadLocal };
}