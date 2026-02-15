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
      server.middlewares.use('/__dev/pause-watch', (req, res, next) => {
        if (req.method !== 'POST') {
          res.writeHead(405, { 'Content-Type': 'application/json' });
          res.end(JSON.stringify({ error: 'Method not allowed' }));
          return;
        }
        paused = true;
        pendingReload = false;
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ paused: true }));
      });

      server.middlewares.use('/__dev/resume-watch', (req, res, next) => {
        if (req.method !== 'POST') {
          res.writeHead(405, { 'Content-Type': 'application/json' });
          res.end(JSON.stringify({ error: 'Method not allowed' }));
          return;
        }
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
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: './src/setupTests.ts',
  },
  build: {
    outDir: 'dist',
    assetsDir: 'assets',
    rollupOptions: {
      output: {
        manualChunks: {
          vendor: ['react', 'react-dom', 'react-router-dom'],
          xterm: ['@xterm/xterm', '@xterm/addon-fit', '@xterm/addon-web-links'],
        },
      },
    },
  },
});
