<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, ApiError, type BootstrapStatus, type Session } from './api'
import { formatDateTime } from './formatting'
import { isSupportedLocale, type SupportedLocale } from './i18n'

const props = defineProps<{
  initialMode: 'bootstrap' | 'login'
  bootstrapStatus: BootstrapStatus | null
}>()

const emit = defineEmits<{
  authenticated: [session: Session]
}>()

const { locale, t } = useI18n()
const mode = ref<'bootstrap' | 'login' | 'mfa'>(props.initialMode)
const busy = ref(false)
const errorCode = ref('')
const successCode = ref('')
const email = ref('')
const displayName = ref('')
const password = ref('')
const passwordConfirmation = ref('')
const token = ref('')
const mfaCode = ref('')

watch(() => props.initialMode, (value) => {
  mode.value = value
})

const activeLocale = computed<SupportedLocale>(() => (
  isSupportedLocale(locale.value) ? locale.value : 'en'
))
const bootstrapExpiry = computed(() => {
  if (!props.bootstrapStatus?.expiresAt) return ''
  try {
    return formatDateTime(props.bootstrapStatus.expiresAt, activeLocale.value)
  } catch {
    return t('common.notAvailable')
  }
})
const feedback = computed(() => {
  if (errorCode.value) return t(`errors.${errorCode.value}`)
  if (successCode.value) return t(`auth.${successCode.value}`)
  return ''
})

function changeLocale(event: Event) {
  const value = (event.target as HTMLSelectElement).value
  if (isSupportedLocale(value)) locale.value = value
}

function normalizeError(error: unknown) {
  errorCode.value = error instanceof ApiError ? error.code : 'request_failed'
}

async function submitBootstrap() {
  errorCode.value = ''
  successCode.value = ''
  if (password.value !== passwordConfirmation.value) {
    errorCode.value = 'password_mismatch'
    return
  }
  busy.value = true
  try {
    await api.bootstrapAdministrator({
      token: token.value,
      email: email.value,
      displayName: displayName.value,
      password: password.value,
      locale: activeLocale.value,
    })
    password.value = ''
    passwordConfirmation.value = ''
    token.value = ''
    mode.value = 'login'
    successCode.value = 'bootstrapComplete'
  } catch (error) {
    normalizeError(error)
  } finally {
    busy.value = false
  }
}

async function submitLogin() {
  errorCode.value = ''
  successCode.value = ''
  busy.value = true
  try {
    const result = await api.login(email.value, password.value)
    password.value = ''
    if (result.kind === 'mfa') {
      mode.value = 'mfa'
      return
    }
    emit('authenticated', result.session)
  } catch (error) {
    normalizeError(error)
  } finally {
    busy.value = false
  }
}

async function submitMFA() {
  errorCode.value = ''
  busy.value = true
  try {
    emit('authenticated', await api.completeMFA(mfaCode.value))
  } catch (error) {
    normalizeError(error)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="auth-screen">
    <section class="auth-card" :aria-labelledby="`${mode}-title`">
      <header class="auth-brand">
        <span class="brand-mark" aria-hidden="true">
          <svg viewBox="0 0 40 40"><path d="M20 3 35 11v18L20 37 5 29V11L20 3Z"/><path class="brand-cut" d="m12 14 8-4 8 4-8 4-8-4Zm0 7 8 4 8-4v7l-8 5-8-5v-7Z"/></svg>
        </span>
        <span><strong>{{ t('brand.name') }}</strong><small>{{ t('brand.tagline') }}</small></span>
      </header>

      <label class="auth-language" for="auth-language">
        <span>{{ t('topbar.language') }}</span>
        <select id="auth-language" :value="locale" @change="changeLocale">
          <option value="en">{{ t('localeNames.en') }}</option>
          <option value="de">{{ t('localeNames.de') }}</option>
        </select>
      </label>

      <form v-if="mode === 'bootstrap'" class="auth-form" @submit.prevent="submitBootstrap">
        <div>
          <p class="eyebrow">{{ t('auth.bootstrapEyebrow') }}</p>
          <h1 id="bootstrap-title">{{ t('auth.bootstrapTitle') }}</h1>
          <p>{{ t('auth.bootstrapBody') }}</p>
        </div>
        <p v-if="!bootstrapStatus?.capabilityActive" class="form-alert" role="alert">
          {{ t('auth.bootstrapUnavailable') }}
        </p>
        <p v-else-if="bootstrapExpiry" class="form-hint">
          {{ t('auth.bootstrapExpires', { date: bootstrapExpiry }) }}
        </p>
        <label><span>{{ t('auth.token') }}</span><input v-model="token" required autocomplete="off" spellcheck="false"></label>
        <label><span>{{ t('auth.displayName') }}</span><input v-model="displayName" required autocomplete="name"></label>
        <label><span>{{ t('auth.email') }}</span><input v-model="email" required type="email" autocomplete="email"></label>
        <label><span>{{ t('auth.password') }}</span><input v-model="password" required type="password" minlength="15" autocomplete="new-password"></label>
        <label><span>{{ t('auth.confirmPassword') }}</span><input v-model="passwordConfirmation" required type="password" minlength="15" autocomplete="new-password"></label>
        <button class="primary-action" type="submit" :disabled="busy || !bootstrapStatus?.capabilityActive">
          {{ busy ? t('common.saving') : t('auth.createAdministrator') }}
        </button>
      </form>

      <form v-else-if="mode === 'login'" class="auth-form" @submit.prevent="submitLogin">
        <div>
          <p class="eyebrow">{{ t('auth.loginEyebrow') }}</p>
          <h1 id="login-title">{{ t('auth.loginTitle') }}</h1>
          <p>{{ t('auth.loginBody') }}</p>
        </div>
        <label><span>{{ t('auth.email') }}</span><input v-model="email" required type="email" autocomplete="username"></label>
        <label><span>{{ t('auth.password') }}</span><input v-model="password" required type="password" autocomplete="current-password"></label>
        <button class="primary-action" type="submit" :disabled="busy">
          {{ busy ? t('auth.signingIn') : t('auth.signIn') }}
        </button>
      </form>

      <form v-else class="auth-form" @submit.prevent="submitMFA">
        <div>
          <p class="eyebrow">{{ t('auth.mfaEyebrow') }}</p>
          <h1 id="mfa-title">{{ t('auth.mfaTitle') }}</h1>
          <p>{{ t('auth.mfaBody') }}</p>
        </div>
        <label><span>{{ t('auth.mfaCode') }}</span><input v-model="mfaCode" required inputmode="numeric" autocomplete="one-time-code" pattern="[0-9]{6}"></label>
        <button class="primary-action" type="submit" :disabled="busy">
          {{ busy ? t('common.checking') : t('auth.verify') }}
        </button>
        <button class="text-action" type="button" @click="mode = 'login'">{{ t('auth.backToLogin') }}</button>
      </form>

      <p v-if="feedback" class="form-feedback" :class="{ error: errorCode }" :role="errorCode ? 'alert' : 'status'" aria-live="polite">
        {{ feedback }}
      </p>
    </section>
  </main>
</template>
