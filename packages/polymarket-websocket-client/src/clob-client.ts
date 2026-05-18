/**
 * Polymarket CLOB WebSocket Client
 * Supports Market (public) and User (authenticated) channels
 */

import { BaseWebSocketClient } from './base-client.js';
import type {
  ClobClientOptions,
  ClobAuth,
  ClobUserSubscribeMode,
  ClobSubscribeMessage,
  ClobAssetSubscriptionMessage,
  ClobMarketEvent,
  ClobUserEvent,
  ClobBookEvent,
  ClobPriceChangeEvent,
  ClobTickSizeChangeEvent,
  ClobLastTradePriceEvent,
  ClobTradeEvent,
  ClobOrderEvent,
} from './types.js';

/** Default CLOB WebSocket URL */
const DEFAULT_CLOB_WSS_URL = 'wss://ws-subscriptions-clob.polymarket.com/ws/market';
const DEFAULT_CLOB_USER_WSS_URL = 'wss://ws-subscriptions-clob.polymarket.com/ws/user';

/** Event callback type */
type EventCallback<T> = (event: T) => void;

/**
 * CLOB Market Channel Client
 * Public channel for orderbook updates and price changes
 */
export class ClobMarketClient extends BaseWebSocketClient {
  private assetIds: Set<string> = new Set();
  private eventCallbacks: Map<string, Set<EventCallback<unknown>>> = new Map();

  constructor(options: ClobClientOptions = {}) {
    super({
      ...options,
      url: options.url ?? DEFAULT_CLOB_WSS_URL,
    });
  }

  /**
   * Subscribe to market updates for specific asset IDs (token IDs)
   * @param assetIds Array of asset/token IDs to subscribe to
   * @throws {TypeError} If assetIds is not an array or contains non-string values
   */
  subscribe(assetIds: string[]): void {
    if (!Array.isArray(assetIds)) {
      throw new TypeError('assetIds must be an array');
    }
    for (const id of assetIds) {
      if (typeof id !== 'string' || id.trim() === '') {
        throw new TypeError('Each asset ID must be a non-empty string');
      }
    }

    const newAssets = assetIds.filter((id) => !this.assetIds.has(id));
    if (newAssets.length === 0) return;

    for (const id of newAssets) {
      this.assetIds.add(id);
    }

    if (this.isConnected) {
      this.sendSubscription(newAssets);
    }
  }

  /**
   * Unsubscribe from market updates for specific asset IDs
   * @param assetIds Array of asset/token IDs to unsubscribe from
   * @throws {TypeError} If assetIds is not an array or contains non-string values
   */
  unsubscribe(assetIds: string[]): void {
    if (!Array.isArray(assetIds)) {
      throw new TypeError('assetIds must be an array');
    }
    for (const id of assetIds) {
      if (typeof id !== 'string' || id.trim() === '') {
        throw new TypeError('Each asset ID must be a non-empty string');
      }
    }

    const existingAssets = assetIds.filter((id) => this.assetIds.has(id));
    if (existingAssets.length === 0) return;

    for (const id of existingAssets) {
      this.assetIds.delete(id);
    }

    if (this.isConnected) {
      this.sendUnsubscription(existingAssets);
    }
  }

  /**
   * Get currently subscribed asset IDs
   */
  getSubscribedAssets(): string[] {
    return Array.from(this.assetIds);
  }

  /**
   * Listen for book events (full orderbook snapshots)
   */
  onBook(callback: EventCallback<ClobBookEvent>): () => void {
    return this.addEventCallback('book', callback);
  }

  /**
   * Listen for price change events
   */
  onPriceChange(callback: EventCallback<ClobPriceChangeEvent>): () => void {
    return this.addEventCallback('price_change', callback);
  }

  /**
   * Listen for tick size change events
   */
  onTickSizeChange(callback: EventCallback<ClobTickSizeChangeEvent>): () => void {
    return this.addEventCallback('tick_size_change', callback);
  }

  /**
   * Listen for last trade price events
   */
  onLastTradePrice(callback: EventCallback<ClobLastTradePriceEvent>): () => void {
    return this.addEventCallback('last_trade_price', callback);
  }

  /**
   * Listen for all market events
   */
  onMarketMessage(callback: EventCallback<ClobMarketEvent>): () => void {
    return this.addEventCallback('message', callback);
  }

  protected onConnected(): void {
    if (this.assetIds.size > 0) {
      this.sendSubscription(Array.from(this.assetIds));
    }
  }

  protected onCleanup(): void {
    this.assetIds.clear();
    this.eventCallbacks.clear();
  }

  protected handleParsedMessage(data: unknown): void {
    if (!data) return;

    if (Array.isArray(data)) {
      for (const item of data) {
        this.processMarketEvent(item);
      }
    } else if (typeof data === 'object') {
      this.processMarketEvent(data);
    }
  }

  private processMarketEvent(event: unknown): void {
    if (!event || typeof event !== 'object') return;

    const marketEvent = event as ClobMarketEvent;
    const eventType = marketEvent.event_type;

    if (eventType) {
      this.emitEventCallback(eventType, marketEvent);
      this.emitEventCallback('message', marketEvent);
    }
  }

  private sendSubscription(assetIds: string[]): void {
    for (let i = 0; i < assetIds.length; i += 50) {
      const chunk = assetIds.slice(i, i + 50);
      const message: ClobAssetSubscriptionMessage = {
        assets_ids: chunk,
        type: 'market',
        custom_feature_enabled: true,
        initial_dump: true,
        operation: 'subscribe',
      };
      this.send(message);
    }
  }

  private sendUnsubscription(assetIds: string[]): void {
    for (let i = 0; i < assetIds.length; i += 50) {
      const chunk = assetIds.slice(i, i + 50);
      const message: ClobAssetSubscriptionMessage = {
        assets_ids: chunk,
        type: 'market',
        custom_feature_enabled: true,
        operation: 'unsubscribe',
      };
      this.send(message);
    }
  }

  private addEventCallback<T>(event: string, callback: EventCallback<T>): () => void {
    if (!this.eventCallbacks.has(event)) {
      this.eventCallbacks.set(event, new Set());
    }
    this.eventCallbacks.get(event)!.add(callback as EventCallback<unknown>);

    return () => {
      this.eventCallbacks.get(event)?.delete(callback as EventCallback<unknown>);
    };
  }

  private emitEventCallback(event: string, data: unknown): void {
    const callbacks = this.eventCallbacks.get(event);
    if (callbacks) {
      for (const callback of callbacks) {
        try {
          callback(data);
        } catch (error) {
          this.options.logger.error(`Error in CLOB market event callback for "${event}":`, error);
        }
      }
    }
  }
}

/**
 * CLOB User Channel Client
 * Authenticated channel for user's orders and trades
 */
export class ClobUserClient extends BaseWebSocketClient {
  private auth: ClobAuth;
  private readonly userSubscribeMode: ClobUserSubscribeMode;
  private marketIds: Set<string> = new Set();
  private eventCallbacks: Map<string, Set<EventCallback<unknown>>> = new Map();

  /**
   * Create a new CLOB User Channel client
   * @param auth Authentication credentials
   * @param options Client options
   * @throws {TypeError} If auth credentials are missing or invalid
   */
  constructor(auth: ClobAuth, options: ClobClientOptions = {}) {
    if (!auth || typeof auth !== 'object') {
      throw new TypeError('auth credentials are required');
    }
    if (typeof auth.apiKey !== 'string' || auth.apiKey.trim() === '') {
      throw new TypeError('auth.apiKey must be a non-empty string');
    }
    if (typeof auth.secret !== 'string' || auth.secret.trim() === '') {
      throw new TypeError('auth.secret must be a non-empty string');
    }
    if (typeof auth.passphrase !== 'string' || auth.passphrase.trim() === '') {
      throw new TypeError('auth.passphrase must be a non-empty string');
    }

    super({
      ...options,
      url: options.url ?? DEFAULT_CLOB_USER_WSS_URL,
    });
    this.auth = auth;
    this.userSubscribeMode = options.userSubscribeMode ?? 'legacy';
  }

  /**
   * Subscribe to updates for specific markets (condition IDs)
   * @param marketIds Array of market/condition IDs to subscribe to
   * @throws {TypeError} If marketIds is not an array or contains non-string values
   */
  subscribe(marketIds: string[]): void {
    if (!Array.isArray(marketIds)) {
      throw new TypeError('marketIds must be an array');
    }
    for (const id of marketIds) {
      if (typeof id !== 'string' || id.trim() === '') {
        throw new TypeError('Each market ID must be a non-empty string');
      }
    }

    const newMarkets = marketIds.filter((id) => !this.marketIds.has(id));
    if (newMarkets.length === 0) return;

    for (const id of newMarkets) {
      this.marketIds.add(id);
    }

    // User channel requires sending full subscription on connect
    // If already connected, we need to reconnect to update subscription
    if (this.isConnected) {
      // Send updated subscription
      this.sendFullSubscription();
    }
  }

  /**
   * Unsubscribe from updates for specific markets
   * @param marketIds Array of market/condition IDs to unsubscribe from
   * @throws {TypeError} If marketIds is not an array or contains non-string values
   */
  unsubscribe(marketIds: string[]): void {
    if (!Array.isArray(marketIds)) {
      throw new TypeError('marketIds must be an array');
    }
    for (const id of marketIds) {
      if (typeof id !== 'string' || id.trim() === '') {
        throw new TypeError('Each market ID must be a non-empty string');
      }
    }

    for (const id of marketIds) {
      this.marketIds.delete(id);
    }

    // If already connected, send updated subscription
    if (this.isConnected) {
      this.sendFullSubscription();
    }
  }

  /**
   * Get currently subscribed market IDs
   */
  getSubscribedMarkets(): string[] {
    return Array.from(this.marketIds);
  }

  /**
   * Listen for trade events
   */
  onTrade(callback: EventCallback<ClobTradeEvent>): () => void {
    return this.addEventCallback('trade', callback);
  }

  /**
   * Listen for order events
   */
  onOrder(callback: EventCallback<ClobOrderEvent>): () => void {
    return this.addEventCallback('order', callback);
  }

  /**
   * Listen for all user events
   */
  onUserMessage(callback: EventCallback<ClobUserEvent>): () => void {
    return this.addEventCallback('message', callback);
  }

  protected onConnected(): void {
    // Send subscription with auth on connection
    this.sendFullSubscription();
  }

  protected onCleanup(): void {
    this.marketIds.clear();
    this.eventCallbacks.clear();
  }

  protected handleParsedMessage(data: unknown): void {
    if (!data || typeof data !== 'object') return;

    const event = data as ClobUserEvent;
    const eventType = event.event_type;

    // Emit to specific event listeners
    this.emitEventCallback(eventType, event);

    // Emit to general message listeners
    this.emitEventCallback('message', event);
  }

  private sendFullSubscription(): void {
    if (this.userSubscribeMode === 'legacy') {
      const message: Record<string, unknown> = {
        type: 'subscribe',
        operation: 'subscribe',
        initial_dump: true,
        auth: {
          apiKey: this.auth.apiKey,
          secret: this.auth.secret,
          passphrase: this.auth.passphrase,
        },
      };
      if (this.marketIds.size > 0) {
        message.markets = Array.from(this.marketIds);
      }
      this.send(message);
      return;
    }

    const message: ClobSubscribeMessage = {
      auth: this.auth,
      type: 'USER',
      markets: Array.from(this.marketIds),
    };
    this.send(message);
  }

  private addEventCallback<T>(event: string, callback: EventCallback<T>): () => void {
    if (!this.eventCallbacks.has(event)) {
      this.eventCallbacks.set(event, new Set());
    }
    this.eventCallbacks.get(event)!.add(callback as EventCallback<unknown>);

    return () => {
      this.eventCallbacks.get(event)?.delete(callback as EventCallback<unknown>);
    };
  }

  private emitEventCallback(event: string, data: unknown): void {
    const callbacks = this.eventCallbacks.get(event);
    if (callbacks) {
      for (const callback of callbacks) {
        try {
          callback(data);
        } catch (error) {
          this.options.logger.error(`Error in CLOB user event callback for "${event}":`, error);
        }
      }
    }
  }
}

/**
 * Unified CLOB Client
 * Provides access to both market and user channels
 */
export class ClobClient {
  private marketClient: ClobMarketClient | null = null;
  private userClient: ClobUserClient | null = null;
  private options: ClobClientOptions;
  private auth?: ClobAuth;

  constructor(options: ClobClientOptions = {}) {
    this.options = options;
    this.auth = options.auth;
  }

  /**
   * Get the market channel client (creates if not exists)
   */
  get market(): ClobMarketClient {
    if (!this.marketClient) {
      this.marketClient = new ClobMarketClient(this.options);
    }
    return this.marketClient;
  }

  /**
   * Get the user channel client (creates if not exists)
   * Requires auth credentials
   */
  get user(): ClobUserClient {
    if (!this.userClient) {
      if (!this.auth) {
        throw new Error('ClobClient requires auth credentials for user channel');
      }
      this.userClient = new ClobUserClient(this.auth, this.options);
    }
    return this.userClient;
  }

  /**
   * Connect to market channel
   */
  async connectMarket(): Promise<void> {
    await this.market.connect();
  }

  /**
   * Connect to user channel
   */
  async connectUser(): Promise<void> {
    await this.user.connect();
  }

  /**
   * Connect to both channels
   */
  async connectAll(): Promise<void> {
    await Promise.all([this.connectMarket(), this.connectUser()]);
  }

  /**
   * Disconnect from all channels
   */
  disconnect(): void {
    this.marketClient?.disconnect();
    this.userClient?.disconnect();
  }
}
