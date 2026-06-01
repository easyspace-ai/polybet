import {
  getMonitorClobSession,
  getMonitorPositions,
  postMonitorHeartbeat,
  postMonitorPositionsSync,
  postMonitorProfitProtectEvaluate,
  postMonitorStopLossTrigger,
  type MonitorClobSession,
  type RiskPositionRow,
} from "@/lib/api";
import { floorCents1, isTrailingStopActive, trailingStopCentsFromHW } from "@/lib/cents";
import {
  effectiveProfitProtectEnabled,
  profitProtectShouldTrigger,
  resolveProfitProtectCents,
} from "@/lib/profitProtect";
import { clobAssetIdForAPI, normalizeTokenId } from "@/lib/clobTokenId";
import {
  clobBookToPolyFrame,
  clobPriceChangeToPolyFrames,
  shouldSyncPositionsOnTrade,
} from "@/lib/monitor/clobAdapter";
import {
  bestBidCentsFromBookFrame,
  mergePolyBookFrame,
  topOfBookMarkCents,
} from "@/lib/riskBook";
import { markUpstreamReconnecting, setUpstreamFromPolyStatus } from "@/lib/wsConnectionLog";
import { riskWsBus, wsBus, type PolyBookFrame } from "@/lib/wsBus";
import {
  ClobMarketClient,
  ClobUserClient,
  type ConnectionState,
} from "polymarket-websocket-client";

const HEARTBEAT_MS = 20_000;
const STOP_LOSS_CLOSE_COOLDOWN_MS = 30_000;
const PROFIT_PROTECT_EVAL_COOLDOWN_MS = 8_000;
const RECONNECT_DEBOUNCE_MS = 5_000;

type BookListener = (tokenId: string, frame: PolyBookFrame) => void;
type PositionsListener = (positions: RiskPositionRow[]) => void;

class MonitorCoordinatorImpl {
  private started = false;
  private creds: MonitorClobSession | null = null;
  private userClient: ClobUserClient | null = null;
  private marketClient: ClobMarketClient | null = null;
  private userUnsubTrade: (() => void) | null = null;
  private userUnsubState: (() => void) | null = null;
  private userUnsubDisconnected: (() => void) | null = null;
  private marketUnsubBook: (() => void) | null = null;
  private marketUnsubPrice: (() => void) | null = null;
  private marketUnsubState: (() => void) | null = null;
  private userConnected = false;
  private obConnected = false;
  private heartbeatTimer: ReturnType<typeof setInterval> | null = null;
  private bookListeners = new Set<BookListener>();
  private positionsListeners = new Set<PositionsListener>();
  /** Ref-counted book subs outside open positions (e.g. /market trade panel). */
  private extraBookRefs = new Map<string, number>();
  private tokenBookMap = new Map<string, PolyBookFrame>();
  private positions: RiskPositionRow[] = [];
  private stopLossGuard = new Map<
    string,
    { inflight: Promise<unknown> | null; lastAttemptMs: number; lastKey: string }
  >();
  private profitProtectGuard = new Map<
    string,
    { inflight: Promise<unknown> | null; lastAttemptMs: number; lastKey: string }
  >();
  private lastReconnectAt = 0;
  private connectionListeners = new Set<() => void>();

  start() {
    if (this.started) return;
    this.started = true;
    void this.bootstrap();
    wsBus.onPositionUpdate(() => void this.refreshPositions());
    riskWsBus.onPositionUpdate(() => void this.refreshPositions());
    wsBus.onMonitorAccountChanged(() => this.reconnect());
    riskWsBus.onMonitorAccountChanged(() => this.reconnect());
    document.addEventListener("visibilitychange", () => {
      if (document.visibilityState === "visible") void this.refreshPositions();
    });
  }

  subscribeBooks(fn: BookListener): () => void {
    this.bookListeners.add(fn);
    return () => this.bookListeners.delete(fn);
  }

  subscribePositions(fn: PositionsListener): () => void {
    this.positionsListeners.add(fn);
    fn(this.positions);
    return () => this.positionsListeners.delete(fn);
  }

  /** Fires when browser CLOB user/market connection state changes. */
  subscribeConnection(fn: () => void): () => void {
    this.connectionListeners.add(fn);
    fn();
    return () => this.connectionListeners.delete(fn);
  }

  getBook(tokenId: string): PolyBookFrame | undefined {
    return this.tokenBookMap.get(normalizeTokenId(tokenId));
  }

  /** Subscribe CLOB market WS for a token (ref-counted; hex or decimal id). */
  subscribeBookToken(tokenId: string): () => void {
    const tid = normalizeTokenId(tokenId);
    if (!tid) return () => {};
    this.extraBookRefs.set(tid, (this.extraBookRefs.get(tid) ?? 0) + 1);
    this.reconcileMarketSubs();
    return () => {
      const next = (this.extraBookRefs.get(tid) ?? 1) - 1;
      if (next <= 0) this.extraBookRefs.delete(tid);
      else this.extraBookRefs.set(tid, next);
      this.reconcileMarketSubs();
    };
  }

  getSubscribedTokens(): string[] {
    return this.marketClient?.getSubscribedAssets() ?? [];
  }

  isUserConnected(): boolean {
    return this.userConnected;
  }

  isOrderbookConnected(): boolean {
    return this.obConnected;
  }

  reconnect() {
    const now = Date.now();
    if (now - this.lastReconnectAt < RECONNECT_DEBOUNCE_MS) return;
    this.lastReconnectAt = now;
    this.teardownStreams();
    void this.bootstrap();
  }

  /** Pull latest positions from REST (e.g. after avg-entry backfill). */
  refreshPositionsNow() {
    void this.refreshPositions();
  }

  private async bootstrap() {
    try {
      this.creds = await getMonitorClobSession();
    } catch (err) {
      console.warn("[Monitor] clob-session failed", err);
      setUpstreamFromPolyStatus({
        polyUserConnected: false,
        polyOrderbookConnected: false,
      });
      return;
    }
    await this.refreshPositions();
    this.startUser();
    this.reconcileMarketSubs();
    this.startHeartbeat();
  }

  private startUser() {
    if (!this.creds) return;
    this.teardownUser();

    const auth = {
      apiKey: this.creds.apiKey,
      secret: this.creds.apiSecret,
      passphrase: this.creds.apiPassphrase,
    };
    this.userClient = new ClobUserClient(auth, {
      url: this.creds.userWsUrl,
      userSubscribeMode: "legacy",
    });

    this.userUnsubTrade = this.userClient.onTrade((ev) => {
      if (!shouldSyncPositionsOnTrade(ev)) return;
      void postMonitorPositionsSync().then(() => this.refreshPositions());
    });
    this.userUnsubState = this.userClient.on("stateChange", ({ state }) => {
      this.onUserState(state);
    });
    this.userUnsubDisconnected = this.userClient.on("disconnected", ({ code, reason }) => {
      if (code !== 1000) {
        console.warn("[Monitor] user ws closed", { code, reason });
      }
    });

    void this.userClient.connect().catch((err) => {
      console.warn("[Monitor] user ws connect failed", err);
    });
  }

  private ensureMarket() {
    if (!this.creds) return;
    if (this.marketClient) return;

    this.marketClient = new ClobMarketClient({
      url: this.creds.marketWsUrl,
    });

    this.marketUnsubBook = this.marketClient.onBook((ev) => {
      this.applyBookFrame(clobBookToPolyFrame(ev));
    });
    this.marketUnsubPrice = this.marketClient.onPriceChange((ev) => {
      for (const frame of clobPriceChangeToPolyFrames(ev)) {
        this.applyBookFrame(frame);
      }
    });
    this.marketUnsubState = this.marketClient.on("stateChange", ({ state }) => {
      this.onMarketState(state);
    });

    void this.marketClient.connect().catch((err) => {
      console.warn("[Monitor] market ws connect failed", err);
    });
  }

  private applyBookFrame(frame: PolyBookFrame) {
    const tid = normalizeTokenId(frame.tokenId);
    if (!tid) return;
    const merged = mergePolyBookFrame({ ...frame, tokenId: tid }, this.tokenBookMap.get(tid));
    this.tokenBookMap.set(tid, merged);
    for (const fn of this.bookListeners) fn(tid, merged);
    this.evaluateStopLossFromBook(tid, merged);
    this.evaluateProfitProtectFromBook(tid, merged);
  }

  private reconcileMarketSubs() {
    const positionTokens = this.positions
      .filter((p) => p.status === "open" && p.tokenId)
      .map((p) => clobAssetIdForAPI(normalizeTokenId(p.tokenId)))
      .filter((id) => id !== "");
    const extraTokens = Array.from(this.extraBookRefs.keys())
      .map((tid) => clobAssetIdForAPI(tid))
      .filter((id) => id !== "");
    const want = Array.from(new Set([...positionTokens, ...extraTokens]));
    if (want.length === 0) {
      const current = this.marketClient?.getSubscribedAssets() ?? [];
      if (current.length > 0) this.marketClient?.unsubscribe(current);
      return;
    }
    this.ensureMarket();
    const current = new Set(this.marketClient?.getSubscribedAssets() ?? []);
    const wantSet = new Set(want);
    const toAdd = want.filter((t) => !current.has(t));
    const toRemove = [...current].filter((t) => !wantSet.has(t));
    if (toAdd.length) this.marketClient?.subscribe(toAdd);
    if (toRemove.length) this.marketClient?.unsubscribe(toRemove);
  }

  private async refreshPositions() {
    try {
      const res = await getMonitorPositions();
      this.positions = res.positions ?? [];
      for (const fn of this.positionsListeners) fn(this.positions);
      this.reconcileMarketSubs();
    } catch (err) {
      console.warn("[Monitor] positions refresh failed", err);
    }
  }

  private startHeartbeat() {
    if (this.heartbeatTimer) clearInterval(this.heartbeatTimer);
    const tick = () => {
      void postMonitorHeartbeat({
        userConnected: this.userConnected,
        orderbookConnected: this.obConnected,
        subscribedTokens: this.marketClient?.getSubscribedAssets() ?? [],
      }).catch(() => {});
    };
    tick();
    this.heartbeatTimer = setInterval(tick, HEARTBEAT_MS);
  }

  private onUserState(state: ConnectionState) {
    this.userConnected = state === "connected";
    this.pushUpstreamState();
    this.sendHeartbeatNow();
  }

  private onMarketState(state: ConnectionState) {
    this.obConnected = state === "connected";
    this.pushUpstreamState();
    this.sendHeartbeatNow();
  }

  private sendHeartbeatNow() {
    void postMonitorHeartbeat({
      userConnected: this.userConnected,
      orderbookConnected: this.obConnected,
      subscribedTokens: this.marketClient?.getSubscribedAssets() ?? [],
    }).catch(() => {});
  }

  private pushUpstreamState() {
    setUpstreamFromPolyStatus({
      polyUserConnected: this.userConnected,
      polyOrderbookConnected: this.obConnected,
      polyUserConnecting:
        this.userClient != null &&
        this.userClient.connectionState !== "connected" &&
        this.creds != null,
      polyOrderbookConnecting:
        this.marketClient != null &&
        this.marketClient.connectionState !== "connected" &&
        this.creds != null,
    });
    for (const fn of this.connectionListeners) fn();
  }

  private evaluateStopLossFromBook(tid: string, frame: PolyBookFrame) {
    for (const pos of this.positions) {
      if (pos.status !== "open" || normalizeTokenId(pos.tokenId) !== tid) continue;
      if (!isTrailingStopActive(pos)) continue;
      const bid = bestBidCentsFromBookFrame(frame);
      const mark = topOfBookMarkCents(frame);
      const effHw = floorCents1(Math.max(floorCents1(pos.highWaterCents), mark ?? 0));
      const trail = trailingStopCentsFromHW(effHw, pos.stopLossPct);
      const triggerPx = floorCents1(bid != null && bid > 0 ? bid : (mark ?? 0));
      if (triggerPx > 0 && triggerPx <= floorCents1(trail)) {
        void this.maybeTriggerStopLoss(pos, triggerPx, trail);
      }
    }
  }

  private async maybeTriggerStopLoss(
    pos: RiskPositionRow,
    triggerPx: number,
    trail: number,
  ) {
    const key = `${pos.status}|${triggerPx}|${trail}|${floorCents1(pos.highWaterCents)}`;
    let guard = this.stopLossGuard.get(pos.id);
    if (!guard) {
      guard = { inflight: null, lastAttemptMs: 0, lastKey: "" };
      this.stopLossGuard.set(pos.id, guard);
    }
    if (guard.inflight) return;
    const now = Date.now();
    if (now - guard.lastAttemptMs < STOP_LOSS_CLOSE_COOLDOWN_MS && guard.lastKey === key) return;
    guard.lastAttemptMs = now;
    guard.lastKey = key;
    guard.inflight = postMonitorStopLossTrigger({
      positionId: pos.id,
      tokenId: pos.tokenId,
      triggerCents: triggerPx,
      trailCents: trail,
    })
      .catch((err) => console.error("[Monitor] stop-loss trigger failed", err))
      .finally(() => {
        guard!.inflight = null;
      });
  }

  private evaluateProfitProtectFromBook(tid: string, frame: PolyBookFrame) {
    for (const pos of this.positions) {
      if (pos.status !== "open" || normalizeTokenId(pos.tokenId) !== tid) continue;
      if (!effectiveProfitProtectEnabled(pos, pos.profitProtectEnabled !== false)) continue;
      if (pos.profitProtectMode !== "cents") continue;
      const bid = bestBidCentsFromBookFrame(frame);
      const mark = floorCents1(bid != null && bid > 0 ? bid : (topOfBookMarkCents(frame) ?? 0));
      if (mark <= 0) continue;

      const cfg = resolveProfitProtectCents(pos);
      if (!cfg) continue;

      if (profitProtectShouldTrigger(pos, mark)) {
        void this.maybeEvaluateProfitProtect(pos, mark, cfg.stop, "trigger");
        continue;
      }
      if (!pos.profitProtectArmed && mark >= cfg.arm) {
        void this.maybeEvaluateProfitProtect(pos, mark, cfg.arm, "arm");
      }
    }
  }

  private async maybeEvaluateProfitProtect(
    pos: RiskPositionRow,
    mark: number,
    refCents: number,
    phase: "arm" | "trigger",
  ) {
    const key = `${phase}|${mark}|${refCents}|${pos.profitProtectCustom ? "c" : "g"}`;
    let guard = this.profitProtectGuard.get(pos.id);
    if (!guard) {
      guard = { inflight: null, lastAttemptMs: 0, lastKey: "" };
      this.profitProtectGuard.set(pos.id, guard);
    }
    if (guard.inflight) return;
    const now = Date.now();
    const cooldown =
      phase === "trigger" ? STOP_LOSS_CLOSE_COOLDOWN_MS : PROFIT_PROTECT_EVAL_COOLDOWN_MS;
    if (now - guard.lastAttemptMs < cooldown && guard.lastKey === key) return;
    guard.lastAttemptMs = now;
    guard.lastKey = key;
    guard.inflight = postMonitorProfitProtectEvaluate({
      positionId: pos.id,
      bidCents: mark,
      askCents: mark,
    })
      .then(() => this.refreshPositions())
      .catch((err) => console.error("[Monitor] profit-protect evaluate failed", err))
      .finally(() => {
        guard!.inflight = null;
      });
  }

  private teardownUser() {
    this.userUnsubTrade?.();
    this.userUnsubState?.();
    this.userUnsubDisconnected?.();
    this.userUnsubTrade = null;
    this.userUnsubState = null;
    this.userUnsubDisconnected = null;
    this.userClient?.disconnect();
    this.userClient = null;
    this.userConnected = false;
  }

  private teardownMarket() {
    this.marketUnsubBook?.();
    this.marketUnsubPrice?.();
    this.marketUnsubState?.();
    this.marketUnsubBook = null;
    this.marketUnsubPrice = null;
    this.marketUnsubState = null;
    this.marketClient?.disconnect();
    this.marketClient = null;
    this.obConnected = false;
  }

  private teardownStreams() {
    this.teardownUser();
    this.teardownMarket();
  }
}

export const monitorCoordinator = new MonitorCoordinatorImpl();

export function installMonitorCoordinator() {
  monitorCoordinator.start();
}

export function requestMonitorReconnect(channel: "orderbook" | "user") {
  markUpstreamReconnecting(channel === "orderbook" ? "ob" : "user");
  monitorCoordinator.reconnect();
}
