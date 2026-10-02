/// <reference types="vitest/config" />

import react from '@vitejs/plugin-react'
import path from 'node:path'
import { defineConfig, searchForWorkspaceRoot } from 'vite'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  build: {
    outDir: 'dist',
  },
  server: {
    fs: {
      // The translation catalog is shared with the iPhone app, outside web/.
      allow: [searchForWorkspaceRoot(process.cwd()), path.resolve(__dirname, '../i18n')],
    },
    proxy: {
      '/api': {
        target: 'http://localhost:7331',
        changeOrigin: true,
      },
      '/v1': {
        target: 'http://localhost:7331',
        changeOrigin: true,
      },
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
    globals: true,
  },
})
