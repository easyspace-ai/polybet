import type { BookLevel, PolyBookFrame } from "@/lib/wsBus";
import { centsFromPrice01, floorCents1 } from "@/lib/cents";

const polyPlatform = "polymarket" as const;

/**
 * Normalize a top-of-book scalar to cents. Server relay WS sends cents (e.g. 78);
 * browser CLOB WS sends 0–1 prices (e.g. 0.78). Values <= 1 are treated as 0–1.
 */
export function topOfBookScalarToCents(v: number | undefined): number | null {
  if (typeof v !== "number" || !Number.isFinite(v) || v <= 0) return null;
  if (v <= 1) return centsFromPrice01(v);
  return floorCents1(v);
}

function mergeBookSide(
  incoming: BookLevel[] | null | undefined,
  existing: BookLevel[] | undefined,
  sortBids: boolean,
  bestCents: number | null | undefined,
): BookLevel[] | undefined {
  if (incoming && incoming.length > 0) {
    const sorted = [...incoming].sort((a, b) => (sortBids ? b.odds - a.odds : a.odds - b.odds));
    return sorted.slice(0, 5);
  }
  if (incoming === undefined || incoming === null) {
    return existing;
  }
  // Empty ladder in payload (e.g. best_bid_ask-only WS tick) — do not wipe cached depth.
  if (incoming.length === 0) {
    const hasTop = typeof bestCents === "number" && bestCents > 0;
    if (hasTop) {
      if (existing && existing.length > 0) return existing;
      return [{ odds: bestCents / 100, size: 0, platform: polyPlatform }];
    }
    return existing ?? [];
  }
  return existing;
}

/** Merge incremental CLOB ticks into cached book state (preserves depth on partial updates). */
export function mergePolyBookFrame(
  incoming: PolyBookFrame,
  existing: PolyBookFrame | undefined,
): PolyBookFrame {
  const bestBidCents = topOfBookScalarToCents(incoming.bestBid);
  const bestAskCents = topOfBookScalarToCents(incoming.bestAsk);
  const bids = mergeBookSide(incoming.bids, existing?.bids, true, bestBidCents ?? undefined);
  const asks = mergeBookSide(incoming.asks, existing?.asks, false, bestAskCents ?? undefined);
  return {
    ...existing,
    ...incoming,
    tokenId: incoming.tokenId,
    bids,
    asks,
    bestBid: bestBidCents ?? existing?.bestBid,
    bestAsk: bestAskCents ?? existing?.bestAsk,
  };
}

/**
 * Best bid in cents for risk UI — must match the Bid column (买一), which reads
 * `bids[0]`. Prefer the sorted ladder over `bestBid` from top-of-book-only ticks,
 * which can lag a full depth update by one tick.
 */
export function bestBidCentsFromBookFrame(frame: PolyBookFrame | undefined): number | null {
  if (!frame) return null;
  if (frame.bids && frame.bids.length > 0) {
    const top = centsFromPrice01(frame.bids[0].odds);
    if (Number.isFinite(top) && top > 0) return top;
  }
  return topOfBookScalarToCents(frame.bestBid);
}

/** Best ask in cents (lowest offer). */
export function bestAskCentsFromBookFrame(frame: PolyBookFrame | undefined): number | null {
  if (!frame) return null;
  if (frame.asks && frame.asks.length > 0) {
    const top = centsFromPrice01(frame.asks[0].odds);
    if (Number.isFinite(top) && top > 0) return top;
  }
  return topOfBookScalarToCents(frame.bestAsk);
}

/** Top-of-book mark for trailing high-water ratchet — matches server max(bid, ask). */
export function topOfBookMarkCents(frame: PolyBookFrame | undefined): number | null {
  const b = bestBidCentsFromBookFrame(frame);
  const a = bestAskCentsFromBookFrame(frame);
  if (b == null && a == null) return null;
  return Math.max(b ?? 0, a ?? 0);
}
