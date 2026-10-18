import { defineConfig } from 'vite';
import { resolve } from 'node:path';

export default defineConfig(({ command }) => ({
  root: resolve(import.meta.dirname),
  publicDir: false,
  build: { outDir: resolve(import.meta.dirname, '../build/vite-web'), emptyOutDir: true, copyPublicDir: false },
  server: { proxy: { '/api/v1': { target: 'http://127.0.0.1:12382', changeOrigin: false } } },
  // Vercel serves the bundled files directly, so retain root-relative paths.
  ...(command === 'build' ? { base: '/' } : {}),
}));
