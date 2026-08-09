/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath, URL } from 'node:url'

// Node 26 defines its own localStorage and sessionStorage globals. Vitest
// populates the test global from the jsdom window only for keys the global does
// not already have, so Node's definitions take precedence: localStorage reads as
// undefined unless node was started with --localstorage-file, and sessionStorage
// resolves to Node's store instead of jsdom's. Turning Node's Web Storage off in the test
// workers puts jsdom back in charge, which is what the browser-shaped tests
// expect. Applied only when the running Node actually defines these globals, so
// this stays a no-op — rather than an unknown-flag crash — on older versions.
const testExecArgv = 'localStorage' in globalThis ? ['--no-experimental-webstorage'] : []

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    port: 3000,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: './src/test-setup.ts',
    execArgv: testExecArgv,
    // The jsdom suite shares browser-shaped globals and currently deadlocks
    // under Vitest's default worker pool in constrained containers. Keep the
    // standard make test-ui command deterministic; individual runs can still
    // opt into a different pool when profiling worker parallelism.
    pool: 'vmThreads',
    maxWorkers: 1,
  },
})
