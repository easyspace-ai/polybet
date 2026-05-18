/**
 * Polymarket WebSocket Client - Type Definitions
 */

// ============================================================================
// Common Types
// ============================================================================

/** WebSocket connection state */
export type ConnectionState = 'disconnected' | 'connecting' | 'connected' | 'reconnecting';

/** Base event emitter callback */
export type EventCallback<T = unknown> = (data: T) => void;

/** Error callback */
export type ErrorCallback = (error: Error) => void;

/** Connection state change callback */
export type StateChangeCallback = (state: ConnectionState, previousState: ConnectionState) => void;

// ============================================================================
// CLOB WebSocket Types
// ============================================================================

/** CLOB WebSocket channel types */
export type ClobChannelType = 'market' | 'user';

/** CLOB API credentials for authenticated channels */
export interface ClobAuth {
  apiKey: string;
  secret: string;
  passphrase: string;
}

/** CLOB subscription message */
export interface ClobSubscribeMessage {
  auth?: ClobAuth;
  type: 'USER' | 'MARKET';
  markets?: string[];    // condition IDs for USER channel
  assets_ids?: string[]; // token IDs for MARKET channel
}

/** CLOB subscribe/unsubscribe to assets message */
export interface ClobAssetSubscriptionMessage {
  assets_ids: string[];
  type?: 'market';
  custom_feature_enabled?: boolean;
  initial_dump?: boolean;
  operation?: 'subscribe' | 'unsubscribe';
}

// ----------------------------------------------------------------------------
// Market Channel Event Types
// ----------------------------------------------------------------------------

/** Order summary in orderbook */
export interface OrderSummary {
  price: string;
  size: string;
}

/** Book event - Full orderbook snapshot */
export interface ClobBookEvent {
  event_type: 'book';
  asset_id: string;
  market: string;
  timestamp: string;
  hash: string;
  bids: OrderSummary[];
  asks: OrderSummary[];
}

/** Price change entry */
export interface PriceChange {
  asset_id: string;
  price: string;
  size: string;
  side: 'BUY' | 'SELL';
  hash: string;
  best_bid: string;
  best_ask: string;
}

/** Price change event - Order placement/cancellation */
export interface ClobPriceChangeEvent {
  event_type: 'price_change';
  market: string;
  price_changes: PriceChange[];
  timestamp: string;
}

/** Tick size change event */
export interface ClobTickSizeChangeEvent {
  event_type: 'tick_size_change';
  asset_id: string;
  market: string;
  old_tick_size: string;
  new_tick_size: string;
  side?: string;
  timestamp: string;
}

/** Last trade price event */
export interface ClobLastTradePriceEvent {
  event_type: 'last_trade_price';
  asset_id: string;
  market: string;
  price: string;
  side: 'BUY' | 'SELL';
  size: string;
  fee_rate_bps: string;
  timestamp: string;
}

/** All market channel events */
export type ClobMarketEvent =
  | ClobBookEvent
  | ClobPriceChangeEvent
  | ClobTickSizeChangeEvent
  | ClobLastTradePriceEvent;

// ----------------------------------------------------------------------------
// User Channel Event Types
// ----------------------------------------------------------------------------

/** Maker order in trade */
export interface MakerOrder {
  asset_id: string;
  matched_amount: string;
  order_id: string;
  outcome: string;
  owner: string;
  price: string;
}

/** Trade status */
export type TradeStatus = 'MATCHED' | 'MINED' | 'CONFIRMED' | 'RETRYING' | 'FAILED';

/** Trade event */
export interface ClobTradeEvent {
  event_type: 'trade';
  type: 'TRADE';
  id: string;
  asset_id: string;
  market: string;
  side: 'BUY' | 'SELL';
  price: string;
  size: string;
  outcome: string;
  status: TradeStatus;
  owner: string;
  trade_owner: string;
  taker_order_id: string;
  maker_orders: MakerOrder[];
  matchtime: string;
  last_update: string;
  timestamp: string;
}

/** Order event type */
export type OrderEventType = 'PLACEMENT' | 'UPDATE' | 'CANCELLATION';

/** Order event */
export interface ClobOrderEvent {
  event_type: 'order';
  type: OrderEventType;
  id: string;
  asset_id: string;
  market: string;
  side: 'BUY' | 'SELL';
  price: string;
  original_size: string;
  size_matched: string;
  outcome: string;
  owner: string;
  order_owner: string;
  associate_trades: string[] | null;
  timestamp: string;
}

/** All user channel events */
export type ClobUserEvent = ClobTradeEvent | ClobOrderEvent;

/** All CLOB events */
export type ClobEvent = ClobMarketEvent | ClobUserEvent;

// ============================================================================
// RTDS WebSocket Types
// ============================================================================

/** RTDS authentication using CLOB credentials */
export interface RtdsClobAuth {
  key: string;
  secret: string;
  passphrase: string;
}

/** RTDS authentication using wallet address */
export interface RtdsGammaAuth {
  address: string;
}

/** RTDS subscription topic */
export type RtdsTopic =
  | 'crypto_prices'
  | 'crypto_prices_chainlink'
  | 'comments'
  | 'activity';

/** RTDS subscription request */
export interface RtdsSubscription {
  topic: RtdsTopic;
  type: string;
  filters?: string;
  clob_auth?: RtdsClobAuth;
  gamma_auth?: RtdsGammaAuth;
}

/** RTDS subscribe/unsubscribe message */
export interface RtdsActionMessage {
  action: 'subscribe' | 'unsubscribe';
  subscriptions: RtdsSubscription[];
}

/** Base RTDS message structure */
export interface RtdsMessage<T = unknown> {
  topic: RtdsTopic;
  type: string;
  timestamp: number;
  payload: T;
}

// ----------------------------------------------------------------------------
// Crypto Prices Types
// ----------------------------------------------------------------------------

/** Crypto price payload */
export interface CryptoPricePayload {
  symbol: string;
  timestamp: number;
  value: number;
}

/** Crypto price update message */
export type CryptoPriceMessage = RtdsMessage<CryptoPricePayload>;

// ----------------------------------------------------------------------------
// Comments Types
// ----------------------------------------------------------------------------

/** Comment profile */
export interface CommentProfile {
  baseAddress: string;
  displayUsernamePublic: boolean;
  name: string;
  proxyWallet: string;
  pseudonym: string;
}

/** Comment payload */
export interface CommentPayload {
  id: string;
  body: string;
  createdAt: string;
  parentCommentID: string | null;
  parentEntityID: number;
  parentEntityType: 'Event' | 'Market';
  profile: CommentProfile;
  reactionCount: number;
  replyAddress: string;
  reportCount: number;
  userAddress: string;
}

/** Comment event type */
export type CommentEventType =
  | 'comment_created'
  | 'comment_removed'
  | 'reaction_created'
  | 'reaction_removed';

/** Comment message */
export type CommentMessage = RtdsMessage<CommentPayload>;

// ============================================================================
// Client Configuration Types
// ============================================================================

/** Logger interface for customizing log output */
export interface Logger {
  /** Log error messages */
  error(message: string, ...args: unknown[]): void;
  /** Log warning messages (optional) */
  warn?(message: string, ...args: unknown[]): void;
  /** Log debug messages (optional) */
  debug?(message: string, ...args: unknown[]): void;
}

/** Proxy agent options */
export interface ProxyOptions {
  /** HTTP proxy URL (e.g., 'http://127.0.0.1:15236') */
  url: string;
  /** Optional headers for proxy authentication */
  headers?: Record<string, string>;
}

/** Base WebSocket client options */
export interface BaseClientOptions {
  /** WebSocket URL */
  url: string;
  /** Enable automatic reconnection (default: true) */
  autoReconnect?: boolean;
  /** Maximum reconnection attempts (default: Infinity) */
  maxReconnectAttempts?: number;
  /** Base delay between reconnection attempts in ms (default: 1000) */
  reconnectDelay?: number;
  /** Maximum reconnection delay in ms (default: 30000) */
  maxReconnectDelay?: number;
  /** Heartbeat/ping interval in ms (default: 30000) */
  heartbeatInterval?: number;
  /** Connection timeout in ms (default: 10000) */
  connectionTimeout?: number;
  /** Custom logger for error and debug messages (default: console) */
  logger?: Logger;
  /** HTTP proxy URL (e.g., 'http://127.0.0.1:15236') */
  proxyUrl?: string;
  /** Optional headers for proxy authentication */
  proxyHeaders?: Record<string, string>;
}

/** User-channel subscribe wire format (`legacy` matches Go marketstream / CLOB subscribe frame). */
export type ClobUserSubscribeMode = 'legacy' | 'typed';

/** CLOB client options */
export interface ClobClientOptions extends Omit<BaseClientOptions, 'url'> {
  /** Custom WebSocket URL (optional, uses default if not provided) */
  url?: string;
  /** Authentication credentials for user channel */
  auth?: ClobAuth;
  /**
   * User feed subscribe format.
   * @default 'legacy' — `{ type: "subscribe", operation: "subscribe", initial_dump: true, auth }`
   */
  userSubscribeMode?: ClobUserSubscribeMode;
}

/** RTDS client options */
export interface RtdsClientOptions extends Omit<BaseClientOptions, 'url'> {
  /** Custom WebSocket URL (optional, uses default if not provided) */
  url?: string;
}

// ============================================================================
// Event Map Types (for typed event emitter)
// ============================================================================

/** Common client event map */
export interface ClientEventMap {
  connected: void;
  disconnected: { code: number; reason: string };
  reconnecting: { attempt: number; maxAttempts: number };
  error: Error;
  stateChange: { state: ConnectionState; previousState: ConnectionState };
}
