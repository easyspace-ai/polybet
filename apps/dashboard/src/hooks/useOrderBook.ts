import { useEffect, useState } from 'react';
import { wsBus, type BookLevel } from '../lib/wsBus';

// Subscribes to the Polymarket book for a given tokenId. Returns null while waiting for first frame.
export function usePolyOrderBook(tokenId: string | null): BookLevel[] | null {
  const [levels, setLevels] = useState<BookLevel[] | null>(null);

  useEffect(() => {
    if (!tokenId) {
      setLevels(null);
      return;
    }
    setLevels(null);
    const off = wsBus.onPolyBook((frame) => {
      if (frame.tokenId !== tokenId) return;
      setLevels(frame.levels);
    });
    wsBus.subscribePolyBook(tokenId);
    return () => {
      off();
      wsBus.unsubscribePolyBook(tokenId);
    };
  }, [tokenId]);

  return levels;
}
