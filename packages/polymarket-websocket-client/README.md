# polymarket-websocket-client

[![npm version](https://badge.fury.io/js/polymarket-websocket-client.svg)](https://badge.fury.io/js/polymarket-websocket-client)

Zero-dependency TypeScript WebSocket client for Polymarket CLOB and RTDS APIs. Supports all WebSocket channels with automatic reconnection, heartbeat, and comprehensive error handling.

## Features

- **Zero Dependencies**: Uses only Node.js native WebSocket (requires Node.js 22+)
- **Full Channel Support**: CLOB Market, CLOB User, and RTDS channels
- **Auto Reconnection**: Exponential backoff with configurable retries
- **Heartbeat**: Automatic ping/pong to maintain connections
- **TypeScript First**: Complete type definitions for all events
- **Dual Module Output**: ESM and CommonJS support

## Installation

```bash
npm install polymarket-websocket-client
# or
pnpm add polymarket-websocket-client
# or
yarn add polymarket-websocket-client
```

## Quick Start

### CLOB Market Channel (Public Orderbook Data)

```typescript
import { ClobMarketClient } from 'polymarket-websocket-client';

const client = new ClobMarketClient();

// Subscribe to orderbook updates
client.onBook((event) => {
  console.log('Orderbook:', event.asset_id, event.bids, event.asks);
});

client.onPriceChange((event) => {
  console.log('Price change:', event.price_changes);
});

client.onLastTradePrice((event) => {
  console.log('Trade:', event.asset_id, event.price, event.size);
});

// Connect and subscribe to assets
await client.connect();
client.subscribe(['71321045679252212594626385532706912750332728571942532289631379312455583992563']);
```

### CLOB User Channel (Authenticated Trading Data)

```typescript
import { ClobUserClient } from 'polymarket-websocket-client';

const client = new ClobUserClient({
  apiKey: 'your-api-key',
  secret: 'your-secret',
  passphrase: 'your-passphrase',
});

// Listen for your trades and orders
client.onTrade((event) => {
  console.log('Trade:', event.id, event.status, event.price, event.size);
});

client.onOrder((event) => {
  console.log('Order:', event.id, event.type, event.price);
});

// Connect and subscribe to markets
await client.connect();
client.subscribe(['0xbd31dc8a20211944f6b70f31557f1001557b59905b7738480ca09bd4532f84af']);
```

### RTDS (Real-Time Data Socket)

```typescript
import { RtdsClient } from 'polymarket-websocket-client';

const client = new RtdsClient();

// Listen for crypto price updates
client.onCryptoPrice((message) => {
  console.log('Price:', message.payload.symbol, message.payload.value);
});

// Listen for comments
client.onComment((message) => {
  console.log('Comment:', message.payload.body);
});

await client.connect();

// Subscribe to crypto prices (Binance)
client.subscribeCryptoPrices(['btcusdt', 'ethusdt']);

// Subscribe to comments
client.subscribeComments('comment_created');
```

### Unified CLOB Client

```typescript
import { ClobClient } from 'polymarket-websocket-client';

const client = new ClobClient({
  auth: {
    apiKey: 'your-api-key',
    secret: 'your-secret',
    passphrase: 'your-passphrase',
  },
});

// Access both channels
client.market.onBook((event) => console.log('Book:', event));
client.user.onTrade((event) => console.log('Trade:', event));

// Connect to both channels
await client.connectAll();

// Subscribe
client.market.subscribe(['asset-id']);
client.user.subscribe(['market-id']);

// Disconnect all
client.disconnect();
```

## Configuration Options

All clients accept configuration options:

```typescript
interface ClientOptions {
  url?: string;                    // Custom WebSocket URL
  autoReconnect?: boolean;         // Enable auto-reconnection (default: true)
  maxReconnectAttempts?: number;   // Max reconnection attempts (default: Infinity)
  reconnectDelay?: number;         // Base delay between reconnects in ms (default: 1000)
  maxReconnectDelay?: number;      // Max reconnection delay in ms (default: 30000)
  heartbeatInterval?: number;      // Heartbeat interval in ms (default: 30000 for CLOB, 5000 for RTDS)
  connectionTimeout?: number;      // Connection timeout in ms (default: 10000)
}
```

## Connection Events

All clients emit connection lifecycle events:

```typescript
client.on('connected', () => {
  console.log('Connected!');
});

client.on('disconnected', ({ code, reason }) => {
  console.log('Disconnected:', code, reason);
});

client.on('reconnecting', ({ attempt, maxAttempts }) => {
  console.log(`Reconnecting... attempt ${attempt}/${maxAttempts}`);
});

client.on('error', (error) => {
  console.error('Error:', error);
});

client.on('stateChange', ({ state, previousState }) => {
  console.log(`State changed: ${previousState} -> ${state}`);
});
```

## API Reference

### ClobMarketClient

| Method | Description |
|--------|-------------|
| `connect()` | Connect to the WebSocket server |
| `disconnect()` | Disconnect from the server |
| `subscribe(assetIds)` | Subscribe to asset IDs (token IDs) |
| `unsubscribe(assetIds)` | Unsubscribe from asset IDs |
| `onBook(callback)` | Listen for orderbook snapshots |
| `onPriceChange(callback)` | Listen for price changes |
| `onTickSizeChange(callback)` | Listen for tick size changes |
| `onLastTradePrice(callback)` | Listen for last trade prices |
| `onMarketMessage(callback)` | Listen for all market events |

### ClobUserClient

| Method | Description |
|--------|-------------|
| `connect()` | Connect to the WebSocket server |
| `disconnect()` | Disconnect from the server |
| `subscribe(marketIds)` | Subscribe to market IDs (condition IDs) |
| `unsubscribe(marketIds)` | Unsubscribe from market IDs |
| `onTrade(callback)` | Listen for trade events |
| `onOrder(callback)` | Listen for order events |
| `onUserMessage(callback)` | Listen for all user events |

### RtdsClient

| Method | Description |
|--------|-------------|
| `connect()` | Connect to the WebSocket server |
| `disconnect()` | Disconnect from the server |
| `subscribeCryptoPrices(symbols?)` | Subscribe to Binance crypto prices |
| `subscribeCryptoPricesChainlink(symbol?)` | Subscribe to Chainlink prices |
| `subscribeComments(type?, gammaAuth?)` | Subscribe to comment events |
| `subscribeActivity(type, clobAuth?, gammaAuth?)` | Subscribe to activity events |
| `onCryptoPrice(callback)` | Listen for crypto price updates |
| `onComment(callback)` | Listen for comment events |
| `onActivity(callback)` | Listen for activity events |
| `onRtdsMessage(callback)` | Listen for all RTDS messages |

## Event Types

### CLOB Market Events

- `book`: Full orderbook snapshot
- `price_change`: Order placement/cancellation updates
- `tick_size_change`: Tick size changes at price extremes
- `last_trade_price`: Trade execution events

### CLOB User Events

- `trade`: Trade lifecycle (MATCHED → MINED → CONFIRMED/FAILED)
- `order`: Order lifecycle (PLACEMENT → UPDATE → CANCELLATION)

### RTDS Events

- `crypto_prices`: Binance price updates
- `crypto_prices_chainlink`: Chainlink price updates
- `comments`: Comment creation/removal and reactions

## Requirements

- Node.js 22+ (for native WebSocket support)

## License

MIT
