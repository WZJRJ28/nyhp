import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { resolve } from 'node:path';

const cacheDir = resolve(process.cwd(), 'backend/.cache/vite');
const srcDir = resolve(process.cwd(), 'src');

export default defineConfig({
  cacheDir,
  plugins: [react()],
  resolve: {
    alias: {
      '@': srcDir,
    },
  },
  build: {
    outDir: resolve(process.cwd(), 'backend/.cache/dist'),
    emptyOutDir: true,
  },
  server: {
    host: '127.0.0.1',
    port: 5173,
    proxy: {
      '/auth': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
    },
  },
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: './web-tests/setup.ts',
  },
});
