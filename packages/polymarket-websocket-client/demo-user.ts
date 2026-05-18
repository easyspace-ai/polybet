/**
 * Polymarket User Channel Demo
 *
 * This demo shows how to use the authenticated CLOB User channel.
 * Requires API credentials from Polymarket.
 *
 * Usage:
 *   export POLYMARKET_API_KEY=your-api-key
 *   export POLYMARKET_API_SECRET=your-api-secret
 *   export POLYMARKET_API_PASSPHRASE=your-api-passphrase
 *   npx tsx demo-user.ts
 */

import { ClobUserClient } from './src/clob-client.js';

async function main() {
  console.log('\n=== CLOB User Channel Demo ===\n');

  const apiKey = "";
  const apiSecret = "";
  const apiPassphrase = "";

  if (!apiKey || !apiSecret || !apiPassphrase) {
    console.log('⚠️  Missing API credentials!\n');
    console.log('Set environment variables:');
    console.log('  export POLYMARKET_API_KEY=your-api-key');
    console.log('  export POLYMARKET_API_SECRET=your-api-secret');
    console.log('  export POLYMARKET_API_PASSPHRASE=your-api-passphrase');
    console.log('\nGet your API credentials at: https://polymarket.com/settings');
    console.log('\nExiting...\n');
    process.exit(1);
  }

  console.log('API Key:', apiKey.slice(0, 8) + '...');

  const client = new ClobUserClient({
    apiKey,
    secret: apiSecret,
    passphrase: apiPassphrase,
  }, {
    proxyUrl: 'http://127.0.0.1:15236',
    connectionTimeout: 30000,
  });

  client.on('stateChange', ({ state }) => {
    console.log('[STATE]', state);
  });

  client.on('error', (error) => {
    console.error('[ERROR]', error.message);
  });

  client.on('disconnected', ({ code, reason }) => {
    console.log('[DISCONNECTED]', code, reason);
  });

  client.onTrade((event) => {
    console.log('\n[TRADE]');
    console.log('  ID:', event.id);
    console.log('  Asset:', event.asset_id.slice(0, 30) + '...');
    console.log('  Side:', event.side);
    console.log('  Price:', event.price);
    console.log('  Size:', event.size);
    console.log('  Status:', event.status);
  });

  client.onOrder((event) => {
    console.log('\n[ORDER]');
    console.log('  ID:', event.id);
    console.log('  Type:', event.type);
    console.log('  Asset:', event.asset_id.slice(0, 30) + '...');
    console.log('  Side:', event.side);
    console.log('  Price:', event.price);
    console.log('  Original Size:', event.original_size);
    console.log('  Matched:', event.size_matched);
  });

  try {
    await client.connect();
    console.log('Connected!\n');

    // Subscribe to all markets (empty array = all)
    // Or specify specific condition IDs: client.subscribe(['0xcondition_id_1', '0xcondition_id_2'])
    client.subscribe([]);
    console.log('Subscribed to all markets');

    console.log('\nWaiting for order/trade events...');
    console.log('Place an order on Polymarket to see events here.\n');

  } catch (error) {
    console.error('Connection failed:', error);
  }

  setTimeout(() => {
    console.log('\nDisconnecting...');
    client.disconnect();
    process.exit(0);
  }, 600000);
}

main().catch(console.error);
