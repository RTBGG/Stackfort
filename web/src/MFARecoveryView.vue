<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

defineProps<{ codes: string[] }>()
const emit = defineEmits<{ done: [] }>()
const { t } = useI18n()
const saved = ref(false)
const heading = ref<HTMLHeadingElement | null>(null)
onMounted(() => heading.value?.focus())
</script>

<template>
  <main class="loading-screen">
    <section class="panel management-form mfa-recovery">
      <h1 ref="heading" tabindex="-1">{{ t('identitySecurity.recoveryTitle') }}</h1>
      <p>{{ t('identitySecurity.recoveryBody') }}</p>
      <p class="form-hint">{{ t('identitySecurity.recoveryWarning') }}</p>
      <ul class="recovery-codes"><li v-for="item in codes" :key="item"><code>{{ item }}</code></li></ul>
      <label class="check-field"><input v-model="saved" type="checkbox"><span>{{ t('identitySecurity.saved') }}</span></label>
      <button class="primary-action" type="button" :disabled="!saved" @click="saved && emit('done')">{{ t('identitySecurity.login') }}</button>
    </section>
  </main>
</template>
