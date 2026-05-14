import { useEffect, useState } from "react";
import { wsBus, type BookLevel } from "@/lib/wsBus";

export function usePolyOrderBook(tokenId: string | null): { bids: BookLevel[]; asks: BookLevel[] } | null {
  const [data, setData] = useState<{ bids: BookLevel[]; asks: BookLevel[] } | null>(null);

  useEffect(() => {
    if (!tokenId) {
      setData(null);
      return;
    }

    setData(null);
    const unsub = wsBus.subscribePolyBook(tokenId, (frame) => {
      if (frame.tokenId !== tokenId) return;
      setData({ bids: frame.bids || [], asks: frame.asks || [] });
    });

    return unsub;
  }, [tokenId]);

  return data;
}
