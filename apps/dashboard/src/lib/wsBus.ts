// Shared WebSocket bus — single connection per dashboard tab.

import type { Market, RiskRuntimeLogEnvelope } from "./api";

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
  | { type: "poly_status"; polyOrderbookConnected?: boolean; polyUserConnected?: boolean }
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

class WsBus {
  private ws: WebSocket | null = null;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private url: string;
  private oddsListeners: OddsListener[] = [];
  private bookListeners: BookListener[] = [];
  private polyBookListeners: PolyBookListener[] = [];
  private polyOddsListeners: PolyOddsListener[] = [];
  private marketLifecycleListeners: MarketLifecycleListener[] = [];
  private statusListeners: StatusListener[] = [];
  private polyStatusListeners: PolyStatusListener[] = [];
  private balanceUpdateListeners: BalanceUpdateListener[] = [];
  private positionUpdateListeners: PositionUpdateListener[] = [];
  private runtimeLogListeners: RuntimeLogListener[] = [];

  private oddsSubRefs = new Set<string>();
  private bookSubRefs = new Set<string>();
  private polyBookSubRefs = new Map<string, number>(); // tokenId -> refCount
  private polyOddsSubRefs = new Map<string, number>(); // tokenId -> refCount

  constructor(url: string) {
    this.url = url;
    if (isBrowser()) this.connect();
  }

  private connect() {
    if (this.ws) return;
    this.ws = new WebSocket(this.url);
    this.ws.onopen = () => {
      for (const l of this.statusListeners) l(true);
      // Re-subscribe
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
    };
    this.ws.onclose = () => {
      this.ws = null;
      for (const l of this.statusListeners) l(false);
      this.reconnectTimer = setTimeout(() => this.connect(), 3000);
    };
    this.ws.onmessage = (ev) => {
      try {
        const msg: IncomingMessage = JSON.parse(ev.data);
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
          for (const l of this.polyBookListeners) l(frame);
        } else if (msg.type === "polyOddsSnapshot" || msg.type === "polyOddsUpdate") {
          for (const l of this.polyOddsListeners) l(msg);
        } else if (msg.type === "marketsSnapshot" || msg.type === "marketUpsert" || msg.type === "marketRemoved") {
          for (const l of this.marketLifecycleListeners) l(msg);
        } else if (msg.type === "poly_status") {
          for (const l of this.polyStatusListeners) l(msg);
        } else if (msg.type === "balance_update") {
          for (const l of this.balanceUpdateListeners) l(msg);
        } else if (msg.type === "position_update") {
          for (const l of this.positionUpdateListeners) l(msg);
        } else if (msg.type === "risk_runtime_log" || msg.type === "risk_runtime_log_snapshot") {
          for (const l of this.runtimeLogListeners) l(msg);
        }
      } catch (err) {
        // ignore
      }
    };
  }

  private send(obj: any) {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(obj));
    }
  }

  onStatusChange(l: StatusListener) {
    this.statusListeners.push(l);
    l(this.ws?.readyState === WebSocket.OPEN);
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
      // We don't unsubscribe from odds for now to keep it simple
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
      if (this.bookListeners.length === 0) {
        // can unsubscribe if needed
      }
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
        // this.send({ type: "unsubscribePolyBook", tokenId });
      } else {
        this.polyBookSubRefs.set(tokenId, newCount);
      }
    };
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

export const wsBus = new WsBus(WS_URL);
export const riskWsBus = new WsBus(RISK_WS_URL);
