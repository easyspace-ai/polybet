/**
 * Mock WebSocket for testing
 */

type WebSocketEventHandler = ((event: Event) => void) | null;
type MessageEventHandler = ((event: MessageEvent) => void) | null;
type CloseEventHandler = ((event: CloseEvent) => void) | null;

export class MockWebSocket {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSING = 2;
  static readonly CLOSED = 3;

  readonly CONNECTING = 0;
  readonly OPEN = 1;
  readonly CLOSING = 2;
  readonly CLOSED = 3;

  url: string;
  readyState: number = MockWebSocket.CONNECTING;

  onopen: WebSocketEventHandler = null;
  onclose: CloseEventHandler = null;
  onerror: WebSocketEventHandler = null;
  onmessage: MessageEventHandler = null;

  sentMessages: string[] = [];

  private static instances: MockWebSocket[] = [];
  private shouldFailConnection = false;
  private connectionDelay = 0;

  constructor(url: string) {
    this.url = url;
    MockWebSocket.instances.push(this);
  }

  static getLastInstance(): MockWebSocket | undefined {
    return MockWebSocket.instances[MockWebSocket.instances.length - 1];
  }

  static getAllInstances(): MockWebSocket[] {
    return [...MockWebSocket.instances];
  }

  static clearInstances(): void {
    MockWebSocket.instances = [];
  }

  static setConnectionBehavior(fail: boolean, delay = 0): void {
    // This will be checked by simulateOpen
  }

  send(data: string): void {
    if (this.readyState !== MockWebSocket.OPEN) {
      throw new Error('WebSocket is not open');
    }
    this.sentMessages.push(data);
  }

  close(code?: number, reason?: string): void {
    if (this.readyState === MockWebSocket.CLOSED) return;

    this.readyState = MockWebSocket.CLOSING;

    // Simulate async close
    queueMicrotask(() => {
      this.readyState = MockWebSocket.CLOSED;
      if (this.onclose) {
        const event = {
          code: code ?? 1000,
          reason: reason ?? '',
          wasClean: true,
        } as CloseEvent;
        this.onclose(event);
      }
    });
  }

  // Test helpers
  simulateOpen(): void {
    this.readyState = MockWebSocket.OPEN;
    if (this.onopen) {
      this.onopen(new Event('open'));
    }
  }

  simulateMessage(data: string | object): void {
    if (this.onmessage) {
      const messageData = typeof data === 'string' ? data : JSON.stringify(data);
      this.onmessage({ data: messageData } as MessageEvent);
    }
  }

  simulateError(): void {
    if (this.onerror) {
      this.onerror(new Event('error'));
    }
  }

  simulateClose(code = 1000, reason = ''): void {
    this.readyState = MockWebSocket.CLOSED;
    if (this.onclose) {
      this.onclose({ code, reason, wasClean: true } as CloseEvent);
    }
  }

  getSentMessages(): string[] {
    return [...this.sentMessages];
  }

  getLastSentMessage(): string | undefined {
    return this.sentMessages[this.sentMessages.length - 1];
  }

  getLastSentMessageParsed<T = unknown>(): T | undefined {
    const msg = this.getLastSentMessage();
    if (msg) {
      try {
        return JSON.parse(msg) as T;
      } catch {
        return undefined;
      }
    }
    return undefined;
  }
}

export function installMockWebSocket(): typeof MockWebSocket {
  (globalThis as unknown as { WebSocket: typeof MockWebSocket }).WebSocket = MockWebSocket;
  return MockWebSocket;
}

export function uninstallMockWebSocket(original: typeof WebSocket): void {
  (globalThis as unknown as { WebSocket: typeof WebSocket }).WebSocket = original;
}
