/**
 * Polymarket WebSocket Client
 * Zero-dependency TypeScript client for Polymarket WebSocket APIs
 *
 * @packageDocumentation
 */

// Core clients
export { ClobClient, ClobMarketClient, ClobUserClient } from './clob-client.js';
export { RtdsClient } from './rtds-client.js';

// Base class (for advanced use cases)
export { BaseWebSocketClient } from './base-client.js';

// Event emitter utility
export { TypedEventEmitter } from './event-emitter.js';

// Types
export type {
  // Connection types
  ConnectionState,
  EventCallback,
  ErrorCallback,
  StateChangeCallback,

  // CLOB types
  ClobChannelType,
  ClobAuth,
  ClobSubscribeMessage,
  ClobAssetSubscriptionMessage,
  ClobMarketEvent,
  ClobUserEvent,
  ClobEvent,

  // CLOB Market events
  OrderSummary,
  ClobBookEvent,
  PriceChange,
  ClobPriceChangeEvent,
  ClobTickSizeChangeEvent,
  ClobLastTradePriceEvent,

  // CLOB User events
  MakerOrder,
  TradeStatus,
  ClobTradeEvent,
  OrderEventType,
  ClobOrderEvent,

  // RTDS types
  RtdsClobAuth,
  RtdsGammaAuth,
  RtdsTopic,
  RtdsSubscription,
  RtdsActionMessage,
  RtdsMessage,

  // RTDS payloads
  CryptoPricePayload,
  CommentPayload,
  CommentProfile,
  CommentEventType,
  CryptoPriceMessage,
  CommentMessage,

  // Client options
  BaseClientOptions,
  ClobClientOptions,
  ClobUserSubscribeMode,
  RtdsClientOptions,

  // Event maps
  ClientEventMap,
} from './types.js';
