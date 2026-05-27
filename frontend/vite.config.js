import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

const outDir = process.env.OUT_DIR || 'target/dist'
const basePath = process.env.BASE_PATH || '/'

export default defineConfig({
  base: basePath,
  plugins: [vue()],
  build: {
    outDir: outDir,
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
