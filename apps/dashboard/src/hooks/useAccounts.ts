import { useEffect, useState } from 'react';
import {
  listPolymarketAccounts,
  createPolymarketAccount,
  activatePolymarketAccount,
  deletePolymarketAccount,
  type PolymarketAccountListItem,
  type PolymarketAccountCreateBody,
} from '@/lib/api';

export function useAccounts() {
  const [accounts, setAccounts] = useState<PolymarketAccountListItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const refresh = () => {
    setLoading(true);
    setError(null);
    listPolymarketAccounts()
      .then(setAccounts)
      .catch((err) => setError(err instanceof Error ? err.message : 'Failed to load accounts'))
      .finally(() => setLoading(false));
  };

  const create = async (body: PolymarketAccountCreateBody) => {
    await createPolymarketAccount(body);
    refresh();
  };

  const activate = async (id: string) => {
    await activatePolymarketAccount(id);
    refresh();
  };

  const remove = async (id: string) => {
    await deletePolymarketAccount(id);
    refresh();
  };

  useEffect(() => {
    refresh();
  }, []);

  return { accounts, loading, error, refresh, create, activate, remove };
}