<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, ApiError, type PanelHostnameStatus } from './api'
import { monitorAdminOperation, operationPending } from './adminOperation'

const { t, te } = useI18n()
const status = ref<PanelHostnameStatus | null>(null)
const busy = ref(false)
const pending = ref(false)
const error = ref('')
const success = ref(false)
const form = reactive({ hostname: '', email: '', acceptTerms: false, confirmOriginChange: false })
let live = true
let stop: (() => void) | undefined
const message = computed(() => te(`errors.${error.value}`) ? t(`errors.${error.value}`) : t('panelHostname.failed'))
const endpoint = computed(() => {
  const hostname = status.value?.hostname
  // Only the canonical HTTPS endpoint may become a navigation link.
  return status.value?.enabled && hostname && /^[a-z0-9.-]+$/.test(hostname)
    && status.value.url === `https://${hostname}/` ? status.value.url : ''
})

function track(id: string) {
  pending.value = true
  stop?.()
  stop = monitorAdminOperation(id, () => {}, async (op) => {
    pending.value = false
    success.value = op.status === 'succeeded'
    if (!success.value) error.value = 'panel_issuance_failed'
    try {
      const current = await api.panelHostname()
      if (live) status.value = current
    } catch {
      if (live && success.value) { success.value = false; error.value = 'operation_status_unavailable' }
    }
  }, () => { pending.value = false; error.value = 'operation_status_unavailable' })
}

async function load() {
  if (busy.value || pending.value) return
  busy.value = true
  error.value = ''
  try {
    const operations = await api.operations()
    if (!live) return
    const active = operations.find((item) => item.kind === 'panel.hostname.issue' && operationPending(item))
    if (active) { track(active.id); return }
    const current = await api.panelHostname()
    if (live) { status.value = current; form.hostname = current.hostname ?? '' }
  } catch (failure) {
    if (live) error.value = failure instanceof ApiError ? failure.code : 'request_failed'
  } finally { if (live) busy.value = false }
}

async function submit() {
  if (busy.value || pending.value || !status.value || status.value.recoveryRequired || !form.acceptTerms || !form.confirmOriginChange) return
  busy.value = true
  error.value = ''
  success.value = false
  try {
    const result = await api.issuePanelHostname({ ...form })
    if (live) { form.acceptTerms = false; form.confirmOriginChange = false; track(result.operationId) }
  } catch (failure) {
    if (live) error.value = failure instanceof ApiError ? failure.code : 'request_failed'
  } finally { if (live) busy.value = false }
}

onMounted(() => { void load() })
onBeforeUnmount(() => { live = false; stop?.() })
</script>

<template>
  <section class="panel panel-hostname-settings">
    <div><p class="eyebrow">{{ t('panelHostname.eyebrow') }}</p><h2>{{ t('panelHostname.title') }}</h2><p>{{ t('panelHostname.body') }}</p></div>
    <p class="form-hint">{{ t('panelHostname.prerequisites') }}</p>
    <p class="form-hint">{{ t('panelHostname.fallback') }}</p>
    <p v-if="error" class="inline-feedback error-feedback" role="alert">{{ message }}</p>
    <p v-if="pending" class="inline-feedback" role="status">{{ t('panelHostname.pending') }}</p>
    <p v-if="success" class="inline-feedback" role="status">{{ t('panelHostname.success') }}</p>
    <p v-if="status?.recoveryRequired" class="inline-feedback error-feedback" role="alert">{{ t('panelHostname.recovery') }}</p>
    <dl v-if="status" class="detail-list">
      <div><dt>{{ t('panelHostname.endpoint') }}</dt><dd><a v-if="endpoint" :href="endpoint" target="_blank" rel="noopener noreferrer">{{ endpoint }}</a><span v-else>{{ t('panelHostname.disabled') }}</span></dd></div>
      <div><dt>{{ t('panelHostname.renewal') }}</dt><dd>{{ status.autoRenew ? t('states.active') : t('states.disabled') }}</dd></div>
    </dl>
    <form class="management-form" @submit.prevent="submit">
      <fieldset :disabled="busy || pending || !status || status.recoveryRequired">
        <label><span>{{ t('panelHostname.hostname') }}</span><input v-model="form.hostname" required type="text" maxlength="253" autocomplete="off" :placeholder="t('panelHostname.hostnameExample')"></label>
        <label><span>{{ t('acme.contactEmail') }}</span><input v-model="form.email" required type="email" maxlength="254" autocomplete="email"></label>
        <label class="check-field"><input v-model="form.acceptTerms" required type="checkbox"><span>{{ t('acme.acceptTerms') }} <a href="https://letsencrypt.org/repository/" target="_blank" rel="noopener noreferrer">{{ t('panelHostname.terms') }}</a></span></label>
        <label class="check-field"><input v-model="form.confirmOriginChange" required type="checkbox"><span>{{ t('panelHostname.confirm') }}</span></label>
        <button class="primary-action" type="submit" :disabled="!form.acceptTerms || !form.confirmOriginChange">{{ t('panelHostname.issue') }}</button>
      </fieldset>
    </form>
    <button class="secondary-action" type="button" :disabled="busy || pending" @click="load">{{ t('common.refresh') }}</button>
  </section>
</template>
