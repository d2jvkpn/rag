import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { execSync } from 'child_process'

const outDir = process.env.OUT_DIR || 'target/dist'
const basePath = normalizeBasePath(process.env.BASE_PATH || '/')

function normalizeBasePath(path) {
  if (path === './' || path === '') return path || '/'
  return path.endsWith('/') ? path : path + '/'
}

function gitCmd(args) {
  try {
    return execSync(`git ${args}`, { encoding: 'utf8' }).trim()
  } catch {
    return 'unknown'
  }
}

const gitBranch = process.env.GIT_BRANCH || gitCmd('rev-parse --abbrev-ref HEAD')
const gitCommit = process.env.GIT_COMMIT || gitCmd('rev-parse HEAD')
const commitTime = process.env.COMMIT_TIME || gitCmd('log -1 --format=%cI')

export default defineConfig({
  base: basePath,
  plugins: [vue()],
  define: {
    __GIT_BRANCH__: JSON.stringify(gitBranch),
    __GIT_COMMIT__: JSON.stringify(gitCommit),
    __COMMIT_TIME__: JSON.stringify(commitTime),
  },
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
