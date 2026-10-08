import { clientPerf } from './clientPerf';

export interface Transport {
  createWebSocket(url: string): WebSocket;
  fetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response>;
}

export const liveTransport: Transport = {
  createWebSocket: (url: string) => new WebSocket(url),
  fetch: (input: RequestInfo | URL, init?: RequestInit) => window.fetch(input, init),
};

// Wrapping an already-instrumented transport would count every message twice.
const instrumentedTransports = new WeakSet<Transport>();

function instrumented(t: Transport): Transport {
  if (instrumentedTransports.has(t)) return t;
  const wrapped: Transport = {
    ...t,
    createWebSocket: (url: string) => clientPerf.wrapSocket(t.createWebSocket(url), url),
  };
  instrumentedTransports.add(wrapped);
  return wrapped;
}

// Module-level singleton. ESM named exports are live bindings,
// so consumers importing `transport` see updates after setTransport().
export let transport: Transport = instrumented(liveTransport);

export function setTransport(t: Transport) {
  transport = instrumented(t);
}
