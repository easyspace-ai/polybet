// Shared WebSocket bus — resilient connection per dashboard tab.

import type { Market, RiskRuntimeLogEnvelope } from "./api";
import { getWSConfig, subscribeWSConfig, type WSClientConfig } from "@/hooks/useWSConfig";
import { appendWSLog, applyUpstreamPolyStatus, setRelayState } from "@/lib/wsConnectionLog";

export type WSConnectionState = "IDLE" | "CONNECTING" | "CONNECTED" | "RECONNECTING" | "STALE";

export type MarketLifecycleMessage =
  | { type: "marketsSnapshot"; data: Market[] }
  | { type: "marketUpsert"; data: Market }
  | { type: "marketRemoved"; id: string };

export interface BalanceUpdateData {
  polymarket: number | null;
  polymarketAccounts: { id: string; name: string; isActive: boolean; polymarket: number | null }[];
}

export type BalanceUpdateMessage = { type: "balance_update"; data: BalanceUpdateData };

export type PositionUpdateMessage = { type: "position_update"; data: unknown[] };

export interface BestOddsEntry {
  marketHash: string;
  isMakerBettingOutcomeOne: boolean;
  takerOdds: number;
  updatedAt: number;
}

export interface BookLevel {
  odds: number;
  size: number;
  platform: "polymarket";
}

export interface BookFrame {
  marketHash: string;
  outcomeOne: BookLevel[];
  outcomeTwo: BookLevel[];
}

export interface PolyBookFrame {
  tokenId: string;
  bids?: BookLevel[];
  asks?: BookLevel[];
  bestBid?: number;
  bestAsk?: number;
}

export interface PolyStatusMessage {
  type: "poly_status";
  polyOrderbookConnected?: boolean;
  polyUserConnected?: boolean;
  orderbookNextRetryAt?: number;
  orderbookReconnectAttempt?: number;
  userNextRetryAt?: number;
  userReconnectAttempt?: number;
  userWsLastIssue?: string;
  wsEvents?: { channel?: string; at?: string; level?: string; message?: string }[];
}

export interface PolyOddsEntry {
  tokenId: string;
  takerOdds: number;
  updatedAt: number;
}

export type PolyOddsMessage =
  | { type: "polyOddsSnapshot"; data: PolyOddsEntry[] }
  | { type: "polyOddsUpdate"; tokenId: string; takerOdds: number; updatedAt: number };

type IncomingMessage =
  | { type: "snapshot"; data: BestOddsEntry[] }
  | { type: "update"; data: BestOddsEntry }
  | { type: "bookSnapshot"; marketHash: string; outcomeOne: BookLevel[]; outcomeTwo: BookLevel[] }
  | { type: "bookUpdate"; marketHash: string; outcomeOne: BookLevel[]; outcomeTwo: BookLevel[] }
  | { type: "polyBookSnapshot"; tokenId: string; bids?: BookLevel[]; asks?: BookLevel[]; bestBid?: number; bestAsk?: number }
  | { type: "polyBookUpdate"; tokenId: string; bids?: BookLevel[]; asks?: BookLevel[]; bestBid?: number; bestAsk?: number }
  | { type: "polyOddsSnapshot"; data: PolyOddsEntry[] }
  | { type: "polyOddsUpdate"; tokenId: string; takerOdds: number; updatedAt: number }
  | { type: "balance_update"; data: BalanceUpdateData }
  | { type: "position_update"; data: unknown[] }
  | { type: "poly_status" }
  & PolyStatusMessage
  | { type: "pong" }
  | { type: "risk_runtime_log"; data: RiskRuntimeLogEnvelope }
  | { type: "risk_runtime_log_snapshot"; data: RiskRuntimeLogEnvelope[] }
  | MarketLifecycleMessage;

export type RiskRuntimeLogMessage =
  | { type: "risk_runtime_log"; data: RiskRuntimeLogEnvelope }
  | { type: "risk_runtime_log_snapshot"; data: RiskRuntimeLogEnvelope[] };

type OddsListener = (
  msg: { type: "snapshot"; data: BestOddsEntry[] } | { type: "update"; data: BestOddsEntry },
) => void;
type BookListener = (frame: BookFrame) => void;
type PolyBookListener = (frame: PolyBookFrame) => void;
type PolyOddsListener = (msg: PolyOddsMessage) => void;
type MarketLifecycleListener = (msg: MarketLifecycleMessage) => void;
type StatusListener = (connected: boolean) => void;
type ConnectionStateListener = (state: WSConnectionState) => void;
type PolyStatusListener = (msg: PolyStatusMessage) => void;
type BalanceUpdateListener = (msg: BalanceUpdateMessage) => void;
type PositionUpdateListener = (msg: PositionUpdateMessage) => void;
type RuntimeLogListener = (msg: RiskRuntimeLogMessage) => void;

const WS_URL = (() => {
  if (import.meta.env.VITE_WS_URL) return import.meta.env.VITE_WS_URL as string;
  const base = import.meta.env.VITE_API_BASE_URL as string | undefined;
  if (base) return base.replace(/^http/, "ws") + "/ws";
  if (typeof window === "undefined") return "ws://127.0.0.1:7633/ws";
  const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${proto}//${window.location.host}/ws`;
})();

const RISK_WS_URL = WS_URL + "/risk";

const isBrowser = () => typeof window !== "undefined" && typeof WebSocket !== "undefined";

function backoffMs(cfg: WSClientConfig, attempt: number): number {
  const base = cfg.wsDashBackoffBaseSec * 1000;
  const max = cfg.wsDashBackoffMaxSec * 1000;
  const exp = base * Math.pow(2, Math.max(0, attempt - 1));
  const delay = Math.min(exp, max);
  const j = cfg.wsDashBackoffJitterPct / 100;
  return Math.round(delay * (1 + (Math.random() * 2 - 1) * j));
}

class WsBus {
  private ws: WebSocket | null = null;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private pingTimer: ReturnType<typeof setInterval> | null = null;
  private pongTimer: ReturnType<typeof setTimeout> | null = null;
  private rafId = 0;
  private lastRaf = 0;
  private url: string;
  private label: string;
  private state: WSConnectionState = "IDLE";
  private reconnectAttempt = 0;
  private cfg: WSClientConfig = getWSConfig();
  private lifecycleBound = false;

  private oddsListeners: OddsListener[] = [];
  private bookListeners: BookListener[] = [];
  private polyBookListeners: PolyBookListener[] = [];
  private polyOddsListeners: PolyOddsListener[] = [];
  private marketLifecycleListeners: MarketLifecycleListener[] = [];
  private statusListeners: StatusListener[] = [];
  private connectionStateListeners: ConnectionStateListener[] = [];
  private polyStatusListeners: PolyStatusListener[] = [];
  private balanceUpdateListeners: BalanceUpdateListener[] = [];
  private positionUpdateListeners: PositionUpdateListener[] = [];
  private runtimeLogListeners: RuntimeLogListener[] = [];

  private oddsSubRefs = new Set<string>();
  private bookSubRefs = new Set<string>();
  private polyBookSubRefs = new Map<string, number>();
  private polyBookLastFrameMs = new Map<string, number>();
  private polyOddsSubRefs = new Map<string, number>();

  constructor(url: string, label: string) {
    this.url = url;
    this.label = label;
    if (isBrowser()) {
      subscribeWSConfig((c) => {
        this.cfg = c;
        this.applySettings();
      });
      this.bindLifecycle();
      this.connect(true);
    }
  }

  private bindLifecycle() {
    if (this.lifecycleBound || !isBrowser()) return;
    this.lifecycleBound = true;
    window.addEventListener("online", () => this.reconnect(true));
    document.addEventListener("visibilitychange", () => {
      if (document.visibilityState === "visible") this.reconnect(true);
    });
    const rafLoop = (ts: number) => {
      if (this.lastRaf > 0) {
        const delta = ts - this.lastRaf;
        if (delta > this.cfg.wsDashSleepThresholdSec * 1000) {
          appendWSLog("relay", "warn", `${this.label}: sleep/wake detected (${Math.round(delta)}ms)`);
          this.reconnect(true);
        }
      }
      this.lastRaf = ts;
      this.rafId = requestAnimationFrame(rafLoop);
    };
    this.rafId = requestAnimationFrame(rafLoop);
  }

  getConnectionState(): WSConnectionState {
    return this.state;
  }

  applySettings() {
    this.clearPingTimers();
    if (this.state === "CONNECTED") {
      this.startPingLoop();
    }
  }

  reconnect(immediate = false) {
    this.clearReconnectTimer();
    if (this.ws) {
      try {
        this.ws.close();
      } catch {
        /* ignore */
      }
      this.ws = null;
    }
    if (immediate) {
      this.reconnectAttempt = 0;
      this.connect(false);
      return;
    }
    this.scheduleReconnect();
  }

  private setState(next: WSConnectionState, logMsg?: string) {
    if (this.state === next) return;
    this.state = next;
    if (logMsg) appendWSLog("relay", next === "CONNECTED" ? "info" : "warn", `${this.label}: ${logMsg}`);
    if (next === "CONNECTED") {
      setRelayState("connected");
    } else if (next === "RECONNECTING" || next === "CONNECTING") {
      setRelayState("reconnecting", { attempt: this.reconnectAttempt });
    } else if (next === "STALE" || next === "IDLE") {
      setRelayState("disconnected", { attempt: this.reconnectAttempt });
    }
    for (const l of this.connectionStateListeners) l(next);
    for (const l of this.statusListeners) l(next === "CONNECTED");
  }

  private connect(isInitial: boolean) {
    if (!isBrowser()) return;
    if (this.ws && (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)) {
      return;
    }
    this.clearReconnectTimer();
    this.setState("CONNECTING");
    this.ws = new WebSocket(this.url);

    this.ws.onopen = () => {
      this.reconnectAttempt = 0;
      this.setState("CONNECTED", "connected");
      this.resubscribeAll();
      this.startPingLoop();
    };

    this.ws.onclose = () => {
      this.ws = null;
      this.clearPingTimers();
      this.setState("RECONNECTING", "closed");
      if (this.cfg.wsAutoReconnectOnDisconnect) {
        this.scheduleReconnect();
      }
    };

    this.ws.onerror = () => {
      appendWSLog("relay", "warn", `${this.label}: error`);
    };

    this.ws.onmessage = (ev) => {
      this.onPongReceived();
      try {
        const msg: IncomingMessage = JSON.parse(ev.data);
        if (msg.type === "pong") return;
        this.dispatchMessage(msg);
      } catch {
        /* ignore */
      }
    };

    if (!isInitial) {
      appendWSLog("relay", "info", `${this.label}: connecting`);
    }
  }

  private scheduleReconnect() {
    this.clearReconnectTimer();
    this.reconnectAttempt += 1;
    const delay = backoffMs(this.cfg, this.reconnectAttempt);
    const nextAt = Date.now() + delay;
    setRelayState("reconnecting", { nextRetryAt: nextAt, attempt: this.reconnectAttempt });
    appendWSLog("relay", "info", `${this.label}: retry in ${Math.round(delay / 1000)}s (#${this.reconnectAttempt})`);
    this.reconnectTimer = setTimeout(() => this.connect(false), delay);
  }

  private clearReconnectTimer() {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
  }

  private startPingLoop() {
    this.clearPingTimers();
    const iv = this.cfg.wsDashPingIntervalSec * 1000;
    this.pingTimer = setInterval(() => {
      if (this.ws?.readyState === WebSocket.OPEN) {
        this.send({ type: "ping" });
        if (this.pongTimer) clearTimeout(this.pongTimer);
        this.pongTimer = setTimeout(() => {
          appendWSLog("relay", "warn", `${this.label}: pong timeout`);
          this.setState("STALE", "stale");
          this.reconnect(true);
        }, this.cfg.wsDashPongTimeoutSec * 1000);
      }
    }, iv);
  }

  private onPongReceived() {
    if (this.pongTimer) {
      clearTimeout(this.pongTimer);
      this.pongTimer = null;
    }
  }

  private clearPingTimers() {
    if (this.pingTimer) {
      clearInterval(this.pingTimer);
      this.pingTimer = null;
    }
    if (this.pongTimer) {
      clearTimeout(this.pongTimer);
      this.pongTimer = null;
    }
  }

  private resubscribeAll() {
    if (this.oddsSubRefs.size > 0) {
      this.send({ type: "subscribeOdds", marketHashes: Array.from(this.oddsSubRefs) });
    }
    for (const marketHash of this.bookSubRefs) {
      this.send({ type: "subscribeBook", marketHash });
    }
    for (const tokenId of this.polyBookSubRefs.keys()) {
      this.send({ type: "subscribePolyBook", tokenId });
    }
    if (this.polyOddsSubRefs.size > 0) {
      this.send({ type: "subscribePolyOdds", tokenIds: Array.from(this.polyOddsSubRefs.keys()) });
    }
  }

  private dispatchMessage(msg: IncomingMessage) {
    if (msg.type === "snapshot" || msg.type === "update") {
      for (const l of this.oddsListeners) l(msg);
    } else if (msg.type === "bookSnapshot" || msg.type === "bookUpdate") {
      const frame: BookFrame = {
        marketHash: msg.marketHash,
        outcomeOne: msg.outcomeOne,
        outcomeTwo: msg.outcomeTwo,
      };
      for (const l of this.bookListeners) l(frame);
    } else if (msg.type === "polyBookSnapshot" || msg.type === "polyBookUpdate") {
      const frame: PolyBookFrame = {
        tokenId: msg.tokenId,
        bids: msg.bids,
        asks: msg.asks,
        bestBid: msg.bestBid,
        bestAsk: msg.bestAsk,
      };
      if (frame.tokenId) {
        this.polyBookLastFrameMs.set(frame.tokenId, Date.now());
      }
      for (const l of this.polyBookListeners) l(frame);
    } else if (msg.type === "polyOddsSnapshot" || msg.type === "polyOddsUpdate") {
      for (const l of this.polyOddsListeners) l(msg);
    } else if (msg.type === "marketsSnapshot" || msg.type === "marketUpsert" || msg.type === "marketRemoved") {
      for (const l of this.marketLifecycleListeners) l(msg);
    } else if (msg.type === "poly_status") {
      applyUpstreamPolyStatus(msg);
      for (const l of this.polyStatusListeners) l(msg);
    } else if (msg.type === "balance_update") {
      for (const l of this.balanceUpdateListeners) l(msg);
    } else if (msg.type === "position_update") {
      for (const l of this.positionUpdateListeners) l(msg);
    } else if (msg.type === "risk_runtime_log" || msg.type === "risk_runtime_log_snapshot") {
      for (const l of this.runtimeLogListeners) l(msg);
    }
  }

  private send(obj: unknown) {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(obj));
    }
  }

  onConnectionStateChange(l: ConnectionStateListener) {
    this.connectionStateListeners.push(l);
    l(this.state);
    return () => {
      this.connectionStateListeners = this.connectionStateListeners.filter((x) => x !== l);
    };
  }

  onStatusChange(l: StatusListener) {
    this.statusListeners.push(l);
    l(this.state === "CONNECTED");
    return () => {
      this.statusListeners = this.statusListeners.filter((x) => x !== l);
    };
  }

  onPolyStatus(l: PolyStatusListener) {
    this.polyStatusListeners.push(l);
    return () => {
      this.polyStatusListeners = this.polyStatusListeners.filter((x) => x !== l);
    };
  }

  onBalanceUpdate(l: BalanceUpdateListener) {
    this.balanceUpdateListeners.push(l);
    return () => {
      this.balanceUpdateListeners = this.balanceUpdateListeners.filter((x) => x !== l);
    };
  }

  onPositionUpdate(l: PositionUpdateListener) {
    this.positionUpdateListeners.push(l);
    return () => {
      this.positionUpdateListeners = this.positionUpdateListeners.filter((x) => x !== l);
    };
  }

  onRuntimeLog(l: RuntimeLogListener) {
    this.runtimeLogListeners.push(l);
    return () => {
      this.runtimeLogListeners = this.runtimeLogListeners.filter((x) => x !== l);
    };
  }

  onMarketLifecycle(l: MarketLifecycleListener) {
    this.marketLifecycleListeners.push(l);
    return () => {
      this.marketLifecycleListeners = this.marketLifecycleListeners.filter((x) => x !== l);
    };
  }

  subscribeOdds(marketHash: string, l: OddsListener) {
    this.oddsListeners.push(l);
    if (!this.oddsSubRefs.has(marketHash)) {
      this.oddsSubRefs.add(marketHash);
      this.send({ type: "subscribeOdds", marketHashes: [marketHash] });
    }
    return () => {
      this.oddsListeners = this.oddsListeners.filter((x) => x !== l);
    };
  }

  subscribeBook(marketHash: string, l: BookListener) {
    this.bookListeners.push(l);
    if (!this.bookSubRefs.has(marketHash)) {
      this.bookSubRefs.add(marketHash);
      this.send({ type: "subscribeBook", marketHash });
    }
    return () => {
      this.bookListeners = this.bookListeners.filter((x) => x !== l);
    };
  }

  subscribePolyBook(tokenId: string, l: PolyBookListener) {
    this.polyBookListeners.push(l);
    const count = this.polyBookSubRefs.get(tokenId) || 0;
    this.polyBookSubRefs.set(tokenId, count + 1);
    if (count === 0) {
      this.send({ type: "subscribePolyBook", tokenId });
    }
    return () => {
      this.polyBookListeners = this.polyBookListeners.filter((x) => x !== l);
      const newCount = (this.polyBookSubRefs.get(tokenId) || 1) - 1;
      if (newCount <= 0) {
        this.polyBookSubRefs.delete(tokenId);
        this.send({ type: "unsubscribePolyBook", tokenId });
      } else {
        this.polyBookSubRefs.set(tokenId, newCount);
      }
    };
  }

  /** Local WS ref-count + last polyBook frame time for one token. */
  getPolyBookLocalState(tokenId: string): { subscribed: boolean; lastFrameMs: number | null } {
    const refs = this.polyBookSubRefs.get(tokenId) || 0;
    const last = this.polyBookLastFrameMs.get(tokenId);
    return { subscribed: refs > 0, lastFrameMs: last ?? null };
  }

  /** Re-send subscribePolyBook without changing listener ref counts. */
  resendPolyBookSubscribe(tokenId: string) {
    if ((this.polyBookSubRefs.get(tokenId) || 0) > 0) {
      this.send({ type: "subscribePolyBook", tokenId });
    }
  }

  subscribePolyOdds(tokenId: string, l: PolyOddsListener) {
    this.polyOddsListeners.push(l);
    const count = this.polyOddsSubRefs.get(tokenId) || 0;
    this.polyOddsSubRefs.set(tokenId, count + 1);
    if (count === 0) {
      this.send({ type: "subscribePolyOdds", tokenIds: [tokenId] });
    }
    return () => {
      this.polyOddsListeners = this.polyOddsListeners.filter((x) => x !== l);
      const newCount = (this.polyOddsSubRefs.get(tokenId) || 1) - 1;
      if (newCount <= 0) {
        this.polyOddsSubRefs.delete(tokenId);
      } else {
        this.polyOddsSubRefs.set(tokenId, newCount);
      }
    };
  }
}

export const wsBus = new WsBus(WS_URL, "Dashboard WS");
export const riskWsBus = new WsBus(RISK_WS_URL, "Risk WS");
