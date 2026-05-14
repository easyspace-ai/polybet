import { useEffect, useState, useCallback } from 'react';
import { getTrades, type Trade } from '@/lib/api';
import { wsBus } from '@/lib/wsBus';

interface TradesState {
  trades: Trade[];
  total: number;
  loading: boolean;
  error: string | null;
  lastRefresh: Date | null;
  wsConnected: boolean;
}

export function useTrades(page: number = 1, limit: number = 20) {
  const [state, setState] = useState<TradesState>({
    trades: [],
    total: 0,
    loading: true,
    error: null,
    lastRefresh: null,
    wsConnected: false,
  });

  const fetchTrades = useCallback(() => {
    setState(prev => ({ ...prev, loading: true, error: null }));
    getTrades(page, limit)
      .then((res) => {
        setState(prev => ({
          ...prev,
          trades: res.trades,
          total: res.total,
          loading: false,
          lastRefresh: new Date(),
        }));
      })
      .catch((err) => {
        setState(prev => ({
          ...prev,
          loading: false,
          error: err instanceof Error ? err.message : '加载交易记录失败',
        }));
      });
  }, [page, limit]);

  useEffect(() => {
    fetchTrades();
    
    // Listen for balance updates which may indicate new trades
    const offBalance = wsBus.onBalanceUpdate(() => {
      // Refresh trades when balance changes (new trade may have happened)
      fetchTrades();
    });

    const offStatus = wsBus.onStatusChange((connected) => {
      setState(prev => ({ ...prev, wsConnected: connected }));
    });

    return () => {
      offBalance();
      offStatus();
    };
  }, [fetchTrades]);

  return { 
    trades: state.trades, 
    total: state.total, 
    loading: state.loading, 
    error: state.error,
    lastRefresh: state.lastRefresh,
    wsConnected: state.wsConnected,
    refresh: fetchTrades,
  };
}