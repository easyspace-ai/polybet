import { useEffect, useState, useCallback } from 'react';
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

const BALANCE_BY_ACCOUNT_KEY = 'polybet:balanceByAccount';

function loadStoredBalanceByAccount(): Record<string, number> {
  if (typeof sessionStorage === 'undefined') return {};
  try {
    const raw = sessionStorage.getItem(BALANCE_BY_ACCOUNT_KEY);
    if (!raw) return {};
    const o = JSON.parse(raw) as unknown;
    if (!o || typeof o !== 'object') return {};
    const out: Record<string, number> = {};
    for (const [k, v] of Object.entries(o)) {
      if (typeof v === 'number' && !Number.isNaN(v)) out[k] = v;
    }
    return out;
  } catch {
    return {};
  }
}

function persistStoredBalanceByAccount(m: Record<string, number>) {
  if (typeof sessionStorage === 'undefined') return;
  try {
    sessionStorage.setItem(BALANCE_BY_ACCOUNT_KEY, JSON.stringify(m));
  } catch {
    /* quota / private mode */
  }
}

/** Accepts API/WS payloads; tolerates legacy PascalCase from older servers. */
function normalizeBalancePayload(raw: unknown): BalanceSummary {
  if (!raw || typeof raw !== 'object') {
    return { polymarket: null, polymarketAccounts: [] };
  }
  const o = raw as Record<string, unknown>;
  const pm = o.polymarket ?? o.Polymarket;
  const acctsRaw = o.polymarketAccounts ?? o.PolymarketAccounts;
  const polymarket = typeof pm === 'number' && !Number.isNaN(pm) ? pm : null;
  const polymarketAccounts: BalanceSummary['polymarketAccounts'] = [];
  if (Array.isArray(acctsRaw)) {
    for (const row of acctsRaw) {
      if (!row || typeof row !== 'object') continue;
      const r = row as Record<string, unknown>;
      const id = typeof r.id === 'string' ? r.id : '';
      if (!id) continue;
      const name = typeof r.name === 'string' ? r.name : '';
      const isActive = Boolean(r.isActive);
      const p = r.polymarket ?? r.Polymarket;
      const polymarketVal =
        typeof p === 'number' && !Number.isNaN(p) ? p : null;
      polymarketAccounts.push({ id, name, isActive, polymarket: polymarketVal });
    }
  }
  return { polymarket, polymarketAccounts };
}

/** Hydrate null per-account balances from sessionStorage; refresh stored map from non-null API values. */
function mergeWithStoredBalances(data: BalanceSummary): BalanceSummary {
  const stored = loadStoredBalanceByAccount();
  const next: Record<string, number> = { ...stored };
  const polymarketAccounts = data.polymarketAccounts.map((row) => {
    let poly = row.polymarket;
    if (poly != null && !Number.isNaN(poly)) {
      next[row.id] = poly;
    } else if (next[row.id] != null) {
      poly = next[row.id];
    }
    return { ...row, polymarket: poly };
  });
  persistStoredBalanceByAccount(next);
  const active = polymarketAccounts.find((a) => a.isActive);
  const polymarket = active?.polymarket ?? data.polymarket ?? null;
  return { polymarket, polymarketAccounts };
}

function applyBalancePayload(raw: unknown): BalanceSummary {
  return mergeWithStoredBalances(normalizeBalancePayload(raw));
}

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
      cache.data = applyBalancePayload(data);
      cache.lastRefresh = new Date();
      cache.loading = false;
      cache.error = null;
    })
    .catch((err) => {
      cache.loading = false;
      cache.error = err instanceof Error ? err.message : '加载余额失败';
      if (cache.data === null) {
        cache.data = applyBalancePayload({ polymarket: null, polymarketAccounts: [] });
      }
    })
    .finally(() => {
      notifySubscribers();
    });
}

function handleBalanceUpdate(msg: BalanceUpdateMessage) {
   cache.data = applyBalancePayload(msg.data);
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