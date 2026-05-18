import { centsFromPrice01 } from "@/lib/cents";
import { normalizeTokenId } from "@/lib/clobTokenId";
import type {
  ClobBookEvent,
  ClobPriceChangeEvent,
  ClobTradeEvent,
} from "polymarket-websocket-client";
import type { PolyBookFrame } from "@/lib/wsBus";

const TRADE_SYNC_STATUSES = new Set(["MATCHED", "MINED", "CONFIRMED"]);

export function shouldSyncPositionsOnTrade(ev: ClobTradeEvent): boolean {
  return TRADE_SYNC_STATUSES.has(String(ev.status ?? "").toUpperCase());
}

export function clobBookToPolyFrame(ev: ClobBookEvent): PolyBookFrame {
  const bids = parseLevels(ev.bids);
  const asks = parseLevels(ev.asks);
  const bestBid01 = bids[0]?.odds;
  const bestAsk01 = asks[0]?.odds;
  return {
    tokenId: normalizeTokenId(ev.asset_id),
    bids,
    asks,
    // Match server polyBookUpdate: bestBid/bestAsk are cents, not 0–1.
    bestBid: bestBid01 != null ? centsFromPrice01(bestBid01) : undefined,
    bestAsk: bestAsk01 != null ? centsFromPrice01(bestAsk01) : undefined,
  };
}

export function clobPriceChangeToPolyFrames(ev: ClobPriceChangeEvent): PolyBookFrame[] {
  const out: PolyBookFrame[] = [];
  for (const ch of ev.price_changes ?? []) {
    const bestBid01 = parseOdds(ch.best_bid);
    const bestAsk01 = parseOdds(ch.best_ask);
    if (bestBid01 == null && bestAsk01 == null) continue;
    out.push({
      tokenId: normalizeTokenId(ch.asset_id),
      bestBid: bestBid01 != null ? centsFromPrice01(bestBid01) : undefined,
      bestAsk: bestAsk01 != null ? centsFromPrice01(bestAsk01) : undefined,
    });
  }
  return out;
}

function parseLevels(
  raw: { price: string; size: string }[] | undefined,
): { odds: number; size: number; platform: "polymarket" }[] {
  if (!raw?.length) return [];
  return raw
    .map((row) => {
      const odds = parseOdds(row.price);
      const size = Number(row.size);
      if (odds == null) return null;
      return {
        odds,
        size: Number.isFinite(size) ? size : 0,
        platform: "polymarket" as const,
      };
    })
    .filter((x): x is { odds: number; size: number; platform: "polymarket" } => x != null);
}

function parseOdds(v: string | number | undefined): number | null {
  const n = Number(v);
  return Number.isFinite(n) ? n : null;
}
