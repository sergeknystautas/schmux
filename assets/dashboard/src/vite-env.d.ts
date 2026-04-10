/// <reference types="vite/client" />

declare module '*.module.css' {
  const classes: Record<string, string>;
  export default classes;
}

declare module '*.css';

declare module 'react-diff-viewer-continued';

// Global test/debug properties exposed for Playwright and scenario tests.
// These are set conditionally (dev mode or VITE_EXPOSE_TERMINAL builds).
interface Window {
  __schmuxTerminal?: import('@xterm/xterm').Terminal;
  __schmuxStream?: import('./lib/terminalStream').default;
  __inputLatency?: typeof import('./lib/inputLatency').inputLatency;
}
