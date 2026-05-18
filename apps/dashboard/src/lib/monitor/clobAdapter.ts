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
  return {
    tokenId: ev.asset_id,
    bids,
    asks,
    bestBid: bids[0]?.odds,
    bestAsk: asks[0]?.odds,
  };
}

export function clobPriceChangeToPolyFrames(ev: ClobPriceChangeEvent): PolyBookFrame[] {
  const out: PolyBookFrame[] = [];
  for (const ch of ev.price_changes ?? []) {
    const bestBid = parseOdds(ch.best_bid);
    const bestAsk = parseOdds(ch.best_ask);
    if (bestBid == null && bestAsk == null) continue;
    out.push({
      tokenId: ch.asset_id,
      bestBid: bestBid ?? undefined,
      bestAsk: bestAsk ?? undefined,
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
