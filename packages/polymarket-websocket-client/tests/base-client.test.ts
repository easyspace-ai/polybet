/**
 * Tests for BaseWebSocketClient
 */

import { describe, it, beforeEach, afterEach, mock } from 'node:test';
import assert from 'node:assert';
import { MockWebSocket, installMockWebSocket } from './mocks/websocket.js';
import { BaseWebSocketClient } from '../src/base-client.js';

// Concrete implementation for testing
class TestWebSocketClient extends BaseWebSocketClient {
  public receivedMessages: unknown[] = [];
  public heartbeatsSent = 0;

  constructor(url: string, options: Record<string, unknown> = {}) {
    super({ url, ...options });
  }

  protected handleParsedMessage(data: unknown): void {
    this.receivedMessages.push(data);
  }

  protected sendHeartbeat(): void {
    this.heartbeatsSent++;
    super.sendHeartbeat();
  }

  // Expose protected methods for testing
  public testSend(data: unknown): void {
    this.send(data);
  }

  public getWs(): WebSocket | null {
    return this.ws;
  }
}

describe('BaseWebSocketClient', () => {
  let originalWebSocket: typeof WebSocket;
  let client: TestWebSocketClient;

  beforeEach(() => {
    originalWebSocket = globalThis.WebSocket;
    installMockWebSocket();
    MockWebSocket.clearInstances();
  });

  afterEach(() => {
    client?.disconnect();
    globalThis.WebSocket = originalWebSocket;
  });

  describe('constructor', () => {
    it('should set default options', () => {
      client = new TestWebSocketClient('wss://test.com');
      assert.strictEqual(client.connectionState, 'disconnected');
      assert.strictEqual(client.isConnected, false);
    });

    it('should accept custom options', () => {
      client = new TestWebSocketClient('wss://test.com', {
        autoReconnect: false,
        maxReconnectAttempts: 5,
        reconnectDelay: 2000,
        maxReconnectDelay: 60000,
        heartbeatInterval: 10000,
        connectionTimeout: 5000,
      });
      assert.strictEqual(client.connectionState, 'disconnected');
    });
  });

  describe('connect()', () => {
    it('should connect to WebSocket server', async () => {
      client = new TestWebSocketClient('wss://test.com');

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance();
      assert.ok(ws);
      assert.strictEqual(ws.url, 'wss://test.com');

      ws.simulateOpen();
      await connectPromise;

      assert.strictEqual(client.connectionState, 'connected');
      assert.strictEqual(client.isConnected, true);
    });

    it('should emit connected event', async () => {
      client = new TestWebSocketClient('wss://test.com');
      let connected = false;
      client.on('connected', () => {
        connected = true;
      });

      const connectPromise = client.connect();
      MockWebSocket.getLastInstance()?.simulateOpen();
      await connectPromise;

      assert.strictEqual(connected, true);
    });

    it('should not reconnect if already connected', async () => {
      client = new TestWebSocketClient('wss://test.com');

      const connectPromise = client.connect();
      MockWebSocket.getLastInstance()?.simulateOpen();
      await connectPromise;

      // Try to connect again
      await client.connect();

      assert.strictEqual(MockWebSocket.getAllInstances().length, 1);
    });

    it('should not reconnect if connecting', async () => {
      client = new TestWebSocketClient('wss://test.com');

      const connectPromise1 = client.connect();
      const connectPromise2 = client.connect();

      MockWebSocket.getLastInstance()?.simulateOpen();
      await Promise.all([connectPromise1, connectPromise2]);

      assert.strictEqual(MockWebSocket.getAllInstances().length, 1);
    });

    it('should handle connection timeout', async () => {
      client = new TestWebSocketClient('wss://test.com', {
        connectionTimeout: 100,
        autoReconnect: false,
      });

      let errorEmitted = false;
      client.on('error', () => {
        errorEmitted = true;
      });

      try {
        await client.connect();
        assert.fail('Should have thrown');
      } catch (error) {
        assert.ok(error instanceof Error);
        assert.ok((error as Error).message.includes('timeout'));
      }

      assert.strictEqual(errorEmitted, true);
    });
  });

  describe('disconnect()', () => {
    it('should disconnect and cleanup', async () => {
      client = new TestWebSocketClient('wss://test.com');

      const connectPromise = client.connect();
      MockWebSocket.getLastInstance()?.simulateOpen();
      await connectPromise;

      client.disconnect();

      assert.strictEqual(client.connectionState, 'disconnected');
      assert.strictEqual(client.isConnected, false);
    });

    it('should emit disconnected event on server close', async () => {
      client = new TestWebSocketClient('wss://test.com', {
        autoReconnect: false,
      });

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      let disconnectInfo: { code: number; reason: string } | null = null;
      client.on('disconnected', (info) => {
        disconnectInfo = info;
      });

      // Simulate server-side close
      ws.simulateClose(1001, 'Server going away');

      await new Promise((resolve) => setTimeout(resolve, 10));

      assert.ok(disconnectInfo);
      assert.strictEqual(disconnectInfo!.code, 1001);
      assert.strictEqual(disconnectInfo!.reason, 'Server going away');
    });

    it('should not auto-reconnect after intentional disconnect', async () => {
      client = new TestWebSocketClient('wss://test.com', {
        autoReconnect: true,
        reconnectDelay: 10,
      });

      const connectPromise = client.connect();
      MockWebSocket.getLastInstance()?.simulateOpen();
      await connectPromise;

      client.disconnect();

      await new Promise((resolve) => setTimeout(resolve, 50));
      assert.strictEqual(MockWebSocket.getAllInstances().length, 1);
    });
  });

  describe('send()', () => {
    it('should send string messages', async () => {
      client = new TestWebSocketClient('wss://test.com');

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      client.testSend('hello');

      assert.deepStrictEqual(ws.getSentMessages(), ['hello']);
    });

    it('should stringify object messages', async () => {
      client = new TestWebSocketClient('wss://test.com');

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      client.testSend({ type: 'test', data: 123 });

      const sent = ws.getLastSentMessageParsed();
      assert.deepStrictEqual(sent, { type: 'test', data: 123 });
    });

    it('should throw if not connected', () => {
      client = new TestWebSocketClient('wss://test.com');

      assert.throws(() => {
        client.testSend('hello');
      }, /not connected/i);
    });
  });

  describe('message handling', () => {
    it('should parse JSON messages', async () => {
      client = new TestWebSocketClient('wss://test.com');

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      ws.simulateMessage({ type: 'test', value: 42 });

      assert.deepStrictEqual(client.receivedMessages, [{ type: 'test', value: 42 }]);
    });

    it('should handle non-JSON messages', async () => {
      client = new TestWebSocketClient('wss://test.com');

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      ws.simulateMessage('plain text');

      assert.deepStrictEqual(client.receivedMessages, ['plain text']);
    });

    it('should emit rawMessage event', async () => {
      client = new TestWebSocketClient('wss://test.com');
      const rawMessages: string[] = [];
      client.on('rawMessage', (msg) => rawMessages.push(msg));

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      ws.simulateMessage('test');

      assert.deepStrictEqual(rawMessages, ['test']);
    });
  });

  describe('auto-reconnection', () => {
    it('should reconnect after unexpected disconnect', async () => {
      client = new TestWebSocketClient('wss://test.com', {
        autoReconnect: true,
        reconnectDelay: 10,
        maxReconnectDelay: 100,
      });

      const connectPromise = client.connect();
      const ws1 = MockWebSocket.getLastInstance()!;
      ws1.simulateOpen();
      await connectPromise;

      const instancesBeforeClose = MockWebSocket.getAllInstances().length;

      // Simulate unexpected close
      ws1.simulateClose(1006, 'Connection lost');

      // Wait for reconnect attempt
      await new Promise((resolve) => setTimeout(resolve, 100));

      // Should have created a new WebSocket instance
      assert.ok(MockWebSocket.getAllInstances().length > instancesBeforeClose);

      const ws2 = MockWebSocket.getLastInstance()!;
      ws2.simulateOpen();
      await new Promise((resolve) => setTimeout(resolve, 10));

      assert.strictEqual(client.isConnected, true);
    });

    it('should emit reconnecting event', async () => {
      client = new TestWebSocketClient('wss://test.com', {
        autoReconnect: true,
        reconnectDelay: 10,
      });

      let reconnectInfo: { attempt: number; maxAttempts: number } | null = null;
      client.on('reconnecting', (info) => {
        reconnectInfo = info;
      });

      const connectPromise = client.connect();
      MockWebSocket.getLastInstance()?.simulateOpen();
      await connectPromise;

      MockWebSocket.getLastInstance()?.simulateClose(1006, '');

      await new Promise((resolve) => setTimeout(resolve, 50));

      assert.ok(reconnectInfo);
      assert.strictEqual(reconnectInfo!.attempt, 1);
    });

    it('should stop after max reconnect attempts', async () => {
      client = new TestWebSocketClient('wss://test.com', {
        autoReconnect: true,
        maxReconnectAttempts: 2,
        reconnectDelay: 10,
      });

      let errorEmitted = false;
      client.on('error', (err) => {
        if (err.message.includes('Max reconnection')) {
          errorEmitted = true;
        }
      });

      const connectPromise = client.connect();
      MockWebSocket.getLastInstance()?.simulateOpen();
      await connectPromise;

      // First disconnect
      MockWebSocket.getLastInstance()?.simulateClose(1006, '');
      await new Promise((resolve) => setTimeout(resolve, 30));

      // Fail first reconnect
      MockWebSocket.getLastInstance()?.simulateClose(1006, '');
      await new Promise((resolve) => setTimeout(resolve, 50));

      // Fail second reconnect
      MockWebSocket.getLastInstance()?.simulateClose(1006, '');
      await new Promise((resolve) => setTimeout(resolve, 100));

      assert.strictEqual(errorEmitted, true);
      assert.strictEqual(client.connectionState, 'disconnected');
    });

    it('should not reconnect when autoReconnect is false', async () => {
      client = new TestWebSocketClient('wss://test.com', {
        autoReconnect: false,
      });

      const connectPromise = client.connect();
      MockWebSocket.getLastInstance()?.simulateOpen();
      await connectPromise;

      const instancesBefore = MockWebSocket.getAllInstances().length;
      MockWebSocket.getLastInstance()?.simulateClose(1006, '');

      await new Promise((resolve) => setTimeout(resolve, 50));

      assert.strictEqual(MockWebSocket.getAllInstances().length, instancesBefore);
      assert.strictEqual(client.connectionState, 'disconnected');
    });
  });

  describe('heartbeat', () => {
    it('should send heartbeat at interval', async () => {
      client = new TestWebSocketClient('wss://test.com', {
        heartbeatInterval: 50,
      });

      const connectPromise = client.connect();
      MockWebSocket.getLastInstance()?.simulateOpen();
      await connectPromise;

      await new Promise((resolve) => setTimeout(resolve, 130));

      assert.ok(client.heartbeatsSent >= 2);
    });

    it('should stop heartbeat on disconnect', async () => {
      client = new TestWebSocketClient('wss://test.com', {
        heartbeatInterval: 50,
      });

      const connectPromise = client.connect();
      MockWebSocket.getLastInstance()?.simulateOpen();
      await connectPromise;

      await new Promise((resolve) => setTimeout(resolve, 60));
      const heartbeatsBefore = client.heartbeatsSent;

      client.disconnect();
      await new Promise((resolve) => setTimeout(resolve, 100));

      assert.strictEqual(client.heartbeatsSent, heartbeatsBefore);
    });
  });

  describe('error handling', () => {
    it('should emit error event on WebSocket error', async () => {
      client = new TestWebSocketClient('wss://test.com', {
        autoReconnect: false,
      });

      let errorEmitted = false;
      client.on('error', () => {
        errorEmitted = true;
      });

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      ws.simulateError();

      assert.strictEqual(errorEmitted, true);
    });

    it('should handle WebSocket constructor throwing', async () => {
      // Save original and replace with throwing constructor
      const OriginalMockWebSocket = globalThis.WebSocket;

      class ThrowingWebSocket {
        constructor() {
          throw new Error('Connection refused');
        }
      }

      (globalThis as unknown as { WebSocket: typeof ThrowingWebSocket }).WebSocket = ThrowingWebSocket as unknown as typeof WebSocket;

      client = new TestWebSocketClient('wss://test.com', {
        autoReconnect: false,
      });

      let errorReceived: Error | null = null;
      client.on('error', (err) => {
        errorReceived = err;
      });

      try {
        await client.connect();
        assert.fail('Should have thrown');
      } catch (error) {
        assert.ok(error instanceof Error);
      }

      globalThis.WebSocket = OriginalMockWebSocket;
    });
  });

  describe('state changes', () => {
    it('should emit stateChange events', async () => {
      client = new TestWebSocketClient('wss://test.com');

      const stateChanges: Array<{ state: string; previousState: string }> = [];
      client.on('stateChange', (change) => {
        stateChanges.push(change);
      });

      const connectPromise = client.connect();
      MockWebSocket.getLastInstance()?.simulateOpen();
      await connectPromise;

      client.disconnect();
      await new Promise((resolve) => setTimeout(resolve, 10));

      assert.ok(stateChanges.some((s) => s.state === 'connecting'));
      assert.ok(stateChanges.some((s) => s.state === 'connected'));
      assert.ok(stateChanges.some((s) => s.state === 'disconnected'));
    });
  });
});
