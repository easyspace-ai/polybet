import { useEffect, useRef, useState } from 'react';
import { getBalances, type BalanceSummary } from '../lib/api';
import { wsBus, type BalanceUpdateMessage } from '../lib/wsBus';

interface CacheState {
  data: BalanceSummary | null;
  loading: boolean;
  lastRefresh: number;
}

const cache: CacheState = {
  data: null,
  loading: true,
  lastRefresh: 0,
};

const subscribers = new Set<(s: CacheState) => void>();

function notifySubscribers() {
  subscribers.forEach((fn) => fn({ ...cache }));
}

function fetchBalance(silent = false) {
  if (!silent) cache.loading = true;
  notifySubscribers();

  getBalances()
    .then((data) => {
      cache.data = data;
      cache.lastRefresh = Date.now();
    })
    .catch(() => {
      if (cache.data === null) {
        cache.data = { polymarket: null, polymarketAccounts: [] };
      }
    })
    .finally(() => {
      cache.loading = false;
      notifySubscribers();
    });
}

function handleBalanceUpdate(msg: BalanceUpdateMessage) {
  cache.data = msg.data;
  cache.lastRefresh = Date.now();
  notifySubscribers();
}

if (typeof window !== 'undefined') {
  fetchBalance(true);
  const wsOff = wsBus.onBalanceUpdate(handleBalanceUpdate);
}

export function useBalanceCache() {
  const [state, setState] = useState<CacheState>({ ...cache });

  useEffect(() => {
    subscribers.add(setState);
    setState({ ...cache });
    return () => {
      subscribers.delete(setState);
    };
  }, []);

  return {
    balance: state.data,
    loading: state.loading,
    refresh: () => fetchBalance(false),
  };
}