/**
 * Tests for CLOB Clients
 */

import { describe, it, beforeEach, afterEach, mock } from 'node:test';
import assert from 'node:assert';
import { MockWebSocket, installMockWebSocket } from './mocks/websocket.js';
import { ClobMarketClient, ClobUserClient, ClobClient } from '../src/clob-client.js';
import type {
  ClobBookEvent,
  ClobPriceChangeEvent,
  ClobTickSizeChangeEvent,
  ClobLastTradePriceEvent,
  ClobTradeEvent,
  ClobOrderEvent,
  ClobSubscribeMessage,
  ClobAssetSubscriptionMessage,
} from '../src/types.js';

describe('ClobMarketClient', () => {
  let originalWebSocket: typeof WebSocket;
  let client: ClobMarketClient;

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
      client = new ClobMarketClient();

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;

      assert.ok(ws.url.includes('ws-subscriptions-clob.polymarket.com'));
      assert.ok(ws.url.includes('/ws/market'));

      ws.simulateOpen();
      await connectPromise;
    });

    it('should accept custom URL', async () => {
      client = new ClobMarketClient({ url: 'wss://custom.url/ws' });

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;

      assert.strictEqual(ws.url, 'wss://custom.url/ws');

      ws.simulateOpen();
      await connectPromise;
    });
  });

  describe('subscribe()', () => {
    it('should track subscribed asset IDs', () => {
      client = new ClobMarketClient();

      client.subscribe(['asset1', 'asset2']);

      assert.deepStrictEqual(client.getSubscribedAssets(), ['asset1', 'asset2']);
    });

    it('should not duplicate subscriptions', () => {
      client = new ClobMarketClient();

      client.subscribe(['asset1']);
      client.subscribe(['asset1', 'asset2']);

      assert.deepStrictEqual(client.getSubscribedAssets(), ['asset1', 'asset2']);
    });

    it('should send subscription when connected', async () => {
      client = new ClobMarketClient();

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      client.subscribe(['asset1', 'asset2']);

      const sent = ws.getLastSentMessageParsed<ClobAssetSubscriptionMessage>();
      assert.deepStrictEqual(sent, {
        assets_ids: ['asset1', 'asset2'],
        type: 'market',
        custom_feature_enabled: true,
        initial_dump: true,
        operation: 'subscribe',
      });
    });

    it('should not send if not connected', () => {
      client = new ClobMarketClient();

      client.subscribe(['asset1']);

      assert.deepStrictEqual(client.getSubscribedAssets(), ['asset1']);
      // No WebSocket created yet
      assert.strictEqual(MockWebSocket.getLastInstance(), undefined);
    });

    it('should skip if no new assets', async () => {
      client = new ClobMarketClient();

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      client.subscribe(['asset1']);
      const messageCount = ws.getSentMessages().length;

      client.subscribe(['asset1']);
      assert.strictEqual(ws.getSentMessages().length, messageCount);
    });

    it('should throw TypeError for non-array input', () => {
      client = new ClobMarketClient();

      assert.throws(
        () => client.subscribe('asset1' as unknown as string[]),
        { name: 'TypeError', message: 'assetIds must be an array' }
      );
    });

    it('should throw TypeError for empty string in array', () => {
      client = new ClobMarketClient();

      assert.throws(
        () => client.subscribe(['asset1', '']),
        { name: 'TypeError', message: 'Each asset ID must be a non-empty string' }
      );
    });

    it('should throw TypeError for non-string in array', () => {
      client = new ClobMarketClient();

      assert.throws(
        () => client.subscribe(['asset1', 123 as unknown as string]),
        { name: 'TypeError', message: 'Each asset ID must be a non-empty string' }
      );
    });
  });

  describe('unsubscribe()', () => {
    it('should remove tracked asset IDs', () => {
      client = new ClobMarketClient();

      client.subscribe(['asset1', 'asset2', 'asset3']);
      client.unsubscribe(['asset2']);

      assert.deepStrictEqual(client.getSubscribedAssets(), ['asset1', 'asset3']);
    });

    it('should throw TypeError for non-array input', () => {
      client = new ClobMarketClient();

      assert.throws(
        () => client.unsubscribe('asset1' as unknown as string[]),
        { name: 'TypeError', message: 'assetIds must be an array' }
      );
    });

    it('should throw TypeError for empty string in array', () => {
      client = new ClobMarketClient();

      assert.throws(
        () => client.unsubscribe(['asset1', '  ']),
        { name: 'TypeError', message: 'Each asset ID must be a non-empty string' }
      );
    });

    it('should send unsubscription when connected', async () => {
      client = new ClobMarketClient();

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      client.subscribe(['asset1', 'asset2']);
      client.unsubscribe(['asset1']);

      const sent = ws.getLastSentMessageParsed<ClobAssetSubscriptionMessage>();
      assert.deepStrictEqual(sent, {
        assets_ids: ['asset1'],
        type: 'market',
        custom_feature_enabled: true,
        operation: 'unsubscribe',
      });
    });

    it('should skip if assets not subscribed', async () => {
      client = new ClobMarketClient();

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      const messageCount = ws.getSentMessages().length;
      client.unsubscribe(['nonexistent']);

      assert.strictEqual(ws.getSentMessages().length, messageCount);
    });
  });

  describe('event callbacks', () => {
    it('onBook() should receive book events', async () => {
      client = new ClobMarketClient();
      const events: ClobBookEvent[] = [];

      client.onBook((event) => events.push(event));

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      const bookEvent: ClobBookEvent = {
        event_type: 'book',
        asset_id: 'asset1',
        market: 'market1',
        timestamp: '2024-01-01T00:00:00Z',
        hash: 'hash123',
        bids: [{ price: '0.5', size: '100' }],
        asks: [{ price: '0.6', size: '200' }],
      };

      ws.simulateMessage(bookEvent);

      assert.deepStrictEqual(events, [bookEvent]);
    });

    it('onPriceChange() should receive price change events', async () => {
      client = new ClobMarketClient();
      const events: ClobPriceChangeEvent[] = [];

      client.onPriceChange((event) => events.push(event));

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      const priceEvent: ClobPriceChangeEvent = {
        event_type: 'price_change',
        market: 'market1',
        timestamp: '2024-01-01T00:00:00Z',
        price_changes: [
          {
            asset_id: 'asset1',
            price: '0.55',
            size: '50',
            side: 'BUY',
            hash: 'hash123',
            best_bid: '0.54',
            best_ask: '0.56',
          },
        ],
      };

      ws.simulateMessage(priceEvent);

      assert.deepStrictEqual(events, [priceEvent]);
    });

    it('onTickSizeChange() should receive tick size events', async () => {
      client = new ClobMarketClient();
      const events: ClobTickSizeChangeEvent[] = [];

      client.onTickSizeChange((event) => events.push(event));

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      const tickEvent: ClobTickSizeChangeEvent = {
        event_type: 'tick_size_change',
        asset_id: 'asset1',
        market: 'market1',
        old_tick_size: '0.01',
        new_tick_size: '0.001',
        timestamp: '2024-01-01T00:00:00Z',
      };

      ws.simulateMessage(tickEvent);

      assert.deepStrictEqual(events, [tickEvent]);
    });

    it('onLastTradePrice() should receive last trade price events', async () => {
      client = new ClobMarketClient();
      const events: ClobLastTradePriceEvent[] = [];

      client.onLastTradePrice((event) => events.push(event));

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      const tradeEvent: ClobLastTradePriceEvent = {
        event_type: 'last_trade_price',
        asset_id: 'asset1',
        market: 'market1',
        price: '0.55',
        side: 'BUY',
        size: '100',
        fee_rate_bps: '0',
        timestamp: '2024-01-01T00:00:00Z',
      };

      ws.simulateMessage(tradeEvent);

      assert.deepStrictEqual(events, [tradeEvent]);
    });

    it('onMarketMessage() should receive all events', async () => {
      client = new ClobMarketClient();
      const events: unknown[] = [];

      client.onMarketMessage((event) => events.push(event));

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      const bookEvent: ClobBookEvent = {
        event_type: 'book',
        asset_id: 'asset1',
        market: 'market1',
        timestamp: '2024-01-01T00:00:00Z',
        hash: 'hash123',
        bids: [],
        asks: [],
      };

      ws.simulateMessage(bookEvent);

      assert.strictEqual(events.length, 1);
      assert.deepStrictEqual(events[0], bookEvent);
    });

    it('should allow unsubscribing from events', async () => {
      client = new ClobMarketClient();
      const events: ClobBookEvent[] = [];

      const unsubscribe = client.onBook((event) => events.push(event));

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      ws.simulateMessage({ event_type: 'book', asset_id: 'a1', market: 'm1', timestamp: '', hash: '', bids: [], asks: [] });

      unsubscribe();

      ws.simulateMessage({ event_type: 'book', asset_id: 'a2', market: 'm1', timestamp: '', hash: '', bids: [], asks: [] });

      assert.strictEqual(events.length, 1);
      assert.strictEqual(events[0].asset_id, 'a1');
    });

    it('should handle callback errors gracefully', async () => {
      const consoleSpy = mock.method(console, 'error', () => {});
      client = new ClobMarketClient();

      client.onBook(() => {
        throw new Error('Test error');
      });

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      ws.simulateMessage({ event_type: 'book', asset_id: 'a1', market: 'm1', timestamp: '', hash: '', bids: [], asks: [] });

      assert.ok(consoleSpy.mock.calls.length > 0);
      consoleSpy.mock.restore();
    });

    it('should ignore invalid messages', async () => {
      client = new ClobMarketClient();
      const events: unknown[] = [];

      client.onMarketMessage((event) => events.push(event));

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
      client = new ClobMarketClient({
        autoReconnect: true,
        reconnectDelay: 10,
      });

      client.subscribe(['asset1', 'asset2']);

      const connectPromise = client.connect();
      const ws1 = MockWebSocket.getLastInstance()!;
      ws1.simulateOpen();
      await connectPromise;

      // Check initial subscription
      const initialSent = ws1.getLastSentMessageParsed<ClobAssetSubscriptionMessage>();
      assert.strictEqual(initialSent?.type, 'market');
      assert.deepStrictEqual(initialSent?.assets_ids, ['asset1', 'asset2']);

      // Simulate disconnect
      ws1.simulateClose(1006, 'Connection lost');

      await new Promise((resolve) => setTimeout(resolve, 50));

      const ws2 = MockWebSocket.getLastInstance()!;
      ws2.simulateOpen();

      await new Promise((resolve) => setTimeout(resolve, 10));

      // Check resubscription
      const resubSent = ws2.getLastSentMessageParsed<ClobAssetSubscriptionMessage>();
      assert.strictEqual(resubSent?.type, 'market');
      assert.deepStrictEqual(resubSent?.assets_ids, ['asset1', 'asset2']);
    });
  });
});

describe('ClobUserClient', () => {
  let originalWebSocket: typeof WebSocket;
  let client: ClobUserClient;
  const testAuth = {
    apiKey: 'test-key',
    secret: 'test-secret',
    passphrase: 'test-passphrase',
  };

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
      client = new ClobUserClient(testAuth);

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;

      assert.ok(ws.url.includes('ws-subscriptions-clob.polymarket.com'));
      assert.ok(ws.url.includes('/ws/user'));

      ws.simulateOpen();
      await connectPromise;
    });

    it('should accept custom URL', async () => {
      client = new ClobUserClient(testAuth, { url: 'wss://custom.url/ws' });

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;

      assert.strictEqual(ws.url, 'wss://custom.url/ws');

      ws.simulateOpen();
      await connectPromise;
    });

    it('should throw TypeError if auth is missing', () => {
      assert.throws(
        () => new ClobUserClient(undefined as unknown as typeof testAuth),
        { name: 'TypeError', message: 'auth credentials are required' }
      );
    });

    it('should throw TypeError if auth.apiKey is empty', () => {
      assert.throws(
        () => new ClobUserClient({ ...testAuth, apiKey: '' }),
        { name: 'TypeError', message: 'auth.apiKey must be a non-empty string' }
      );
    });

    it('should throw TypeError if auth.secret is empty', () => {
      assert.throws(
        () => new ClobUserClient({ ...testAuth, secret: '  ' }),
        { name: 'TypeError', message: 'auth.secret must be a non-empty string' }
      );
    });

    it('should throw TypeError if auth.passphrase is missing', () => {
      const invalidAuth = { apiKey: 'key', secret: 'secret' } as typeof testAuth;
      assert.throws(
        () => new ClobUserClient(invalidAuth),
        { name: 'TypeError', message: 'auth.passphrase must be a non-empty string' }
      );
    });
  });

  describe('subscribe()', () => {
    it('should track subscribed market IDs', () => {
      client = new ClobUserClient(testAuth);

      client.subscribe(['market1', 'market2']);

      assert.deepStrictEqual(client.getSubscribedMarkets(), ['market1', 'market2']);
    });

    it('should not duplicate subscriptions', () => {
      client = new ClobUserClient(testAuth);

      client.subscribe(['market1']);
      client.subscribe(['market1', 'market2']);

      assert.deepStrictEqual(client.getSubscribedMarkets(), ['market1', 'market2']);
    });

    it('should throw TypeError for non-array input', () => {
      client = new ClobUserClient(testAuth);

      assert.throws(
        () => client.subscribe('market1' as unknown as string[]),
        { name: 'TypeError', message: 'marketIds must be an array' }
      );
    });

    it('should throw TypeError for empty string in array', () => {
      client = new ClobUserClient(testAuth);

      assert.throws(
        () => client.subscribe(['market1', '']),
        { name: 'TypeError', message: 'Each market ID must be a non-empty string' }
      );
    });

    it('should send full subscription with auth when connected', async () => {
      client = new ClobUserClient(testAuth);

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      client.subscribe(['market1', 'market2']);

      const sent = ws.getLastSentMessageParsed<Record<string, unknown>>();
      assert.deepStrictEqual(sent, {
        type: 'subscribe',
        operation: 'subscribe',
        initial_dump: true,
        auth: {
          apiKey: testAuth.apiKey,
          secret: testAuth.secret,
          passphrase: testAuth.passphrase,
        },
        markets: ['market1', 'market2'],
      });
    });
  });

  describe('unsubscribe()', () => {
    it('should remove tracked market IDs', () => {
      client = new ClobUserClient(testAuth);

      client.subscribe(['market1', 'market2', 'market3']);
      client.unsubscribe(['market2']);

      assert.deepStrictEqual(client.getSubscribedMarkets(), ['market1', 'market3']);
    });

    it('should send updated subscription when connected', async () => {
      client = new ClobUserClient(testAuth);

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      client.subscribe(['market1', 'market2']);
      client.unsubscribe(['market1']);

      const sent = ws.getLastSentMessageParsed<ClobSubscribeMessage>();
      assert.deepStrictEqual(sent?.markets, ['market2']);
    });
  });

  describe('event callbacks', () => {
    it('onTrade() should receive trade events', async () => {
      client = new ClobUserClient(testAuth);
      const events: ClobTradeEvent[] = [];

      client.onTrade((event) => events.push(event));

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      const tradeEvent: ClobTradeEvent = {
        event_type: 'trade',
        type: 'TRADE',
        id: 'trade1',
        asset_id: 'asset1',
        market: 'market1',
        side: 'BUY',
        price: '0.55',
        size: '100',
        outcome: 'Yes',
        status: 'CONFIRMED',
        owner: 'owner1',
        trade_owner: 'trade_owner1',
        taker_order_id: 'taker1',
        maker_orders: [],
        matchtime: '2024-01-01T00:00:00Z',
        last_update: '2024-01-01T00:00:00Z',
        timestamp: '2024-01-01T00:00:00Z',
      };

      ws.simulateMessage(tradeEvent);

      assert.deepStrictEqual(events, [tradeEvent]);
    });

    it('onOrder() should receive order events', async () => {
      client = new ClobUserClient(testAuth);
      const events: ClobOrderEvent[] = [];

      client.onOrder((event) => events.push(event));

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      const orderEvent: ClobOrderEvent = {
        event_type: 'order',
        type: 'PLACEMENT',
        id: 'order1',
        asset_id: 'asset1',
        market: 'market1',
        side: 'BUY',
        price: '0.55',
        original_size: '100',
        size_matched: '0',
        outcome: 'Yes',
        owner: 'owner1',
        order_owner: 'order_owner1',
        associate_trades: null,
        timestamp: '2024-01-01T00:00:00Z',
      };

      ws.simulateMessage(orderEvent);

      assert.deepStrictEqual(events, [orderEvent]);
    });

    it('onUserMessage() should receive all events', async () => {
      client = new ClobUserClient(testAuth);
      const events: unknown[] = [];

      client.onUserMessage((event) => events.push(event));

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      const tradeEvent = {
        event_type: 'trade',
        type: 'TRADE',
        id: 'trade1',
      };

      ws.simulateMessage(tradeEvent);

      assert.strictEqual(events.length, 1);
    });

    it('should handle callback errors gracefully', async () => {
      const consoleSpy = mock.method(console, 'error', () => {});
      client = new ClobUserClient(testAuth);

      client.onTrade(() => {
        throw new Error('Test error');
      });

      const connectPromise = client.connect();
      const ws = MockWebSocket.getLastInstance()!;
      ws.simulateOpen();
      await connectPromise;

      ws.simulateMessage({ event_type: 'trade', type: 'TRADE', id: 't1' });

      assert.ok(consoleSpy.mock.calls.length > 0);
      consoleSpy.mock.restore();
    });
  });

  describe('reconnection', () => {
    it('should resubscribe with auth on reconnection', async () => {
      client = new ClobUserClient(testAuth, {
        autoReconnect: true,
        reconnectDelay: 10,
      });

      client.subscribe(['market1']);

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

      const resubSent = ws2.getLastSentMessageParsed<Record<string, unknown>>();
      assert.strictEqual(resubSent?.type, 'subscribe');
      assert.deepStrictEqual(resubSent?.auth, {
        apiKey: testAuth.apiKey,
        secret: testAuth.secret,
        passphrase: testAuth.passphrase,
      });
      assert.deepStrictEqual(resubSent?.markets, ['market1']);
    });
  });
});

describe('ClobClient', () => {
  let originalWebSocket: typeof WebSocket;
  let client: ClobClient;
  const testAuth = {
    apiKey: 'test-key',
    secret: 'test-secret',
    passphrase: 'test-passphrase',
  };

  beforeEach(() => {
    originalWebSocket = globalThis.WebSocket;
    installMockWebSocket();
    MockWebSocket.clearInstances();
  });

  afterEach(() => {
    client?.disconnect();
    globalThis.WebSocket = originalWebSocket;
  });

  describe('market property', () => {
    it('should create market client on first access', () => {
      client = new ClobClient();

      const market1 = client.market;
      const market2 = client.market;

      assert.strictEqual(market1, market2);
      assert.ok(market1 instanceof ClobMarketClient);
    });
  });

  describe('user property', () => {
    it('should create user client on first access with auth', () => {
      client = new ClobClient({ auth: testAuth });

      const user1 = client.user;
      const user2 = client.user;

      assert.strictEqual(user1, user2);
      assert.ok(user1 instanceof ClobUserClient);
    });

    it('should throw without auth', () => {
      client = new ClobClient();

      assert.throws(() => {
        void client.user;
      }, /requires auth/i);
    });
  });

  describe('connectMarket()', () => {
    it('should connect to market channel', async () => {
      client = new ClobClient();

      const connectPromise = client.connectMarket();
      MockWebSocket.getLastInstance()?.simulateOpen();
      await connectPromise;

      assert.strictEqual(client.market.isConnected, true);
    });
  });

  describe('connectUser()', () => {
    it('should connect to user channel', async () => {
      client = new ClobClient({ auth: testAuth });

      const connectPromise = client.connectUser();
      MockWebSocket.getLastInstance()?.simulateOpen();
      await connectPromise;

      assert.strictEqual(client.user.isConnected, true);
    });
  });

  describe('connectAll()', () => {
    it('should connect to both channels', async () => {
      client = new ClobClient({ auth: testAuth });

      const connectPromise = client.connectAll();

      await new Promise((resolve) => setTimeout(resolve, 10));

      const instances = MockWebSocket.getAllInstances();
      assert.strictEqual(instances.length, 2);

      instances.forEach((ws) => ws.simulateOpen());
      await connectPromise;

      assert.strictEqual(client.market.isConnected, true);
      assert.strictEqual(client.user.isConnected, true);
    });
  });

  describe('disconnect()', () => {
    it('should disconnect from all channels', async () => {
      client = new ClobClient({ auth: testAuth });

      const connectPromise = client.connectAll();

      await new Promise((resolve) => setTimeout(resolve, 10));
      MockWebSocket.getAllInstances().forEach((ws) => ws.simulateOpen());
      await connectPromise;

      client.disconnect();

      await new Promise((resolve) => setTimeout(resolve, 10));

      assert.strictEqual(client.market.isConnected, false);
      assert.strictEqual(client.user.isConnected, false);
    });

    it('should handle disconnect when not connected', () => {
      client = new ClobClient();

      // Should not throw
      client.disconnect();
    });
  });
});
