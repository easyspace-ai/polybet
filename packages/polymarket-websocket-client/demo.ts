/**
 * Polymarket WebSocket Demo
 * Tests CLOB Market and RTDS clients
 */

import { ClobMarketClient, ClobClient } from './src/clob-client.js';
import { RtdsClient } from './src/rtds-client.js';

const DEMO_TOKEN_ID = '11015470973684177829729219287262166995141465048508201953575582100565462316088';

async function demoCLOBMarket() {
  console.log('\n=== CLOB Market Channel Demo ===\n');

  const client = new ClobMarketClient({
    proxyUrl: 'http://127.0.0.1:15236',
    connectionTimeout: 30000,
  });

  const subscribedTokens = new Set<string>();

  client.on('stateChange', ({ state }) => {
    console.log('[STATE]', state);
  });

  client.on('error', (error) => {
    console.error('[ERROR]', error.message);
  });

  client.onBook((event) => {
    console.log('[BOOK]', event.asset_id.slice(0, 25) + '...', {
      bids: event.bids.length,
      asks: event.asks.length,
    });
  });

  client.onPriceChange((event) => {
    console.log('[PRICE_CHANGE]', event.price_changes.length, 'changes');
  });

  client.onLastTradePrice((event) => {
    console.log('[LAST_TRADE]', event.asset_id.slice(0, 25) + '...', '->', event.price);
  });

  client.on('rawMessage', (msg) => {
    if (msg.startsWith('{"id"') && msg.includes('clob_token_ids')) {
      try {
        const data = JSON.parse(msg);
        if (data.event_type === 'new_market' && data.clob_token_ids) {
          console.log('[NEW_MARKET]', data.question?.slice(0, 50));
          const yesToken = data.clob_token_ids[0];
          if (!subscribedTokens.has(yesToken)) {
            subscribedTokens.add(yesToken);
            client.subscribe([yesToken]);
            console.log('[AUTO_SUB]', yesToken.slice(0, 25) + '...');
          }
        }
      } catch {
        // Ignore parse errors
      }
    }
  });

  await client.connect();

  console.log('Subscribing to initial markets...');
  client.subscribe([DEMO_TOKEN_ID]);

  setTimeout(() => {
    console.log('\nDisconnecting CLOB market...');
    console.log('Total subscribed tokens:', subscribedTokens.size);
    client.disconnect();
    process.exit(0);
  }, 30000);
}

async function demoRTDS() {
  console.log('\n=== RTDS Demo ===\n');

  const client = new RtdsClient({
    proxyUrl: 'http://127.0.0.1:15236',
  });

  client.onCryptoPrice((message) => {
    console.log('[CRYPTO]', message.topic, message.payload.symbol, message.payload.value);
  });

  client.onComment((message) => {
    console.log('[COMMENT]', message.payload.body.slice(0, 50));
  });

  client.onActivity((message) => {
    console.log('[ACTIVITY]', message.topic, message.type);
  });

  client.onRtdsMessage((message) => {
    console.log('[RTDS MSG]', message.topic, message.type);
  });

  client.on('stateChange', ({ state }) => {
    console.log('[STATE]', state);
  });

  await client.connect();

  client.subscribeCryptoPrices();
  client.subscribeCryptoPricesChainlink();
  client.subscribeComments();

  setTimeout(() => {
    console.log('\nDisconnecting RTDS...');
    client.disconnect();
    process.exit(0);
  }, 10000);
}

async function main() {
  console.log('Polymarket WebSocket Client Demo');
  console.log('================================\n');

  const mode = process.argv[2] || 'rtds';

  switch (mode) {
    case 'clob-market':
      await demoCLOBMarket();
      break;
    case 'rtds':
      await demoRTDS();
      break;
    default:
      console.log('Usage: npx tsx demo.ts [clob-market|rtds]');
      console.log('\nDemos:');
      console.log('  clob-market - CLOB market channel (orderbook, prices)');
      console.log('  rtds        - RTDS (crypto prices, comments)');
      console.log('\nFor User Channel (authenticated):');
      console.log('  npx tsx demo-user.ts');
  }
}

main().catch(console.error);
