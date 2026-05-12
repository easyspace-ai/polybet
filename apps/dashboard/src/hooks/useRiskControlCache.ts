import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
  getRiskPositions,
  getRiskTasks,
  type RiskPositionRow,
  type RiskPositionsMeta,
  type RiskTaskRow,
} from '@/lib/api';
import { useEffect, useRef, useState } from 'react';
import { wsBus, type PositionUpdateMessage, type PolyBookFrame } from '@/lib/wsBus';

interface RiskData {
  positions: RiskPositionRow[];
  meta: RiskPositionsMeta | null;
  tasks: RiskTaskRow[];
}

const TOKEN_UPDATE_THROTTLE_MS = 1000;

function useRiskPositions() {
  return useQuery({
    queryKey: ['risk', 'positions'],
    queryFn: () => getRiskPositions(),
    staleTime: 10_000,
    refetchInterval: 30_000,
  });
}

function useRiskTasks() {
  return useQuery({
    queryKey: ['risk', 'tasks'],
    queryFn: () => getRiskTasks(50),
    staleTime: 10_000,
    refetchInterval: 30_000,
  });
}

function usePolyBookUpdates() {
  const queryClient = useQueryClient();
  const lastUpdateRef = useRef(0);
  const positionsRef = useRef<RiskPositionRow[]>([]);

  useEffect(() => {
    const handlePositionUpdate = (msg: PositionUpdateMessage) => {
      positionsRef.current = msg.data as RiskPositionRow[];
      queryClient.setQueryData(['risk', 'positions'], (old: { positions: RiskPositionRow[] } | undefined) => {
        if (!old) return { positions: positionsRef.current };
        return { ...old, positions: positionsRef.current };
      });
    };

    const handlePolyBook = (frame: PolyBookFrame) => {
      const now = Date.now();
      if (now - lastUpdateRef.current < TOKEN_UPDATE_THROTTLE_MS) return;
      lastUpdateRef.current = now;

      queryClient.setQueryData(['risk', 'positions'], (old: { positions: RiskPositionRow[] } | undefined) => {
        if (!old) return old;
        
        const updatedPositions = old.positions.map(pos => {
          if (pos.status !== 'open' || !pos.tokenId || pos.tokenId !== frame.tokenId) {
            return pos;
          }
          
          const bestBid = frame.levels.find(l => l.size > 0)?.odds;
          if (!bestBid) return pos;
          
          const newBid = Math.round(bestBid * 100);
          if (newBid === pos.currentCents) return pos;
          
          const hw = pos.highWaterCents;
          const trail = hw * (1 - pos.stopLossPct / 100);
          
          return {
            ...pos,
            currentCents: newBid,
            trailingStopCents: trail,
          };
        });
        
        return { ...old, positions: updatedPositions };
      });
    };

    wsBus.onPositionUpdate(handlePositionUpdate);
    wsBus.onPolyBook(handlePolyBook);

    return () => {
      wsBus.offPositionUpdate(handlePositionUpdate);
      wsBus.offPolyBook(handlePolyBook);
    };
  }, [queryClient]);
}

export function useRiskControlCache() {
  const positionsQuery = useRiskPositions();
  const tasksQuery = useRiskTasks();
  
  usePolyBookUpdates();

  const [forceRefresh, setForceRefresh] = useState(0);

  const isLoading = positionsQuery.isLoading || tasksQuery.isLoading;
  const isError = positionsQuery.isError || tasksQuery.isError;

  const data: RiskData = {
    positions: positionsQuery.data?.positions ?? [],
    meta: positionsQuery.data?.meta ?? null,
    tasks: tasksQuery.data?.tasks ?? [],
  };

  const refresh = async () => {
    setForceRefresh(prev => prev + 1);
    await Promise.all([
      positionsQuery.refetch(),
      tasksQuery.refetch(),
    ]);
  };

  return {
    positions: data.positions,
    meta: data.meta,
    tasks: data.tasks,
    loading: isLoading,
    error: isError,
    refresh,
  };
}