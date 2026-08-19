// Singleton store for the daemon host's load average, received via
// server_load messages on /ws/dashboard. Written by useSessionsWebSocket,
// read by the ServerLoad debug pane. Cleared to null on socket close so a
// backend that never sends server_load (debug off, older daemon) shows
// the waiting state instead of a value from a previous runtime.

export interface ServerLoadAvg {
  one: number;
  five: number;
  fifteen: number;
}

let _latest: ServerLoadAvg | null = null;
let _version = 0;

export function updateServerLoad(load: ServerLoadAvg | null) {
  _latest = load;
  _version++;
}

export function getServerLoad(): ServerLoadAvg | null {
  return _latest;
}

export function getServerLoadVersion(): number {
  return _version;
}
