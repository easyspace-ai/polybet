import type { PolyBookFrame } from "@/lib/wsBus";
import { centsFromPrice01, floorCents1 } from "@/lib/cents";

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
  if (typeof frame.bestBid === "number" && frame.bestBid > 0) {
    return floorCents1(frame.bestBid);
  }
  return null;
}

/** Best ask in cents (lowest offer). */
export function bestAskCentsFromBookFrame(frame: PolyBookFrame | undefined): number | null {
  if (!frame) return null;
  if (frame.asks && frame.asks.length > 0) {
    const top = centsFromPrice01(frame.asks[0].odds);
    if (Number.isFinite(top) && top > 0) return top;
  }
  if (typeof frame.bestAsk === "number" && frame.bestAsk > 0) {
    return floorCents1(frame.bestAsk);
  }
  return null;
}

/** Top-of-book mark for trailing high-water ratchet — matches server max(bid, ask). */
export function topOfBookMarkCents(frame: PolyBookFrame | undefined): number | null {
  const b = bestBidCentsFromBookFrame(frame);
  const a = bestAskCentsFromBookFrame(frame);
  if (b == null && a == null) return null;
  return Math.max(b ?? 0, a ?? 0);
}
