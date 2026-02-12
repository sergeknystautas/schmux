import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Plugin that allows the Go backend to pause Vite's file watching during
// git operations (rebase, merge) to prevent transform errors from transient
// conflict markers in source files.
function pauseWatchPlugin() {
  let paused = false;
  let pendingReload = false;

  return {
    name: 'pause-watch',
    configureServer(server) {
      server.middlewares.use('/__dev/pause-watch', (_req, res) => {
        paused = true;
        pendingReload = false;
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ paused: true }));
      });

      server.middlewares.use('/__dev/resume-watch', (_req, res) => {
        const hadPending = pendingReload;
        paused = false;
        pendingReload = false;
        if (hadPending) {
          server.ws.send({ type: 'full-reload' });
        }
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ paused: false, reloaded: hadPending }));
      });
    },
    handleHotUpdate() {
      if (paused) {
        pendingReload = true;
        return [];
      }
    },
  };
}

export default defineConfig({
  plugins: [react(), pauseWatchPlugin()],
  server: {
    port: 5173,
    strictPort: true, // Fail if port is already in use
    cors: true, // Allow the Go proxy to connect
  },
  build: {
    outDir: 'dist',
    assetsDir: 'assets',
    chunkSizeWarningLimit: 1100,
  },
});
