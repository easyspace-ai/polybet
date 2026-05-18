/**
 * Runtime WebSocket factory: browser uses global WebSocket; Node uses `ws` (+ optional proxy).
 */

export interface NodeWebSocketOptions {
  handshakeTimeout?: number;
  agent?: unknown;
}

export function isBrowserRuntime(): boolean {
  return typeof globalThis.WebSocket !== 'undefined';
}

/** Create a WebSocket synchronously (browser, tests with MockWebSocket, etc.). */
export function createPlatformWebSocketSync(url: string, options?: NodeWebSocketOptions): WebSocket {
  if (options?.agent) {
    console.warn(
      '[polymarket-websocket-client] proxyUrl is ignored in browser; configure system/Electron proxy instead.',
    );
  }
  return new globalThis.WebSocket(url);
}

/** Create a WebSocket in Node (uses `ws` + optional proxy agent). */
export async function createPlatformWebSocket(
  url: string,
  options?: NodeWebSocketOptions,
): Promise<WebSocket> {
  if (isBrowserRuntime()) {
    return createPlatformWebSocketSync(url, options);
  }

  const { default: WS } = await import('ws');
  const wsOptions: import('ws').ClientOptions = {
    handshakeTimeout: options?.handshakeTimeout,
  };
  if (options?.agent) {
    wsOptions.agent = options.agent as import('ws').ClientOptions['agent'];
  }
  return new WS(url, wsOptions) as unknown as WebSocket;
}
