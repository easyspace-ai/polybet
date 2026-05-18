/**
 * Polymarket RTDS (Real-Time Data Socket) WebSocket Client
 * Supports crypto prices, comments, and activity streams
 */

import { BaseWebSocketClient } from './base-client.js';
import type {
  RtdsClientOptions,
  RtdsSubscription,
  RtdsActionMessage,
  RtdsMessage,
  RtdsTopic,
  RtdsClobAuth,
  RtdsGammaAuth,
  CryptoPricePayload,
  CommentPayload,
} from './types.js';

/** Default RTDS WebSocket URL */
const DEFAULT_RTDS_WSS_URL = 'wss://ws-live-data.polymarket.com';

/** Ping interval for RTDS (recommended 5 seconds) */
const RTDS_PING_INTERVAL = 5000;

/** Subscription configuration */
interface SubscriptionConfig {
  topic: RtdsTopic;
  type: string;
  filters?: string;
  clobAuth?: RtdsClobAuth;
  gammaAuth?: RtdsGammaAuth;
}

/** Event callback type */
type RtdsEventCallback<T> = (message: RtdsMessage<T>) => void;

/**
 * RTDS WebSocket Client
 * Provides real-time data streaming from Polymarket
 */
export class RtdsClient extends BaseWebSocketClient {
  private subscriptions: Map<string, SubscriptionConfig> = new Map();
  private eventCallbacks: Map<string, Set<RtdsEventCallback<unknown>>> = new Map();

  constructor(options: RtdsClientOptions = {}) {
    super({
      ...options,
      url: options.url ?? DEFAULT_RTDS_WSS_URL,
      heartbeatInterval: options.heartbeatInterval ?? RTDS_PING_INTERVAL,
    });
  }

  // ============================================================================
  // Crypto Prices Subscriptions
  // ============================================================================

  /**
   * Subscribe to crypto prices from Binance
   * @param symbols Optional array of symbols to filter (e.g., ['btcusdt', 'ethusdt'])
   * @throws {TypeError} If symbols is provided but not an array or contains invalid values
   */
  subscribeCryptoPrices(symbols?: string[]): void {
    if (symbols !== undefined) {
      if (!Array.isArray(symbols)) {
        throw new TypeError('symbols must be an array');
      }
      for (const symbol of symbols) {
        if (typeof symbol !== 'string' || symbol.trim() === '') {
          throw new TypeError('Each symbol must be a non-empty string');
        }
      }
    }
    const filters = symbols ? JSON.stringify(symbols.map(s => ({ symbol: s.toLowerCase() }))) : undefined;
    this.addSubscription({
      topic: 'crypto_prices',
      type: 'update',
      filters,
    });
  }

  /**
   * Unsubscribe from crypto prices (Binance)
   */
  unsubscribeCryptoPrices(): void {
    this.removeSubscription('crypto_prices', 'update');
  }

  /**
   * Subscribe to crypto prices from Chainlink
   * @param symbol Optional symbol to filter (e.g., 'eth/usd')
   * @throws {TypeError} If symbol is provided but not a string
   */
  subscribeCryptoPricesChainlink(symbol?: string): void {
    if (symbol !== undefined && (typeof symbol !== 'string' || symbol.trim() === '')) {
      throw new TypeError('symbol must be a non-empty string');
    }
    const filters = symbol ? JSON.stringify({ symbol }) : '';
    this.addSubscription({
      topic: 'crypto_prices_chainlink',
      type: '*',
      filters,
    });
  }

  /**
   * Unsubscribe from crypto prices (Chainlink)
   */
  unsubscribeCryptoPricesChainlink(): void {
    this.removeSubscription('crypto_prices_chainlink', '*');
  }

  /**
   * Listen for crypto price updates (from either source)
   */
  onCryptoPrice(callback: RtdsEventCallback<CryptoPricePayload>): () => void {
    const unsub1 = this.addEventCallback('crypto_prices', callback);
    const unsub2 = this.addEventCallback('crypto_prices_chainlink', callback);
    return () => {
      unsub1();
      unsub2();
    };
  }

  // ============================================================================
  // Comments Subscriptions
  // ============================================================================

  /**
   * Subscribe to comment events
   * @param type Event type ('comment_created', 'comment_removed', 'reaction_created', 'reaction_removed')
   * @param gammaAuth Optional Gamma authentication
   */
  subscribeComments(
    type: 'comment_created' | 'comment_removed' | 'reaction_created' | 'reaction_removed' = 'comment_created',
    gammaAuth?: RtdsGammaAuth
  ): void {
    this.addSubscription({
      topic: 'comments',
      type,
      gammaAuth,
    });
  }

  /**
   * Unsubscribe from comment events
   */
  unsubscribeComments(type: string = 'comment_created'): void {
    this.removeSubscription('comments', type);
  }

  /**
   * Listen for comment events
   */
  onComment(callback: RtdsEventCallback<CommentPayload>): () => void {
    return this.addEventCallback('comments', callback);
  }

  // ============================================================================
  // Activity Subscriptions
  // ============================================================================

  /**
   * Subscribe to activity events
   * @param type Activity event type
   * @param clobAuth Optional CLOB authentication
   * @param gammaAuth Optional Gamma authentication
   * @throws {TypeError} If type is not a non-empty string
   */
  subscribeActivity(
    type: string,
    clobAuth?: RtdsClobAuth,
    gammaAuth?: RtdsGammaAuth
  ): void {
    if (typeof type !== 'string' || type.trim() === '') {
      throw new TypeError('type must be a non-empty string');
    }
    this.addSubscription({
      topic: 'activity',
      type,
      clobAuth,
      gammaAuth,
    });
  }

  /**
   * Unsubscribe from activity events
   * @param type Activity event type
   * @throws {TypeError} If type is not a non-empty string
   */
  unsubscribeActivity(type: string): void {
    if (typeof type !== 'string' || type.trim() === '') {
      throw new TypeError('type must be a non-empty string');
    }
    this.removeSubscription('activity', type);
  }

  /**
   * Listen for activity events
   */
  onActivity(callback: RtdsEventCallback<unknown>): () => void {
    return this.addEventCallback('activity', callback);
  }

  // ============================================================================
  // Generic Subscriptions
  // ============================================================================

  /**
   * Subscribe with custom configuration
   */
  subscribeCustom(config: SubscriptionConfig): void {
    this.addSubscription(config);
  }

  /**
   * Unsubscribe from a custom subscription
   */
  unsubscribeCustom(topic: RtdsTopic, type: string): void {
    this.removeSubscription(topic, type);
  }

  /**
   * Listen for all RTDS messages
   */
  onRtdsMessage(callback: RtdsEventCallback<unknown>): () => void {
    return this.addEventCallback('*', callback);
  }

  // ============================================================================
  // Connection Lifecycle
  // ============================================================================

  protected onConnected(): void {
    // Resubscribe to all subscriptions on reconnection
    if (this.subscriptions.size > 0) {
      this.sendSubscriptions(Array.from(this.subscriptions.values()));
    }
  }

  protected onCleanup(): void {
    this.subscriptions.clear();
    this.eventCallbacks.clear();
  }

  protected handleParsedMessage(data: unknown): void {
    if (!data || typeof data !== 'object') return;

    const message = data as RtdsMessage;

    // Emit to topic-specific listeners
    if (message.topic) {
      this.emitEventCallback(message.topic, message);
    }

    // Emit to wildcard listeners
    this.emitEventCallback('*', message);
  }

  protected sendHeartbeat(): void {
    // RTDS expects PING messages
    try {
      this.send({ type: 'PING' });
    } catch {
      // Ignore send errors during heartbeat
    }
  }

  // ============================================================================
  // Internal Methods
  // ============================================================================

  private getSubscriptionKey(topic: RtdsTopic, type: string): string {
    return `${topic}:${type}`;
  }

  private addSubscription(config: SubscriptionConfig): void {
    const key = this.getSubscriptionKey(config.topic, config.type);
    this.subscriptions.set(key, config);

    if (this.isConnected) {
      this.sendSubscriptions([config]);
    }
  }

  private removeSubscription(topic: RtdsTopic, type: string): void {
    const key = this.getSubscriptionKey(topic, type);
    const config = this.subscriptions.get(key);

    if (config) {
      this.subscriptions.delete(key);

      if (this.isConnected) {
        this.sendUnsubscriptions([config]);
      }
    }
  }

  private sendSubscriptions(configs: SubscriptionConfig[]): void {
    const subscriptions: RtdsSubscription[] = configs.map((config) => ({
      topic: config.topic,
      type: config.type,
      filters: config.filters,
      clob_auth: config.clobAuth,
      gamma_auth: config.gammaAuth,
    }));

    const message: RtdsActionMessage = {
      action: 'subscribe',
      subscriptions,
    };

    this.send(message);
  }

  private sendUnsubscriptions(configs: SubscriptionConfig[]): void {
    const subscriptions: RtdsSubscription[] = configs.map((config) => ({
      topic: config.topic,
      type: config.type,
      filters: config.filters,
    }));

    const message: RtdsActionMessage = {
      action: 'unsubscribe',
      subscriptions,
    };

    this.send(message);
  }

  private addEventCallback<T>(event: string, callback: RtdsEventCallback<T>): () => void {
    if (!this.eventCallbacks.has(event)) {
      this.eventCallbacks.set(event, new Set());
    }
    this.eventCallbacks.get(event)!.add(callback as RtdsEventCallback<unknown>);

    return () => {
      this.eventCallbacks.get(event)?.delete(callback as RtdsEventCallback<unknown>);
    };
  }

  private emitEventCallback(event: string, data: unknown): void {
    const callbacks = this.eventCallbacks.get(event);
    if (callbacks) {
      for (const callback of callbacks) {
        try {
          callback(data as RtdsMessage);
        } catch (error) {
          this.options.logger.error(`Error in RTDS event callback for "${event}":`, error);
        }
      }
    }
  }

  /**
   * Get all active subscriptions
   */
  getSubscriptions(): SubscriptionConfig[] {
    return Array.from(this.subscriptions.values());
  }
}
