import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'path';

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

// Load Istanbul coverage plugin when VITE_COVERAGE is set (for scenario tests).
// Uses dynamic import so the plugin is only required when needed.
async function loadPlugins() {
  const plugins = [react(), pauseWatchPlugin()];

  if (process.env.VITE_COVERAGE === 'true') {
    try {
      const { default: istanbul } = await import('vite-plugin-istanbul');
      plugins.push(
        istanbul({
          include: 'src/**/*',
          exclude: ['node_modules', 'src/setupTests.ts'],
          extension: ['.js', '.ts', '.jsx', '.tsx'],
        })
      );
    } catch {
      console.warn('vite-plugin-istanbul not installed, skipping coverage instrumentation');
    }
  }

  return plugins;
}

export default defineConfig(async () => ({
  plugins: await loadPlugins(),
  resolve: {
    alias: {
      '@dashboard': path.resolve(__dirname, 'src'),
    },
  },
  server: {
    host: '::', // Listen on both IPv4 and IPv6 (Meta devservers are IPv6-only)
    port: 5173,
    strictPort: true, // Fail if port is already in use
    cors: true, // Allow the Go proxy to connect
  },
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: './src/setupTests.ts',
    coverage: {
      provider: 'v8',
      reporter: ['text', 'json'],
    },
  },
  build: {
    outDir: 'dist',
    assetsDir: 'assets',
    rollupOptions: {
      output: {
        manualChunks: {
          vendor: ['react', 'react-dom', 'react-router-dom'],
          xterm: [
            '@xterm/xterm',
            '@xterm/addon-fit',
            '@xterm/addon-web-links',
            '@xterm/addon-unicode11',
          ],
          markdown: ['react-markdown', 'remark-gfm', 'react-diff-viewer-continued'],
        },
      },
    },
  },
}));
