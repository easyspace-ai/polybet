import { useEffect, useRef, useState } from 'react';
import { wsBus } from '../lib/wsBus';
import { getMarkets, type Market } from '../lib/api';

// If WS never delivers a non-empty `marketsSnapshot`, fall back to REST once after this delay.
const WS_SNAPSHOT_FALLBACK_MS = 3_000;

/**
 * Market list: hydrate from GET /api/markets (eager + timed fallback) and keep in sync
 * via WS `marketsSnapshot` / `marketUpsert` / `marketRemoved`.
 *
 * Empty WS snapshots are ignored — the server can send `data: []` on connect while sync
 * is still catching up; treating that as "done" used to skip REST entirely (no /api/markets
 * in DevTools and a blank page).
 */
export function useMarketList(): { markets: Market[]; loading: boolean } {
  const [markets, setMarkets] = useState<Market[]>([]);
  const [loading, setLoading] = useState(true);
  const cache = useRef(new Map<string, Market>());
  const snapshotReceived = useRef(false);

  useEffect(() => {
    let cancelled = false;

    const applyFullSnapshot = (data: Market[]) => {
      cache.current = new Map(data.map((m) => [m.id, m]));
      setMarkets(Array.from(cache.current.values()));
    };

    const tryHydrateFromREST = (afterErrorStillStopLoading: boolean) =>
      getMarkets()
        .then((data) => {
          if (cancelled || snapshotReceived.current) return;
          if (data.length > 0) {
            snapshotReceived.current = true;
            applyFullSnapshot(data);
          }
          setLoading(false);
        })
        .catch(() => {
          if (!cancelled && afterErrorStillStopLoading) setLoading(false);
        });

    // Eager REST so we match DB quickly even if the first WS frame is an empty snapshot.
    void tryHydrateFromREST(false);

    const off = wsBus.onMarketLifecycle((msg) => {
      if (msg.type === 'marketsSnapshot') {
        if (msg.data.length === 0) return;
        snapshotReceived.current = true;
        applyFullSnapshot(msg.data);
        setLoading(false);
      } else if (msg.type === 'marketUpsert') {
        cache.current.set(msg.data.id, msg.data);
        setMarkets(Array.from(cache.current.values()));
        setLoading(false);
      } else if (msg.type === 'marketRemoved') {
        cache.current.delete(msg.id);
        setMarkets(Array.from(cache.current.values()));
        setLoading(false);
      }
    });

    const fallback = setTimeout(() => {
      if (snapshotReceived.current) return;
      void tryHydrateFromREST(true);
    }, WS_SNAPSHOT_FALLBACK_MS);

    return () => {
      cancelled = true;
      clearTimeout(fallback);
      off();
    };
  }, []);

  return { markets, loading };
}
