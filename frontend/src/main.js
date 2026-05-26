import { createApp } from 'vue'
import { createPinia } from 'pinia'
import {
  NAlert,
  NBadge,
  NButton,
  NCard,
  NCheckbox,
  NCheckboxGroup,
  NDataTable,
  NDescriptions,
  NDescriptionsItem,
  NDialogProvider,
  NDrawer,
  NDrawerContent,
  NDropdown,
  NDynamicTags,
  NEmpty,
  NForm,
  NFormItem,
  NIcon,
  NInput,
  NLayout,
  NLayoutContent,
  NLayoutSider,
  NMenu,
  NMessageProvider,
  NModal,
  NRadioButton,
  NRadioGroup,
  NScrollbar,
  NSelect,
  NSpace,
  NSpin,
  NTag,
  NText,
  NUpload,
  NUploadDragger,
} from 'naive-ui'
import router from './router/index.js'
import App from './App.vue'
import { loadConfig } from './config/app-config.js'
import './styles/main.css'

async function bootstrap() {
  await loadConfig()
  const app = createApp(App)
  app.use(createPinia())
  app.use(router)
  app.component('NAlert', NAlert)
  app.component('NBadge', NBadge)
  app.component('NButton', NButton)
  app.component('NCard', NCard)
  app.component('NCheckbox', NCheckbox)
  app.component('NCheckboxGroup', NCheckboxGroup)
  app.component('NDataTable', NDataTable)
  app.component('NDescriptions', NDescriptions)
  app.component('NDescriptionsItem', NDescriptionsItem)
  app.component('NDialogProvider', NDialogProvider)
  app.component('NDrawer', NDrawer)
  app.component('NDrawerContent', NDrawerContent)
  app.component('NDropdown', NDropdown)
  app.component('NDynamicTags', NDynamicTags)
  app.component('NEmpty', NEmpty)
  app.component('NForm', NForm)
  app.component('NFormItem', NFormItem)
  app.component('NIcon', NIcon)
  app.component('NInput', NInput)
  app.component('NLayout', NLayout)
  app.component('NLayoutContent', NLayoutContent)
  app.component('NLayoutSider', NLayoutSider)
  app.component('NMenu', NMenu)
  app.component('NMessageProvider', NMessageProvider)
  app.component('NModal', NModal)
  app.component('NRadioButton', NRadioButton)
  app.component('NRadioGroup', NRadioGroup)
  app.component('NScrollbar', NScrollbar)
  app.component('NSelect', NSelect)
  app.component('NSpace', NSpace)
  app.component('NSpin', NSpin)
  app.component('NTag', NTag)
  app.component('NText', NText)
  app.component('NUpload', NUpload)
  app.component('NUploadDragger', NUploadDragger)
  app.mount('#app')
}

bootstrap().catch(err => {
  document.body.innerHTML = `<div style="padding:2rem;font-family:monospace;color:#c00">
    <b>配置加载失败</b><br>${err.message}
  </div>`
})
