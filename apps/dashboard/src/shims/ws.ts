/** Browser build stub — dashboard uses native WebSocket via polymarket-websocket-client. */
export default class WebSocketStub {
  constructor() {
    throw new Error("ws package is not available in the browser bundle");
  }
}
