import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  build: {
    outDir: 'target/dist',
    emptyOutDir: true,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('node_modules/naive-ui')) return 'vendor-naive-ui'
        },
      },
    },
  },
  server: {
    port: 3062,
    proxy: {
      '/api': 'http://localhost:3061',
    },
  },
})
