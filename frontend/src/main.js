import { createApp } from 'vue'
import { createPinia } from 'pinia'
import naive from 'naive-ui'
import router from './router/index.js'
import App from './App.vue'
import { loadConfig } from './config/app-config.js'
import './styles/main.css'

async function bootstrap() {
  await loadConfig()
  const app = createApp(App)
  app.use(createPinia())
  app.use(router)
  app.use(naive)
  app.mount('#app')
}

bootstrap().catch(err => {
  document.body.innerHTML = `<div style="padding:2rem;font-family:monospace;color:#c00">
    <b>配置加载失败</b><br>${err.message}
  </div>`
})
