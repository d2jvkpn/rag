<template>
  <n-config-provider :locale="naiveLocale" :date-locale="naiveDateLocale">
    <n-message-provider>
      <n-dialog-provider>
        <router-view />
      </n-dialog-provider>
    </n-message-provider>
  </n-config-provider>
</template>

<script setup>
import { computed } from 'vue'
import { NConfigProvider, NMessageProvider, NDialogProvider, zhCN, enUS, dateZhCN, dateEnUS } from 'naive-ui'
import { useLocaleStore } from './stores/locale.js'

const localeStore = useLocaleStore()

const naiveLocale = computed(() => localeStore.locale === 'zh' ? zhCN : enUS)
const naiveDateLocale = computed(() => localeStore.locale === 'zh' ? dateZhCN : dateEnUS)
</script>
