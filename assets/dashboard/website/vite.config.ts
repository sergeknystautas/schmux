import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path, { resolve } from 'path';

export default defineConfig({
  plugins: [react()],
  root: __dirname,
  base: process.env.VITE_BASE || '/',
  resolve: {
    alias: {
      '@dashboard': path.resolve(__dirname, '../src'),
    },
  },
  server: {
    port: 3000,
    strictPort: true,
  },
  build: {
    outDir: path.resolve(__dirname, '../../../dist/website'),
    emptyOutDir: true,
    assetsDir: 'assets',
    rollupOptions: {
      input: {
        main: resolve(__dirname, 'index.html'),
        demo: resolve(__dirname, 'demo/index.html'),
      },
    },
  },
});
