declare module 'https-proxy-agent' {
  import type { AgentConnectOpts } from 'agent-base';
  import type { OutgoingHttpHeaders } from 'http';

  export interface HttpsProxyAgentOptions {
    headers?: OutgoingHttpHeaders | (() => OutgoingHttpHeaders);
  }

  export class HttpsProxyAgent<T extends string = string> {
    constructor(url: string | URL, options?: HttpsProxyAgentOptions);
  }
}
