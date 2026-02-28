export interface Transport {
  createWebSocket(url: string): WebSocket;
  fetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response>;
}

export const liveTransport: Transport = {
  createWebSocket: (url: string) => new WebSocket(url),
  fetch: (input: RequestInfo | URL, init?: RequestInit) => window.fetch(input, init),
};

// Module-level singleton. ESM named exports are live bindings,
// so consumers importing `transport` see updates after setTransport().
export let transport: Transport = liveTransport;

export function setTransport(t: Transport) {
  transport = t;
}
