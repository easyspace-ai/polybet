/**
 * Base WebSocket Client
 * Provides connection management, automatic reconnection, heartbeat, and error handling
 */

import { TypedEventEmitter } from './event-emitter.js';
import type { BaseClientOptions, ConnectionState, ClientEventMap, Logger } from './types.js';
import {
  createPlatformWebSocket,
  createPlatformWebSocketSync,
  isBrowserRuntime,
  type NodeWebSocketOptions,
} from './websocket-env.js';

/** Default logger using console */
const defaultLogger: Logger = {
  error: (message: string, ...args: unknown[]) => console.error(message, ...args),
};

/** Default configuration values */
const DEFAULTS = {
  autoReconnect: true,
  maxReconnectAttempts: Infinity,
  reconnectDelay: 1000,
  maxReconnectDelay: 30000,
  heartbeatInterval: 10000,
  connectionTimeout: 10000,
} as const;

/** Extended event map including raw message */
interface BaseEventMap extends ClientEventMap {
  rawMessage: string;
}

/** Internal options type */
interface InternalOptions {
  url: string;
  autoReconnect: boolean;
  maxReconnectAttempts: number;
  reconnectDelay: number;
  maxReconnectDelay: number;
  heartbeatInterval: number;
  connectionTimeout: number;
  logger: Logger;
  proxyUrl?: string;
  proxyHeaders?: Record<string, string>;
}

/**
 * Abstract base class for WebSocket clients
 * Handles connection lifecycle, reconnection logic, and heartbeat mechanism
 */
export abstract class BaseWebSocketClient extends TypedEventEmitter<BaseEventMap> {
  protected ws: WebSocket | null = null;
  protected readonly options: InternalOptions;
  protected state: ConnectionState = 'disconnected';
  protected reconnectAttempts = 0;
  protected reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  protected heartbeatTimer: ReturnType<typeof setInterval> | null = null;
  protected connectionTimer: ReturnType<typeof setTimeout> | null = null;
  protected isIntentionalClose = false;
  protected pendingSubscriptions: (() => void)[] = [];

  constructor(options: BaseClientOptions) {
    super();
    this.options = {
      url: options.url,
      autoReconnect: options.autoReconnect ?? DEFAULTS.autoReconnect,
      maxReconnectAttempts: options.maxReconnectAttempts ?? DEFAULTS.maxReconnectAttempts,
      reconnectDelay: options.reconnectDelay ?? DEFAULTS.reconnectDelay,
      maxReconnectDelay: options.maxReconnectDelay ?? DEFAULTS.maxReconnectDelay,
      heartbeatInterval: options.heartbeatInterval ?? DEFAULTS.heartbeatInterval,
      connectionTimeout: options.connectionTimeout ?? DEFAULTS.connectionTimeout,
      logger: options.logger ?? defaultLogger,
      proxyUrl: options.proxyUrl,
      proxyHeaders: options.proxyHeaders,
    };
  }

  /**
   * Current connection state
   */
  get connectionState(): ConnectionState {
    return this.state;
  }

  /**
   * Whether the client is currently connected
   */
  get isConnected(): boolean {
    return this.state === 'connected';
  }

  /**
   * Connect to the WebSocket server
   */
  async connect(): Promise<void> {
    if (this.state === 'connected' || this.state === 'connecting') {
      return;
    }

    this.isIntentionalClose = false;
    await this.createConnection();
  }

  /**
   * Disconnect from the WebSocket server
   */
  disconnect(): void {
    this.isIntentionalClose = true;
    this.cleanup();
    this.setState('disconnected');
  }

  /**
   * Send a message through the WebSocket
   */
  protected send(data: unknown): void {
    if (!this.ws || this.state !== 'connected') {
      throw new Error('WebSocket is not connected');
    }

    const message = typeof data === 'string' ? data : JSON.stringify(data);
    this.ws.send(message);
  }

  /**
   * Create the WebSocket connection
   */
  private async createConnection(): Promise<void> {
    this.setState('connecting');

    const wsOptions: NodeWebSocketOptions = {
      handshakeTimeout: this.options.connectionTimeout,
    };

    if (this.options.proxyUrl) {
      if (isBrowserRuntime()) {
        this.options.logger.error(
          '[polymarket-websocket-client] proxyUrl is ignored in browser; configure system/Electron proxy instead.',
        );
      } else {
        const { createProxyAgent } = await import('./proxy-agent.js');
        wsOptions.agent = await createProxyAgent(this.options.proxyUrl, this.options.proxyHeaders);
      }
    }

    return new Promise((resolve, reject) => {
      const attach = (ws: WebSocket) => {
        try {
          this.ws = ws;
          this.ws.onopen = () => {
            this.clearConnectionTimeout();
            this.reconnectAttempts = 0;
            this.setState('connected');
            this.startHeartbeat();
            this.flushPendingSubscriptions();
            this.onConnected();
            this.emit('connected', undefined);
            resolve();
          };

          this.ws.onclose = (event) => {
            this.clearConnectionTimeout();
            this.stopHeartbeat();
            const code = event.code;
            const reason = event.reason;
            this.emit('disconnected', { code, reason });
            this.onDisconnected(code, reason);

            if (!this.isIntentionalClose && this.options.autoReconnect) {
              this.scheduleReconnect();
            } else {
              this.setState('disconnected');
            }
          };

          this.ws.onerror = () => {
            const error = new Error('WebSocket error');
            this.handleError(error);
          };

          this.ws.onmessage = (event) => {
            const data = typeof event.data === 'string' ? event.data : String(event.data);
            this.handleIncomingMessage(data);
          };

          this.connectionTimer = setTimeout(() => {
            const error = new Error(
              `Connection timeout after ${this.options.connectionTimeout}ms`,
            );
            this.handleError(error);
            this.ws?.close();
            reject(error);
          }, this.options.connectionTimeout);
        } catch (error) {
          this.clearConnectionTimeout();
          this.handleError(error instanceof Error ? error : new Error(String(error)));
          reject(error);
        }
      };

      if (isBrowserRuntime()) {
        attach(createPlatformWebSocketSync(this.options.url, wsOptions));
        return;
      }

      void createPlatformWebSocket(this.options.url, wsOptions)
        .then(attach)
        .catch((error: unknown) => {
          this.clearConnectionTimeout();
          this.handleError(error instanceof Error ? error : new Error(String(error)));
          reject(error);
        });
    });
  }

  /**
   * Handle incoming messages
   */
  private handleIncomingMessage(data: string): void {
    this.emit('rawMessage', data);

    const trimmed = data.trim();
    if (trimmed.length > 0 && trimmed[0] !== '{' && trimmed[0] !== '[') {
      const text = trimmed.toUpperCase();
      if (text === 'PONG') {
        return;
      }
      if (text === 'PING') {
        try {
          this.send('PONG');
        } catch {
          // ignore
        }
        return;
      }
    }

    try {
      const parsed = JSON.parse(data);
      this.handleParsedMessage(parsed);
    } catch {
      this.handleParsedMessage(data);
    }
  }

  /**
   * Handle errors
   */
  private handleError(error: Error): void {
    this.emit('error', error);
    this.onError(error);
  }

  /**
   * Update connection state
   */
  private setState(newState: ConnectionState): void {
    if (this.state !== newState) {
      const previousState = this.state;
      this.state = newState;
      this.emit('stateChange', { state: newState, previousState });
    }
  }

  /**
   * Schedule a reconnection attempt
   */
  private scheduleReconnect(): void {
    if (this.reconnectAttempts >= this.options.maxReconnectAttempts) {
      this.setState('disconnected');
      this.handleError(new Error('Max reconnection attempts reached'));
      return;
    }

    this.setState('reconnecting');
    this.reconnectAttempts++;

    const delay = Math.min(
      this.options.reconnectDelay * Math.pow(2, this.reconnectAttempts - 1) + Math.random() * 1000,
      this.options.maxReconnectDelay
    );

    this.emit('reconnecting', {
      attempt: this.reconnectAttempts,
      maxAttempts: this.options.maxReconnectAttempts,
    });

    this.reconnectTimer = setTimeout(async () => {
      try {
        await this.createConnection();
      } catch {
        // Error already handled in createConnection
      }
    }, delay);
  }

  /**
   * Start the heartbeat mechanism
   */
  private startHeartbeat(): void {
    this.stopHeartbeat();
    this.heartbeatTimer = setInterval(() => {
      if (this.state === 'connected') {
        this.sendHeartbeat();
      }
    }, this.options.heartbeatInterval);
  }

  /**
   * Stop the heartbeat mechanism
   */
  private stopHeartbeat(): void {
    if (this.heartbeatTimer) {
      clearInterval(this.heartbeatTimer);
      this.heartbeatTimer = null;
    }
  }

  /**
   * Clear connection timeout
   */
  private clearConnectionTimeout(): void {
    if (this.connectionTimer) {
      clearTimeout(this.connectionTimer);
      this.connectionTimer = null;
    }
  }

  /**
   * Flush pending subscriptions after connection
   */
  private flushPendingSubscriptions(): void {
    const pending = this.pendingSubscriptions;
    this.pendingSubscriptions = [];
    for (const subscription of pending) {
      try {
        subscription();
      } catch (error) {
        this.handleError(error instanceof Error ? error : new Error(String(error)));
      }
    }
  }

  /**
   * Cleanup resources
   */
  private cleanup(): void {
    this.clearConnectionTimeout();
    this.stopHeartbeat();

    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }

    if (this.ws) {
      this.ws.onopen = null;
      this.ws.onclose = null;
      this.ws.onerror = null;
      this.ws.onmessage = null;
      const open = 1;
      const connecting = 0;
      if (this.ws.readyState === open || this.ws.readyState === connecting) {
        this.ws.close(1000, 'Client disconnect');
      }
      this.ws = null;
    }

    this.pendingSubscriptions = [];
    this.reconnectAttempts = 0;
    this.onCleanup();
  }

  /**
   * Called during cleanup to allow subclasses to release resources
   */
  protected onCleanup(): void {}

  /**
   * Send heartbeat/ping message
   */
  protected sendHeartbeat(): void {
    try {
      this.send('PING');
    } catch {
      // Ignore send errors during heartbeat
    }
  }

  /**
   * Called when connection is established
   */
  protected onConnected(): void {}

  /**
   * Called when connection is closed
   */
  protected onDisconnected(_code: number, _reason: string): void {}

  /**
   * Called when a message is received
   */
  protected abstract handleParsedMessage(data: unknown): void;

  /**
   * Called when an error occurs
   */
  protected onError(_error: Error): void {}
}
