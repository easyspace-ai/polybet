import { getRiskBook, getRiskBookSubscriptions, type RiskBookResponse } from "@/lib/api";
import type { PolyBookFrame } from "@/lib/wsBus";
import { appendWSLog } from "@/lib/wsConnectionLog";
import { bestBidCentsFromBookFrame } from "@/lib/riskBook";

const LOG_PREFIX = "[Risk Guardian]";

/** Match server polyBookSubStaleAfter (15s). */
export const BOOK_WS_STALE_MS = 15_000;
/** Per-token minimum interval between REST pulls. */
export const BOOK_REST_MIN_INTERVAL_MS = 3_000;
/** How often the fallback loop scans open positions. */
export const BOOK_FALLBACK_TICK_MS = 2_000;
/** Grace after subscribe before treating missing WS frames as stale. */
export const BOOK_SUBSCRIBE_GRACE_MS = 5_000;
const WS_LOG_THROTTLE_MS = 10_000;

type TokenBookMeta = {
  lastWsFrameMs: number;
  subscribedAtMs: number;
  lastRestMs: number;
  restBackoffMs: number;
  lastLoggedWsBid: number | null;
  lastWsLogMs: number;
};

const tokenMeta = new Map<string, TokenBookMeta>();

function metaFor(tokenId: string): TokenBookMeta {
  let m = tokenMeta.get(tokenId);
  if (!m) {
    m = {
      lastWsFrameMs: 0,
      subscribedAtMs: 0,
      lastRestMs: 0,
      restBackoffMs: BOOK_REST_MIN_INTERVAL_MS,
      lastLoggedWsBid: null,
      lastWsLogMs: 0,
    };
    tokenMeta.set(tokenId, m);
  }
  return m;
}

function logBookEvent(
  channel: "ws" | "rest",
  tokenId: string,
  bidCents: number | null,
  extra?: string,
) {
  const bidTxt = bidCents != null && bidCents > 0 ? `${bidCents.toFixed(1)}¢` : "—";
  const suffix = extra ? ` ${extra}` : "";
  const line = `${LOG_PREFIX} ${channel === "ws" ? "WS tick" : "REST refresh"} ${tokenId.slice(0, 10)}… bid=${bidTxt}${suffix}`;
  console.info(line);
  if (channel === "rest") {
    appendWSLog("ob", "warn", line.replace(LOG_PREFIX + " ", ""));
  }
}

export function markTokenSubscribed(tokenId: string): void {
  const m = metaFor(tokenId);
  m.subscribedAtMs = Date.now();
}

export function clearTokenBookMeta(tokenId: string): void {
  tokenMeta.delete(tokenId);
}

/** Record a live WS book frame; throttled console log when bid moves. */
export function onWsBookFrame(tokenId: string, frame: PolyBookFrame): void {
  const m = metaFor(tokenId);
  const now = Date.now();
  m.lastWsFrameMs = now;
  const bid = bestBidCentsFromBookFrame(frame);
  const bidChanged =
    bid != null && m.lastLoggedWsBid != null && Math.abs(bid - m.lastLoggedWsBid) >= 0.5;
  const throttleOk = now - m.lastWsLogMs >= WS_LOG_THROTTLE_MS;
  if (bid != null && (m.lastLoggedWsBid == null || bidChanged || throttleOk)) {
    m.lastLoggedWsBid = bid;
    m.lastWsLogMs = now;
    logBookEvent("ws", tokenId, bid);
  }
}

function restBookToFrame(resp: RiskBookResponse): PolyBookFrame {
  return {
    tokenId: resp.tokenId,
    bids: resp.bids,
    asks: resp.asks,
    bestBid: resp.bestBid,
    bestAsk: resp.bestAsk,
  };
}

function shouldRestFallback(
  tokenId: string,
  obConnected: boolean,
  serverSub?: { stale?: boolean; upstreamSubscribed?: boolean; lastFrameMs?: number },
): { needed: boolean; reason: string } {
  const m = metaFor(tokenId);
  const now = Date.now();
  if (serverSub?.stale && !serverSub.upstreamSubscribed) {
    return { needed: true, reason: "upstream_unsubscribed" };
  }
  if (serverSub?.stale && serverSub.lastFrameMs) {
    return { needed: true, reason: "server_stale" };
  }
  if (!obConnected) {
    return { needed: true, reason: "ob_disconnected" };
  }
  if (m.lastWsFrameMs === 0) {
    if (m.subscribedAtMs > 0 && now - m.subscribedAtMs < BOOK_SUBSCRIBE_GRACE_MS) {
      return { needed: false, reason: "" };
    }
    return { needed: true, reason: "no_ws_frame" };
  }
  if (now - m.lastWsFrameMs > BOOK_WS_STALE_MS) {
    return { needed: true, reason: "ws_stale" };
  }
  return { needed: false, reason: "" };
}

export type BookFallbackDeps = {
  getOpenTokenIds: () => string[];
  isObConnected: () => boolean;
  applyRestBook: (tokenId: string, frame: PolyBookFrame, resp: RiskBookResponse) => void;
};

async function refreshTokenBook(
  tokenId: string,
  reason: string,
  deps: BookFallbackDeps,
): Promise<void> {
  const m = metaFor(tokenId);
  const now = Date.now();
  const minGap = Math.max(BOOK_REST_MIN_INTERVAL_MS, m.restBackoffMs);
  if (now - m.lastRestMs < minGap) return;

  m.lastRestMs = now;
  try {
    const resp = await getRiskBook(tokenId, { refresh: true, reason });
    m.restBackoffMs = BOOK_REST_MIN_INTERVAL_MS;
    const frame = restBookToFrame(resp);
    const bid = bestBidCentsFromBookFrame(frame);
    logBookEvent("rest", tokenId, bid, `reason=${reason} source=${resp.source ?? "?"}`);
    deps.applyRestBook(tokenId, frame, resp);
  } catch (err) {
    m.restBackoffMs = Math.min(m.restBackoffMs * 2, 30_000);
    const msg = err instanceof Error ? err.message : String(err);
    console.warn(`${LOG_PREFIX} REST refresh failed ${tokenId.slice(0, 10)}…: ${msg}`);
    appendWSLog("ob", "warn", `REST 盘口兜底失败 ${tokenId.slice(0, 10)}…: ${msg}`);
  }
}

let fallbackTimer: ReturnType<typeof setInterval> | null = null;
let fallbackRefCount = 0;
const fallbackHidden = false;
let fallbackDeps: BookFallbackDeps | null = null;
let fallbackTickCount = 0;

async function runFallbackTick(): Promise<void> {
  if (fallbackHidden || !fallbackDeps) return;
  const obConnected = fallbackDeps.isObConnected();
  const tokens = fallbackDeps.getOpenTokenIds();
  if (tokens.length === 0) return;

  fallbackTickCount += 1;
  const serverSubs = new Map<
    string,
    { stale?: boolean; upstreamSubscribed?: boolean; lastFrameMs?: number }
  >();
  if (fallbackTickCount % 3 === 0) {
    try {
      const resp = await getRiskBookSubscriptions(tokens);
      for (const sub of resp.subscriptions ?? []) {
        if (sub.tokenId) serverSubs.set(sub.tokenId, sub);
      }
    } catch {
      // subscription health is best-effort
    }
  }

  for (const tid of tokens) {
    const { needed, reason } = shouldRestFallback(tid, obConnected, serverSubs.get(tid));
    if (!needed) continue;
    await refreshTokenBook(tid, reason, fallbackDeps);
  }
}

export function startBookFallbackPoller(deps: BookFallbackDeps): () => void {
  fallbackDeps = deps;
  fallbackRefCount += 1;
  if (fallbackTimer) {
    return () => stopBookFallbackPoller();
  }
  fallbackTimer = setInterval(() => {
    void runFallbackTick();
  }, BOOK_FALLBACK_TICK_MS);
  void runFallbackTick();
  return () => stopBookFallbackPoller();
}

function stopBookFallbackPoller(): void {
  fallbackRefCount = Math.max(0, fallbackRefCount - 1);
  if (fallbackRefCount > 0 || !fallbackTimer) return;
  clearInterval(fallbackTimer);
  fallbackTimer = null;
  fallbackDeps = null;
}
