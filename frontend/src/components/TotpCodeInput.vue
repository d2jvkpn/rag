<template>
  <div class="totp-boxes" @focusout="onFocusOut">
    <input
      v-for="i in 6"
      :key="i"
      ref="inputs"
      class="totp-box"
      type="text"
      inputmode="numeric"
      maxlength="1"
      :value="digits[i - 1]"
      @keydown="onKeydown($event, i - 1)"
      @input="onInput($event, i - 1)"
      @paste.prevent="onPaste($event)"
      @focus="$event.target.select()"
    />
  </div>
</template>

<script setup>
import { ref, watch } from 'vue'

const props = defineProps({ modelValue: { type: String, default: '' } })
const emit = defineEmits(['update:modelValue', 'blur', 'complete'])

const inputs = ref([])
const digits = ref(Array(6).fill(''))

watch(() => props.modelValue, (val) => {
  const chars = (val || '').split('')
  digits.value = Array(6).fill('').map((_, i) => chars[i] || '')
  if (!val && inputs.value[0]) inputs.value[0].focus()
}, { immediate: true })

function sync() {
  emit('update:modelValue', digits.value.join(''))
}

function onInput(e, idx) {
  const ch = e.target.value.replace(/\D/g, '').slice(-1)
  digits.value[idx] = ch
  sync()
  if (ch && idx < 5) {
    inputs.value[idx + 1].focus()
  } else if (ch && idx === 5) {
    emit('complete')
  }
}

function onKeydown(e, idx) {
  if (e.key === 'Backspace') {
    if (digits.value[idx]) {
      digits.value[idx] = ''
      sync()
    } else if (idx > 0) {
      digits.value[idx - 1] = ''
      sync()
      inputs.value[idx - 1].focus()
    }
    e.preventDefault()
  } else if (e.key === 'ArrowLeft' && idx > 0) {
    inputs.value[idx - 1].focus()
  } else if (e.key === 'ArrowRight' && idx < 5) {
    inputs.value[idx + 1].focus()
  }
}

function onFocusOut(e) {
  if (!e.currentTarget.contains(e.relatedTarget)) emit('blur')
}

function onPaste(e) {
  const text = (e.clipboardData || window.clipboardData).getData('text').replace(/\D/g, '').slice(0, 6)
  text.split('').forEach((ch, i) => { digits.value[i] = ch })
  digits.value.fill('', text.length)
  sync()
  const next = Math.min(text.length, 5)
  inputs.value[next].focus()
  if (text.length === 6) emit('complete')
}
</script>

<style scoped>
.totp-boxes {
  display: flex;
  gap: 8px;
  justify-content: center;
}
.totp-box {
  width: 44px;
  height: 52px;
  text-align: center;
  font-size: 22px;
  font-weight: 600;
  border: 1.5px solid var(--n-border-color, #e0e0e6);
  border-radius: 6px;
  background: transparent;
  color: inherit;
  outline: none;
  transition: border-color 0.2s;
  caret-color: transparent;
}
.totp-box:focus {
  border-color: var(--n-color-target, #18a058);
}
</style>
