import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  build: {
    outDir: 'target/dist',
    emptyOutDir: true,
  },
  server: {
    port: 3062,
    proxy: {
      '/api': 'http://localhost:3061',
    },
  },
})
