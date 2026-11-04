import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  base: '/assets/',
  plugins: [react(), tailwindcss()],
  build: {
    outDir: '../internal/assets',
    assetsDir: '',
    assetsInlineLimit: 0,
    emptyOutDir: true,
    sourcemap: false,
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      '/api': {
        target: process.env.API_PROXY_TARGET || 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    environmentOptions: { jsdom: { url: 'http://localhost:8080' } },
    clearMocks: true,
    restoreMocks: true,
  },
});
