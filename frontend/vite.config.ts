import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  server: {
    proxy: {
      '/api': {
        target: process.env.VITE_API_PROXY ?? 'http://127.0.0.1:8080',
        changeOrigin: true,
        xfwd: true,
      },
    },
  },
  test: {
    environment: 'jsdom',
    clearMocks: true,
    restoreMocks: true,
    unstubGlobals: true,
  },
})

