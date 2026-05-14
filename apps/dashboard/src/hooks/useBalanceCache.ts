import { useEffect, useState, useRef, useCallback } from 'react';
import { getBalances, type BalanceSummary } from '@/lib/api';
import { wsBus, type BalanceUpdateMessage } from '@/lib/wsBus';

interface BalanceState {
  data: BalanceSummary | null;
  loading: boolean;
  error: string | null;
  lastRefresh: Date | null;
  wsConnected: boolean;
}

const cache: BalanceState = {
  data: null,
  loading: true,
  error: null,
  lastRefresh: null,
  wsConnected: false,
};

const subscribers = new Set<(state: BalanceState) => void>();

function notifySubscribers() {
  subscribers.forEach(fn => fn({ ...cache }));
}

function fetchBalance(silent = false) {
  if (!silent) {
    cache.loading = true;
    cache.error = null;
    notifySubscribers();
  }

  getBalances()
    .then((data) => {
      cache.data = data;
      cache.lastRefresh = new Date();
      cache.loading = false;
      cache.error = null;
    })
    .catch((err) => {
      cache.loading = false;
      cache.error = err instanceof Error ? err.message : '加载余额失败';
      if (cache.data === null) {
        cache.data = { polymarket: null, polymarketAccounts: [] };
      }
    })
    .finally(() => {
      notifySubscribers();
    });
}

function handleBalanceUpdate(msg: BalanceUpdateMessage) {
   cache.data = msg.data;
   cache.loading = false;
   cache.error = null;
   cache.lastRefresh = new Date();
   notifySubscribers();
}

function handleWsStatus(connected: boolean) {
  cache.wsConnected = connected;
  notifySubscribers();
}

// Initialize on client side
if (typeof window !== 'undefined') {
  fetchBalance(true); // Initial silent fetch
  wsBus.onBalanceUpdate(handleBalanceUpdate);
  wsBus.onStatusChange(handleWsStatus);
}

export function useBalanceCache() {
  const [state, setState] = useState<BalanceState>({ ...cache });

  useEffect(() => {
    subscribers.add(setState);
    // Sync current state
    setState({ ...cache });
    return () => {
      subscribers.delete(setState);
    };
  }, []);

  const refresh = useCallback(() => {
    fetchBalance(false);
  }, []);

  return {
    balance: state.data,
    loading: state.loading,
    error: state.error,
    lastRefresh: state.lastRefresh,
    wsConnected: state.wsConnected,
    refresh,
  };
}