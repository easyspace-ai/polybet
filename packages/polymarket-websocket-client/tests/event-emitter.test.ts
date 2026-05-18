/**
 * Tests for TypedEventEmitter
 */

import { describe, it, beforeEach, mock } from 'node:test';
import assert from 'node:assert';
import { TypedEventEmitter } from '../src/event-emitter.js';

interface TestEvents {
  message: string;
  data: { id: number; value: string };
  empty: void;
}

// Create a testable subclass that exposes emit
class TestableEmitter extends TypedEventEmitter<TestEvents> {
  public testEmit<K extends keyof TestEvents>(event: K, data: TestEvents[K]): void {
    this.emit(event, data);
  }
}

describe('TypedEventEmitter', () => {
  let emitter: TestableEmitter;

  beforeEach(() => {
    emitter = new TestableEmitter();
  });

  describe('on()', () => {
    it('should subscribe to an event and receive data', () => {
      const messages: string[] = [];
      emitter.on('message', (msg) => messages.push(msg));

      emitter.testEmit('message', 'hello');
      emitter.testEmit('message', 'world');

      assert.deepStrictEqual(messages, ['hello', 'world']);
    });

    it('should support multiple listeners for the same event', () => {
      const results: string[] = [];
      emitter.on('message', (msg) => results.push(`a:${msg}`));
      emitter.on('message', (msg) => results.push(`b:${msg}`));

      emitter.testEmit('message', 'test');

      assert.deepStrictEqual(results, ['a:test', 'b:test']);
    });

    it('should return an unsubscribe function', () => {
      const messages: string[] = [];
      const unsubscribe = emitter.on('message', (msg) => messages.push(msg));

      emitter.testEmit('message', 'before');
      unsubscribe();
      emitter.testEmit('message', 'after');

      assert.deepStrictEqual(messages, ['before']);
    });

    it('should handle object data correctly', () => {
      const received: Array<{ id: number; value: string }> = [];
      emitter.on('data', (d) => received.push(d));

      emitter.testEmit('data', { id: 1, value: 'test' });

      assert.deepStrictEqual(received, [{ id: 1, value: 'test' }]);
    });
  });

  describe('once()', () => {
    it('should only receive the first event', () => {
      const messages: string[] = [];
      emitter.once('message', (msg) => messages.push(msg));

      emitter.testEmit('message', 'first');
      emitter.testEmit('message', 'second');

      assert.deepStrictEqual(messages, ['first']);
    });

    it('should return an unsubscribe function that works before event fires', () => {
      const messages: string[] = [];
      const unsubscribe = emitter.once('message', (msg) => messages.push(msg));

      unsubscribe();
      emitter.testEmit('message', 'test');

      assert.deepStrictEqual(messages, []);
    });
  });

  describe('off()', () => {
    it('should remove a specific listener', () => {
      const messages: string[] = [];
      const listener = (msg: string) => messages.push(msg);

      emitter.on('message', listener);
      emitter.testEmit('message', 'before');
      emitter.off('message', listener);
      emitter.testEmit('message', 'after');

      assert.deepStrictEqual(messages, ['before']);
    });

    it('should only remove the specified listener', () => {
      const results: string[] = [];
      const listener1 = (msg: string) => results.push(`1:${msg}`);
      const listener2 = (msg: string) => results.push(`2:${msg}`);

      emitter.on('message', listener1);
      emitter.on('message', listener2);
      emitter.off('message', listener1);
      emitter.testEmit('message', 'test');

      assert.deepStrictEqual(results, ['2:test']);
    });

    it('should handle removing non-existent listener gracefully', () => {
      const listener = () => {};
      // Should not throw
      emitter.off('message', listener);
    });

    it('should clean up empty listener sets', () => {
      const listener = () => {};
      emitter.on('message', listener);
      assert.strictEqual(emitter.listenerCount('message'), 1);

      emitter.off('message', listener);
      assert.strictEqual(emitter.listenerCount('message'), 0);
    });
  });

  describe('emit()', () => {
    it('should catch and log errors from listeners', () => {
      const consoleSpy = mock.method(console, 'error', () => {});

      emitter.on('message', () => {
        throw new Error('Test error');
      });

      // Should not throw
      emitter.testEmit('message', 'test');

      assert.strictEqual(consoleSpy.mock.calls.length, 1);
      consoleSpy.mock.restore();
    });

    it('should continue executing other listeners after one throws', () => {
      const consoleSpy = mock.method(console, 'error', () => {});
      const messages: string[] = [];

      emitter.on('message', () => {
        throw new Error('Test error');
      });
      emitter.on('message', (msg) => messages.push(msg));

      emitter.testEmit('message', 'test');

      assert.deepStrictEqual(messages, ['test']);
      consoleSpy.mock.restore();
    });

    it('should do nothing when no listeners exist', () => {
      // Should not throw
      emitter.testEmit('message', 'test');
    });
  });

  describe('removeAllListeners()', () => {
    it('should remove all listeners for a specific event', () => {
      emitter.on('message', () => {});
      emitter.on('message', () => {});
      emitter.on('data', () => {});

      emitter.removeAllListeners('message');

      assert.strictEqual(emitter.listenerCount('message'), 0);
      assert.strictEqual(emitter.listenerCount('data'), 1);
    });

    it('should remove all listeners when no event specified', () => {
      emitter.on('message', () => {});
      emitter.on('data', () => {});

      emitter.removeAllListeners();

      assert.strictEqual(emitter.listenerCount('message'), 0);
      assert.strictEqual(emitter.listenerCount('data'), 0);
    });
  });

  describe('listenerCount()', () => {
    it('should return 0 for events with no listeners', () => {
      assert.strictEqual(emitter.listenerCount('message'), 0);
    });

    it('should return correct count of listeners', () => {
      emitter.on('message', () => {});
      emitter.on('message', () => {});
      emitter.on('message', () => {});

      assert.strictEqual(emitter.listenerCount('message'), 3);
    });

    it('should update count when listeners are removed', () => {
      const unsub1 = emitter.on('message', () => {});
      emitter.on('message', () => {});

      assert.strictEqual(emitter.listenerCount('message'), 2);
      unsub1();
      assert.strictEqual(emitter.listenerCount('message'), 1);
    });
  });
});
