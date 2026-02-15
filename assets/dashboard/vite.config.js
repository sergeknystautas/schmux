import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Plugin that allows the Go backend to pause Vite's file watching during
// git operations (rebase, merge) to prevent transform errors from transient
// conflict markers in source files.
function pauseWatchPlugin() {
  let paused = false;
  let originalRestart = null;

  return {
    name: 'pause-watch',
    configureServer(server) {
      // Intercept server.restart() to prevent config-change restarts while paused.
      // When vite.config.js changes during git sync, Vite normally restarts the
      // server, which disconnects the browser and causes a page reload. By blocking
      // restart() while paused, we preserve the browser's connection and modal state.
      originalRestart = server.restart.bind(server);
      server.restart = async (...args) => {
        if (paused) {
          return;
        }
        return originalRestart(...args);
      };

      server.middlewares.use('/__dev/pause-watch', (req, res, next) => {
        if (req.method !== 'POST') {
          res.writeHead(405, { 'Content-Type': 'application/json' });
          res.end(JSON.stringify({ error: 'Method not allowed' }));
          return;
        }
        paused = true;
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ paused: true }));
      });

      server.middlewares.use('/__dev/resume-watch', (req, res, next) => {
        if (req.method !== 'POST') {
          res.writeHead(405, { 'Content-Type': 'application/json' });
          res.end(JSON.stringify({ error: 'Method not allowed' }));
          return;
        }
        paused = false;
        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ paused: false }));
      });
    },
    handleHotUpdate() {
      if (paused) {
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
