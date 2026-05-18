import { useEffect, useState } from "react";
import { normalizeTokenId } from "@/lib/clobTokenId";
import { monitorCoordinator } from "@/lib/monitor/coordinator";
import { type BookLevel } from "@/lib/wsBus";

export function usePolyOrderBook(tokenId: string | null): { bids: BookLevel[]; asks: BookLevel[] } | null {
  const [data, setData] = useState<{ bids: BookLevel[]; asks: BookLevel[] } | null>(null);

  useEffect(() => {
    if (!tokenId) {
      setData(null);
      return;
    }

    const tid = normalizeTokenId(tokenId);
    setData(null);

    const applyFrame = (frameTid: string, frame: { bids?: BookLevel[]; asks?: BookLevel[] }) => {
      if (frameTid !== tid) return;
      setData({ bids: frame.bids ?? [], asks: frame.asks ?? [] });
    };

    const cached = monitorCoordinator.getBook(tid);
    if (cached) applyFrame(tid, cached);

    const unsubBook = monitorCoordinator.subscribeBooks(applyFrame);
    const unsubToken = monitorCoordinator.subscribeBookToken(tokenId);

    return () => {
      unsubBook();
      unsubToken();
    };
  }, [tokenId]);

  return data;
}
