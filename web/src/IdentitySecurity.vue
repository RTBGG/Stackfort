<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, ApiError, type TOTPStatus } from './api'
import { formatDateTime, formatNumber } from './formatting'
import { isSupportedLocale } from './i18n'

const emit = defineEmits<{ changed: [recoveryCodes: string[]]; logout: [] }>()
const { t, te, locale } = useI18n()
const activeLocale = computed(() => isSupportedLocale(locale.value) ? locale.value : 'en')
const status = ref<TOTPStatus | null>(null)
const busy = ref(false)
const error = ref('')
const currentFactor = ref('')
const code = ref('')
const removeConfirmed = ref(false)
const enrollment = ref<{ challengeId: string; secret: string; expiresAt: string } | null>(null)
const codeInput = ref<HTMLInputElement | null>(null)
const setupButton = ref<HTMLButtonElement | null>(null)
let live = true
let generation = 0
let expiryTimer: ReturnType<typeof setTimeout> | null = null
const errorMessage = computed(() => {
  if (error.value === 'verification_failed') return t('identitySecurity.verificationFailed')
  if (error.value === 'setup_expired') return t('identitySecurity.expired')
  return te(`errors.${error.value}`) ? t(`errors.${error.value}`) : t('identitySecurity.requestFailed')
})

function discardSetup() {
  if (expiryTimer) clearTimeout(expiryTimer)
  expiryTimer = null
  if (enrollment.value) enrollment.value.secret = ''
  enrollment.value = null
  code.value = ''
  currentFactor.value = ''
}

async function load() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  const requestGeneration = ++generation
  try {
    const result = await api.totpStatus()
    if (live && generation === requestGeneration) status.value = result
  } catch (failure) {
    if (live && generation === requestGeneration) error.value = failure instanceof ApiError ? failure.code : 'request_failed'
  } finally {
    if (live && generation === requestGeneration) busy.value = false
  }
}

async function begin() {
  if (busy.value || !status.value || (status.value.enabled && !currentFactor.value)) return
  busy.value = true
  error.value = ''
  const requestGeneration = ++generation
  try {
    const result = await api.beginTOTP(currentFactor.value)
    if (!live || generation !== requestGeneration) return
    discardSetup()
    enrollment.value = { challengeId: result.challengeId, secret: result.secret, expiresAt: result.expiresAt }
    const expiresIn = Date.parse(result.expiresAt) - Date.now()
    if (!Number.isFinite(expiresIn) || expiresIn <= 0 || expiresIn > 11 * 60_000) {
      discardSetup()
      error.value = 'setup_expired'
      return
    }
    expiryTimer = setTimeout(() => {
      discardSetup()
      error.value = 'setup_expired'
    }, expiresIn)
    busy.value = false
    await nextTick()
    if (live && generation === requestGeneration) codeInput.value?.focus()
  } catch (failure) {
    if (live && generation === requestGeneration) error.value = failure instanceof ApiError ? failure.code : 'request_failed'
  } finally {
    currentFactor.value = ''
    if (live && generation === requestGeneration) busy.value = false
  }
}

async function confirm() {
  if (busy.value || !enrollment.value || !/^\d{6}$/.test(code.value)) return
  if (Date.parse(enrollment.value.expiresAt) <= Date.now()) {
    discardSetup()
    error.value = 'setup_expired'
    return
  }
  busy.value = true
  error.value = ''
  const requestGeneration = ++generation
  try {
    const result = await api.confirmTOTP(enrollment.value.challengeId, code.value)
    if (!live || generation !== requestGeneration) return
    discardSetup()
    emit('changed', result.recoveryCodes)
  } catch (failure) {
    if (live && generation === requestGeneration) error.value = failure instanceof ApiError ? failure.code : 'request_failed'
  } finally {
    code.value = ''
    if (live && generation === requestGeneration) busy.value = false
  }
}

async function remove() {
  if (busy.value || !status.value?.enabled || !removeConfirmed.value || !currentFactor.value) return
  busy.value = true
  error.value = ''
  const requestGeneration = ++generation
  try {
    await api.disableTOTP(currentFactor.value)
    if (live && generation === requestGeneration) {
      discardSetup()
      emit('changed', [])
    }
  } catch (failure) {
    if (live && generation === requestGeneration) error.value = failure instanceof ApiError ? failure.code : 'request_failed'
  } finally {
    currentFactor.value = ''
    removeConfirmed.value = false
    if (live && generation === requestGeneration) busy.value = false
  }
}

async function cancel() {
  if (busy.value) return
  generation += 1
  discardSetup()
  error.value = ''
  await nextTick()
  setupButton.value?.focus()
}

onMounted(load)
onBeforeUnmount(() => { live = false; generation += 1; discardSetup() })
</script>

<template>
  <section class="panel management-form identity-security" :aria-busy="busy">
    <h2>{{ t('identitySecurity.title') }}</h2>
    <p>{{ t('identitySecurity.body') }}</p>
    <p v-if="error" class="inline-feedback error" role="alert">{{ errorMessage }}</p>
    <button v-if="error === 'recent_authentication_required' || error === 'authentication_required'" class="secondary-action" type="button" :disabled="busy" @click="emit('logout')">{{ t('identitySecurity.signInAgain') }}</button>
    <template v-if="status">
      <p role="status">{{ t(status.enabled ? 'identitySecurity.enabled' : 'identitySecurity.disabled') }}</p>
      <p v-if="status.enabled">{{ t('identitySecurity.remaining', { count: formatNumber(status.recoveryCodesRemaining, activeLocale) }) }}</p>
      <form v-if="enrollment" class="management-form" @submit.prevent="confirm">
        <p>{{ t('identitySecurity.setupHint') }}</p>
        <label><span>{{ t('identitySecurity.secret') }}</span><input :value="enrollment.secret" readonly autocomplete="off" spellcheck="false"></label>
        <p>{{ t('identitySecurity.expires', { date: formatDateTime(enrollment.expiresAt, activeLocale) }) }}</p>
        <label><span>{{ t('identitySecurity.code') }}</span><input ref="codeInput" v-model="code" required inputmode="numeric" autocomplete="one-time-code" pattern="[0-9]{6}" maxlength="6" :disabled="busy"></label>
        <p class="form-hint">{{ t('identitySecurity.stay') }}</p>
        <div class="card-actions"><button class="primary-action" type="submit" :disabled="busy || !/^\d{6}$/.test(code)">{{ t('identitySecurity.confirm') }}</button><button class="secondary-action" type="button" :disabled="busy" @click="cancel">{{ t('identitySecurity.cancel') }}</button></div>
      </form>
      <form v-else class="management-form" @submit.prevent="begin">
        <template v-if="status.enabled">
          <label><span>{{ t('identitySecurity.currentFactor') }}</span><input v-model="currentFactor" type="password" required autocomplete="off" maxlength="128" :disabled="busy"></label>
          <p>{{ t('identitySecurity.replacementHint') }}</p>
        </template>
        <button ref="setupButton" class="primary-action" type="submit" :disabled="busy || (status.enabled && !currentFactor)">{{ t(status.enabled ? 'identitySecurity.replace' : 'identitySecurity.enable') }}</button>
        <template v-if="status.enabled">
          <label class="check-field"><input v-model="removeConfirmed" type="checkbox" :disabled="busy"><span>{{ t('identitySecurity.removeConfirm') }}</span></label>
          <button class="danger-action" type="button" :disabled="busy || !removeConfirmed || !currentFactor" @click="remove">{{ t('identitySecurity.remove') }}</button>
        </template>
      </form>
    </template>
    <button v-else-if="!busy" class="secondary-action" type="button" @click="load">{{ t('common.retry') }}</button>
  </section>
</template>
