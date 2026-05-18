/**
 * Typed Event Emitter
 * A generic event emitter with full TypeScript support
 */

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type EventMap = { [key: string]: any };
type EventCallback<T> = (data: T) => void;

export class TypedEventEmitter<Events extends EventMap> {
  private listeners = new Map<keyof Events, Set<EventCallback<unknown>>>();

  /**
   * Subscribe to an event
   */
  on<K extends keyof Events>(event: K, callback: EventCallback<Events[K]>): () => void {
    if (!this.listeners.has(event)) {
      this.listeners.set(event, new Set());
    }
    this.listeners.get(event)!.add(callback as EventCallback<unknown>);

    // Return unsubscribe function
    return () => this.off(event, callback);
  }

  /**
   * Subscribe to an event for one-time execution
   */
  once<K extends keyof Events>(event: K, callback: EventCallback<Events[K]>): () => void {
    const onceCallback: EventCallback<Events[K]> = (data) => {
      this.off(event, onceCallback);
      callback(data);
    };
    return this.on(event, onceCallback);
  }

  /**
   * Unsubscribe from an event
   */
  off<K extends keyof Events>(event: K, callback: EventCallback<Events[K]>): void {
    const callbacks = this.listeners.get(event);
    if (callbacks) {
      callbacks.delete(callback as EventCallback<unknown>);
      if (callbacks.size === 0) {
        this.listeners.delete(event);
      }
    }
  }

  /**
   * Emit an event with data
   */
  protected emit<K extends keyof Events>(event: K, data: Events[K]): void {
    const callbacks = this.listeners.get(event);
    if (callbacks) {
      for (const callback of callbacks) {
        try {
          callback(data);
        } catch (error) {
          console.error(`Error in event listener for "${String(event)}":`, error);
        }
      }
    }
  }

  /**
   * Remove all listeners for a specific event or all events
   */
  removeAllListeners(event?: keyof Events): void {
    if (event !== undefined) {
      this.listeners.delete(event);
    } else {
      this.listeners.clear();
    }
  }

  /**
   * Get the number of listeners for an event
   */
  listenerCount(event: keyof Events): number {
    return this.listeners.get(event)?.size ?? 0;
  }
}
