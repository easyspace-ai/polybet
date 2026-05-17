import { useEffect, useState, useCallback } from 'react';
import {
  listPolymarketAccounts,
  createPolymarketAccount,
  activatePolymarketAccount,
  deletePolymarketAccount,
  type PolymarketAccountListItem,
  type PolymarketAccountCreateBody,
} from '@/lib/api';

interface AccountsState {
  accounts: PolymarketAccountListItem[];
  loading: boolean;
  error: string | null;
}

const cache: AccountsState = {
  accounts: [],
  loading: true,
  error: null,
};

const subscribers = new Set<(state: AccountsState) => void>();

function notifySubscribers() {
  subscribers.forEach((fn) => fn({ ...cache }));
}

function fetchAccounts(): Promise<void> {
  cache.loading = true;
  cache.error = null;
  notifySubscribers();

  return listPolymarketAccounts()
    .then((data) => {
      cache.accounts = Array.isArray(data) ? data : [];
      cache.loading = false;
      cache.error = null;
    })
    .catch((err) => {
      cache.loading = false;
      cache.error = err instanceof Error ? err.message : '加载账号失败';
    })
    .finally(() => {
      notifySubscribers();
    });
}

if (typeof window !== 'undefined') {
  fetchAccounts();
}

export function useAccounts() {
  const [state, setState] = useState<AccountsState>({ ...cache });

  useEffect(() => {
    subscribers.add(setState);
    setState({ ...cache });
    return () => {
      subscribers.delete(setState);
    };
  }, []);

  const refresh = useCallback(() => fetchAccounts(), []);

  const create = useCallback(async (body: PolymarketAccountCreateBody) => {
    await createPolymarketAccount(body);
  }, []);

  const activate = useCallback(async (id: string) => {
    await activatePolymarketAccount(id);
    await fetchAccounts();
  }, []);

  const remove = useCallback(async (id: string) => {
    await deletePolymarketAccount(id);
    await fetchAccounts();
  }, []);

  return {
    accounts: state.accounts ?? [],
    loading: state.loading,
    error: state.error,
    refresh,
    create,
    activate,
    remove,
  };
}
