/**
 * Tests for RtdsClient
 */

import { describe, it, beforeEach, afterEach, mock } from 'node:test';
import assert from 'node:assert';
import { MockWebSocket, installMockWebSocket } from './mocks/websocket.js';
import { RtdsClient } from '../src/rtds-client.js';
import type { RtdsActionMessage, RtdsMessage, CryptoPricePayload, CommentPayload } from '../src/types.js';

describe('RtdsClient', () => {
  let originalWebSocket: typeof WebSocket;
  let client: RtdsClient;

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
    it('should use default URL', async () => {
      client = new RtdsClient();

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;

      assert.ok(ws.url.includes('ws-live-data.polymarket.com'));

      ws.simulateOpen();
      await connectPromise;
    });

    it('should accept custom URL', async () => {
      client = new RtdsClient({ url: 'wss://custom.url/ws' });

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;

      assert.strictEqual(ws.url, 'wss://custom.url/ws');

      ws.simulateOpen();
      await connectPromise;
    });

    it('should use PING message for heartbeat', async () => {
      client = new RtdsClient({ heartbeatInterval: 50 });

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      // Wait for heartbeat
      await new Promise((resolve) => setTimeout(resolve, 100));

      // Check that PING was sent
      const pings = ws.getSentMessages().filter((m) => {
        try {
          const parsed = JSON.parse(m);
          return parsed.type === 'PING';
        } catch {
          return false;
        }
      });

      assert.ok(pings.length > 0);
    });
  });

  describe('subscribeCryptoPrices()', () => {
    it('should subscribe to all crypto prices without filter', async () => {
      client = new RtdsClient();

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      client.subscribeCryptoPrices();

      const sent = ws.getLastSentMessageParsed<RtdsActionMessage>();
      assert.strictEqual(sent?.action, 'subscribe');
      assert.strictEqual(sent?.subscriptions[0].topic, 'crypto_prices');
      assert.strictEqual(sent?.subscriptions[0].type, 'update');
      assert.strictEqual(sent?.subscriptions[0].filters, undefined);
    });

    it('should subscribe with symbol filter', async () => {
      client = new RtdsClient();

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      client.subscribeCryptoPrices(['btcusdt', 'ethusdt']);

      const sent = ws.getLastSentMessageParsed<RtdsActionMessage>();
      assert.strictEqual(
        sent?.subscriptions[0].filters,
        JSON.stringify([{ symbol: 'btcusdt' }, { symbol: 'ethusdt' }]),
      );
    });

    it('should track subscription', () => {
      client = new RtdsClient();

      client.subscribeCryptoPrices(['btcusdt']);

      const subs = client.getSubscriptions();
      assert.strictEqual(subs.length, 1);
      assert.strictEqual(subs[0].topic, 'crypto_prices');
    });

    it('should throw TypeError for non-array symbols', () => {
      client = new RtdsClient();

      assert.throws(
        () => client.subscribeCryptoPrices('btcusdt' as unknown as string[]),
        { name: 'TypeError', message: 'symbols must be an array' }
      );
    });

    it('should throw TypeError for empty string in symbols array', () => {
      client = new RtdsClient();

      assert.throws(
        () => client.subscribeCryptoPrices(['btcusdt', '']),
        { name: 'TypeError', message: 'Each symbol must be a non-empty string' }
      );
    });
  });

  describe('unsubscribeCryptoPrices()', () => {
    it('should unsubscribe from crypto prices', async () => {
      client = new RtdsClient();

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      client.subscribeCryptoPrices();
      client.unsubscribeCryptoPrices();

      const sent = ws.getLastSentMessageParsed<RtdsActionMessage>();
      assert.strictEqual(sent?.action, 'unsubscribe');
      assert.strictEqual(sent?.subscriptions[0].topic, 'crypto_prices');
    });

    it('should remove subscription tracking', () => {
      client = new RtdsClient();

      client.subscribeCryptoPrices();
      client.unsubscribeCryptoPrices();

      assert.strictEqual(client.getSubscriptions().length, 0);
    });
  });

  describe('subscribeCryptoPricesChainlink()', () => {
    it('should subscribe to chainlink prices', async () => {
      client = new RtdsClient();

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      client.subscribeCryptoPricesChainlink();

      const sent = ws.getLastSentMessageParsed<RtdsActionMessage>();
      assert.strictEqual(sent?.subscriptions[0].topic, 'crypto_prices_chainlink');
      assert.strictEqual(sent?.subscriptions[0].type, '*');
    });

    it('should subscribe with symbol filter', async () => {
      client = new RtdsClient();

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      client.subscribeCryptoPricesChainlink('eth/usd');

      const sent = ws.getLastSentMessageParsed<RtdsActionMessage>();
      assert.ok(sent?.subscriptions[0].filters?.includes('eth/usd'));
    });
  });

  describe('unsubscribeCryptoPricesChainlink()', () => {
    it('should unsubscribe from chainlink prices', async () => {
      client = new RtdsClient();

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      client.subscribeCryptoPricesChainlink();
      client.unsubscribeCryptoPricesChainlink();

      const sent = ws.getLastSentMessageParsed<RtdsActionMessage>();
      assert.strictEqual(sent?.action, 'unsubscribe');
      assert.strictEqual(sent?.subscriptions[0].topic, 'crypto_prices_chainlink');
    });
  });

  describe('subscribeComments()', () => {
    it('should subscribe to comments with default type', async () => {
      client = new RtdsClient();

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      client.subscribeComments();

      const sent = ws.getLastSentMessageParsed<RtdsActionMessage>();
      assert.strictEqual(sent?.subscriptions[0].topic, 'comments');
      assert.strictEqual(sent?.subscriptions[0].type, 'comment_created');
    });

    it('should subscribe with specific event type', async () => {
      client = new RtdsClient();

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      client.subscribeComments('reaction_created');

      const sent = ws.getLastSentMessageParsed<RtdsActionMessage>();
      assert.strictEqual(sent?.subscriptions[0].type, 'reaction_created');
    });

    it('should include gamma auth if provided', async () => {
      client = new RtdsClient();
      const gammaAuth = { address: '0x123' };

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      client.subscribeComments('comment_created', gammaAuth);

      const sent = ws.getLastSentMessageParsed<RtdsActionMessage>();
      assert.deepStrictEqual(sent?.subscriptions[0].gamma_auth, gammaAuth);
    });
  });

  describe('unsubscribeComments()', () => {
    it('should unsubscribe from comments', async () => {
      client = new RtdsClient();

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      client.subscribeComments();
      client.unsubscribeComments();

      const sent = ws.getLastSentMessageParsed<RtdsActionMessage>();
      assert.strictEqual(sent?.action, 'unsubscribe');
      assert.strictEqual(sent?.subscriptions[0].topic, 'comments');
    });
  });

  describe('subscribeActivity()', () => {
    it('should subscribe to activity', async () => {
      client = new RtdsClient();

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      client.subscribeActivity('order_placed');

      const sent = ws.getLastSentMessageParsed<RtdsActionMessage>();
      assert.strictEqual(sent?.subscriptions[0].topic, 'activity');
      assert.strictEqual(sent?.subscriptions[0].type, 'order_placed');
    });

    it('should include auth if provided', async () => {
      client = new RtdsClient();
      const clobAuth = { key: 'k', secret: 's', passphrase: 'p' };
      const gammaAuth = { address: '0x123' };

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      client.subscribeActivity('order_placed', clobAuth, gammaAuth);

      const sent = ws.getLastSentMessageParsed<RtdsActionMessage>();
      assert.deepStrictEqual(sent?.subscriptions[0].clob_auth, clobAuth);
      assert.deepStrictEqual(sent?.subscriptions[0].gamma_auth, gammaAuth);
    });

    it('should throw TypeError for empty type string', () => {
      client = new RtdsClient();

      assert.throws(
        () => client.subscribeActivity(''),
        { name: 'TypeError', message: 'type must be a non-empty string' }
      );
    });

    it('should throw TypeError for whitespace-only type', () => {
      client = new RtdsClient();

      assert.throws(
        () => client.subscribeActivity('   '),
        { name: 'TypeError', message: 'type must be a non-empty string' }
      );
    });
  });

  describe('unsubscribeActivity()', () => {
    it('should unsubscribe from activity', async () => {
      client = new RtdsClient();

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      client.subscribeActivity('order_placed');
      client.unsubscribeActivity('order_placed');

      const sent = ws.getLastSentMessageParsed<RtdsActionMessage>();
      assert.strictEqual(sent?.action, 'unsubscribe');
      assert.strictEqual(sent?.subscriptions[0].topic, 'activity');
    });
  });

  describe('subscribeCustom()', () => {
    it('should subscribe with custom config', async () => {
      client = new RtdsClient();

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      client.subscribeCustom({
        topic: 'crypto_prices',
        type: 'custom_type',
        filters: 'custom_filter',
      });

      const sent = ws.getLastSentMessageParsed<RtdsActionMessage>();
      assert.strictEqual(sent?.subscriptions[0].topic, 'crypto_prices');
      assert.strictEqual(sent?.subscriptions[0].type, 'custom_type');
      assert.strictEqual(sent?.subscriptions[0].filters, 'custom_filter');
    });
  });

  describe('unsubscribeCustom()', () => {
    it('should unsubscribe custom subscription', async () => {
      client = new RtdsClient();

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      client.subscribeCustom({ topic: 'crypto_prices', type: 'custom_type' });
      client.unsubscribeCustom('crypto_prices', 'custom_type');

      const sent = ws.getLastSentMessageParsed<RtdsActionMessage>();
      assert.strictEqual(sent?.action, 'unsubscribe');
    });

    it('should handle unsubscribe of non-existent subscription', async () => {
      client = new RtdsClient();

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      const messageCount = ws.getSentMessages().length;
      client.unsubscribeCustom('crypto_prices', 'nonexistent');

      // No new message should be sent
      assert.strictEqual(ws.getSentMessages().length, messageCount);
    });
  });

  describe('event callbacks', () => {
    it('onCryptoPrice() should receive crypto price events from both sources', async () => {
      client = new RtdsClient();
      const events: RtdsMessage<CryptoPricePayload>[] = [];

      client.onCryptoPrice((msg) => events.push(msg));

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      // Binance price
      ws.simulateMessage({
        topic: 'crypto_prices',
        type: 'update',
        timestamp: 1234567890,
        payload: { symbol: 'btcusdt', timestamp: 1234567890, value: 50000 },
      });

      // Chainlink price
      ws.simulateMessage({
        topic: 'crypto_prices_chainlink',
        type: 'update',
        timestamp: 1234567891,
        payload: { symbol: 'eth/usd', timestamp: 1234567891, value: 3000 },
      });

      assert.strictEqual(events.length, 2);
      assert.strictEqual(events[0].topic, 'crypto_prices');
      assert.strictEqual(events[1].topic, 'crypto_prices_chainlink');
    });

    it('onComment() should receive comment events', async () => {
      client = new RtdsClient();
      const events: RtdsMessage<CommentPayload>[] = [];

      client.onComment((msg) => events.push(msg));

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      ws.simulateMessage({
        topic: 'comments',
        type: 'comment_created',
        timestamp: 1234567890,
        payload: {
          id: 'comment1',
          body: 'Test comment',
          createdAt: '2024-01-01T00:00:00Z',
          parentCommentID: null,
          parentEntityID: 123,
          parentEntityType: 'Event',
          profile: {
            baseAddress: '0x123',
            displayUsernamePublic: true,
            name: 'Test User',
            proxyWallet: '0x456',
            pseudonym: 'test',
          },
          reactionCount: 0,
          replyAddress: '0x789',
          reportCount: 0,
          userAddress: '0x123',
        },
      });

      assert.strictEqual(events.length, 1);
      assert.strictEqual(events[0].topic, 'comments');
    });

    it('onActivity() should receive activity events', async () => {
      client = new RtdsClient();
      const events: RtdsMessage<unknown>[] = [];

      client.onActivity((msg) => events.push(msg));

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      ws.simulateMessage({
        topic: 'activity',
        type: 'order_placed',
        timestamp: 1234567890,
        payload: { orderId: 'order1' },
      });

      assert.strictEqual(events.length, 1);
      assert.strictEqual(events[0].topic, 'activity');
    });

    it('onRtdsMessage() should receive all messages', async () => {
      client = new RtdsClient();
      const events: RtdsMessage<unknown>[] = [];

      client.onRtdsMessage((msg) => events.push(msg));

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      ws.simulateMessage({
        topic: 'crypto_prices',
        type: 'update',
        timestamp: 1234567890,
        payload: {},
      });

      ws.simulateMessage({
        topic: 'comments',
        type: 'comment_created',
        timestamp: 1234567891,
        payload: {},
      });

      assert.strictEqual(events.length, 2);
    });

    it('should allow unsubscribing from events', async () => {
      client = new RtdsClient();
      const events: RtdsMessage<unknown>[] = [];

      const unsubscribe = client.onCryptoPrice((msg) => events.push(msg));

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      ws.simulateMessage({
        topic: 'crypto_prices',
        type: 'update',
        timestamp: 1,
        payload: {},
      });

      unsubscribe();

      ws.simulateMessage({
        topic: 'crypto_prices',
        type: 'update',
        timestamp: 2,
        payload: {},
      });

      assert.strictEqual(events.length, 1);
    });

    it('should handle callback errors gracefully', async () => {
      const consoleSpy = mock.method(console, 'error', () => {});
      client = new RtdsClient();

      client.onCryptoPrice(() => {
        throw new Error('Test error');
      });

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      ws.simulateMessage({
        topic: 'crypto_prices',
        type: 'update',
        timestamp: 1,
        payload: {},
      });

      assert.ok(consoleSpy.mock.calls.length > 0);
      consoleSpy.mock.restore();
    });

    it('should ignore invalid messages', async () => {
      client = new RtdsClient();
      const events: unknown[] = [];

      client.onRtdsMessage((msg) => events.push(msg));

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      ws.simulateMessage(null as unknown as object);
      ws.simulateMessage('not an object');

      assert.strictEqual(events.length, 0);
    });
  });

  describe('reconnection', () => {
    it('should resubscribe on reconnection', async () => {
      client = new RtdsClient({
        autoReconnect: true,
        reconnectDelay: 10,
      });

      client.subscribeCryptoPrices(['btcusdt']);
      client.subscribeComments();

      const connectPromise = client.connect();
      const ws1 = MockWebSocket.getLastInstance()!;
      ws1.simulateOpen();
      await connectPromise;

      // Simulate disconnect
      ws1.simulateClose(1006, 'Connection lost');

      await new Promise((resolve) => setTimeout(resolve, 50));

      const ws2 = MockWebSocket.getLastInstance()!;
      ws2.simulateOpen();

      await new Promise((resolve) => setTimeout(resolve, 10));

      // Check resubscription message
      const messages = ws2.getSentMessages();
      const subMessages = messages
        .map((m) => {
          try {
            return JSON.parse(m);
          } catch {
            return null;
          }
        })
        .filter((m) => m?.action === 'subscribe');

      assert.ok(subMessages.length > 0);

      // Should have subscriptions for both crypto_prices and comments
      const allSubs = subMessages.flatMap((m: RtdsActionMessage) => m.subscriptions);
      assert.ok(allSubs.some((s) => s.topic === 'crypto_prices'));
      assert.ok(allSubs.some((s) => s.topic === 'comments'));
    });
  });

  describe('getSubscriptions()', () => {
    it('should return all active subscriptions', () => {
      client = new RtdsClient();

      client.subscribeCryptoPrices();
      client.subscribeComments();
      client.subscribeActivity('order_placed');

      const subs = client.getSubscriptions();
      assert.strictEqual(subs.length, 3);
    });
  });
});
