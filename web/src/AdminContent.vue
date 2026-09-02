<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AdminPageKey } from './admin'
import type {
  ACMEAccount,
  AccountPHPStatus,
  AuditEvent,
  BuildInfo,
  Domain,
  DomainWAFException,
  DomainTargetInput,
  HostCapabilities,
  HostingAccount,
  HostingPackage,
  Operation,
  PackageLimits,
  Session,
  UpdateStatus,
} from './api'
import { formatBytes, formatDateTime, formatNumber, formatPercent } from './formatting'
import { isSupportedLocale, type SupportedLocale } from './i18n'

const props = defineProps<{
  page: AdminPageKey
  session: Session
  health: 'loading' | 'healthy' | 'unavailable'
  build: BuildInfo | null
  packages: HostingPackage[]
  accounts: HostingAccount[]
  domains: Domain[]
  wafExceptions: DomainWAFException[]
  operations: Operation[]
  auditEvents: AuditEvent[]
  capabilities: HostCapabilities | null
  updateStatus: UpdateStatus | null
  phpStatus: AccountPHPStatus | null
  acmeAccounts: ACMEAccount[]
  loading: boolean
  actionBusy: boolean
  errorCode: string
  noticeCode: string
}>()

const emit = defineEmits<{
  refresh: []
  createPackage: [input: { name: string; slug: string; limits: PackageLimits }]
  createAccount: [input: { name: string; slug: string; packageId: string; ownerIdentityId?: string }]
  registerACMEAccount: [input: {
    environment: 'letsencrypt-production'; contactEmail: string; termsAccepted: boolean
  }]
  selectAccount: [accountId: string]
  createDomain: [input: {
    accountId: string
    name: string
    canonicalMode: Domain['canonicalMode']
    target: DomainTargetInput
    disableTls: boolean
    tlsMode?: 'acme'
    wafMode: Domain['waf']['mode']
    cachePreset: NonNullable<Domain['cache']>['preset']
  }]
  domainAction: [input: { accountId: string; domainId: string; action: 'suspend' | 'resume' | 'remove' }]
  loadWAFExceptions: [input: { accountId: string; domainId: string }]
  createWAFException: [input: {
    accountId: string; domainId: string; ruleId: number; requestPath?: string; parameter?: string; expiresAt: string
  }]
  removeWAFException: [input: { accountId: string; domainId: string; exceptionId: string }]
  updatePolicy: [input: { channel: UpdateStatus['channel']; automaticChecks: boolean }]
  checkUpdates: []
  logout: []
}>()

const { locale, t } = useI18n()
const selectedAccountId = ref('')
const packageForm = reactive({
  name: '', slug: '', maxDomains: 10, maxDatabases: 5, maxDatabaseUsers: 5, maxScheduledJobs: 10, cpuQuotaPercent: 100,
  memoryGiB: 2, storageGiB: 20, backupStorageGiB: 20, bandwidthGiB: 100,
  allowedPhpVersions: [] as string[],
  wafExceptions: false,
})
const accountForm = reactive({ name: '', slug: '', packageId: '', ownerIdentityId: '' })
const acmeForm = reactive({ contactEmail: props.session.identity.email, termsAccepted: false })
const domainForm = reactive({
  name: '', canonicalMode: 'serve_both' as Domain['canonicalMode'],
  targetType: 'static' as 'static' | 'php', phpVersion: '',
  rootMode: 'default' as 'default' | 'custom', documentRoot: '', tls: true,
  wafMode: 'off' as Domain['waf']['mode'],
  cachePreset: 'disabled' as NonNullable<Domain['cache']>['preset'],
})
const selectedWAFDomainId = ref('')
const wafExceptionForm = reactive({
  ruleId: 941100,
  requestPath: '',
  parameter: '',
  expiresAt: defaultExceptionExpiry(),
})
const updateForm = reactive({
  channel: 'stable' as UpdateStatus['channel'],
  automaticChecks: true,
})

const activeLocale = computed<SupportedLocale>(() => (
  isSupportedLocale(locale.value) ? locale.value : 'en'
))
const activeOperations = computed(() => props.operations.filter((item) => (
  item.status === 'pending' || item.status === 'running' || item.status === 'cancelling'
)))
const selectedAccount = computed(() => (
  props.accounts.find((item) => item.id === selectedAccountId.value) ?? null
))
const selectedPackage = computed(() => props.packages.find((item) => item.id === selectedAccount.value?.packageId) ?? null)
const selectedWAFDomain = computed(() => props.domains.find((item) => item.id === selectedWAFDomainId.value) ?? null)
const canUseWAFExceptions = computed(() => selectedPackage.value?.limits.features.wafExceptions === true)
const managedPHPVersions = computed(() => props.capabilities?.managedPhpVersions ?? [])
const availablePHPVersions = computed(() => props.phpStatus?.availableVersions ?? [])
const canCreateDomain = computed(() => Boolean(selectedAccount.value?.hostReady) && (
  domainForm.targetType === 'static' || availablePHPVersions.value.includes(domainForm.phpVersion)
))
const healthyServices = computed(() => props.capabilities?.services.filter((item) => (
  item.activeState === 'active'
)).length ?? 0)
const platformLabel = computed(() => {
  if (!props.capabilities) return t('common.notAvailable')
  return `${props.capabilities.platform.distributionId} ${props.capabilities.platform.versionId}`
})
const productionACMEAccount = computed(() => props.acmeAccounts.find((item) => (
  item.environment === 'letsencrypt-production'
)) ?? null)
const acmeRegistrationPending = computed(() => props.operations.some((item) => (
  item.kind === 'acme.account.register' && (
    item.status === 'pending' || item.status === 'running' || item.status === 'cancelling'
  )
)))
const updatePolicyChanged = computed(() => Boolean(props.updateStatus) && (
  updateForm.channel !== props.updateStatus?.channel
  || updateForm.automaticChecks !== props.updateStatus?.automaticChecks
))

watch(() => props.packages, (packages) => {
  if (!accountForm.packageId && packages[0]) accountForm.packageId = packages[0].id
}, { immediate: true })

watch(managedPHPVersions, (versions) => {
  packageForm.allowedPhpVersions = packageForm.allowedPhpVersions.filter((version) => versions.includes(version))
})

watch(availablePHPVersions, (versions) => {
  if (domainForm.targetType === 'php' && !versions.includes(domainForm.phpVersion)) {
    domainForm.phpVersion = versions[0] ?? ''
  }
}, { immediate: true })

watch([() => props.accounts, () => props.page], ([accounts, page]) => {
  if (page !== 'domains') return
  if (!accounts.some((account) => account.id === selectedAccountId.value)) {
    selectedAccountId.value = accounts[0]?.id ?? ''
  }
  if (selectedAccountId.value) emit('selectAccount', selectedAccountId.value)
}, { immediate: true })

watch(selectedAccountId, () => { selectedWAFDomainId.value = '' })

watch(() => props.updateStatus, (status) => {
  if (!status) return
  updateForm.channel = status.channel
  updateForm.automaticChecks = status.automaticChecks
}, { immediate: true })

function defaultExceptionExpiry(): string {
  const date = new Date(Date.now() + 60 * 60 * 1000)
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60_000)
  return local.toISOString().slice(0, 16)
}

function displayDate(value?: string): string {
  if (!value) return t('common.notAvailable')
  try {
    return formatDateTime(value, activeLocale.value)
  } catch {
    return t('common.notAvailable')
  }
}

function submitUpdatePolicy() {
  emit('updatePolicy', {
    channel: updateForm.channel,
    automaticChecks: updateForm.automaticChecks,
  })
}

function optionalBytes(gibibytes: number): number | undefined {
  return Number.isFinite(gibibytes) && gibibytes > 0 ? Math.round(gibibytes * (1024 ** 3)) : undefined
}

function domainWAFMode(domain: Domain): Domain['waf']['mode'] {
  return domain.waf?.mode ?? 'off'
}

function domainCachePreset(domain: Domain): NonNullable<Domain['cache']>['preset'] {
  return domain.cache?.preset ?? 'disabled'
}

function submitPackage() {
  emit('createPackage', {
    name: packageForm.name,
    slug: packageForm.slug,
    limits: {
      maxDomains: packageForm.maxDomains,
      maxDatabases: packageForm.maxDatabases,
      maxDatabaseUsers: packageForm.maxDatabaseUsers,
      maxScheduledJobs: packageForm.maxScheduledJobs,
      maxOciApplications: 0,
      cpuQuotaPercent: packageForm.cpuQuotaPercent || undefined,
      memoryBytes: optionalBytes(packageForm.memoryGiB),
      storageBytes: optionalBytes(packageForm.storageGiB),
      backupStorageBytes: optionalBytes(packageForm.backupStorageGiB),
      monthlyCombinedBytes: optionalBytes(packageForm.bandwidthGiB),
      allowedPhpVersions: [...packageForm.allowedPhpVersions],
      features: {
        ociApplications: false,
        customRedirects: true,
        wafExceptions: packageForm.wafExceptions,
        scheduledBackups: false,
      },
    },
  })
}

function submitAccount() {
  emit('createAccount', {
    name: accountForm.name,
    slug: accountForm.slug,
    packageId: accountForm.packageId,
    ownerIdentityId: accountForm.ownerIdentityId || undefined,
  })
}

function submitACMEAccount() {
  if (productionACMEAccount.value || !acmeForm.termsAccepted) return
  emit('registerACMEAccount', {
    environment: 'letsencrypt-production',
    contactEmail: acmeForm.contactEmail,
    termsAccepted: acmeForm.termsAccepted,
  })
}

function changeAccount(event: Event) {
  selectedAccountId.value = (event.target as HTMLSelectElement).value
  if (selectedAccountId.value) emit('selectAccount', selectedAccountId.value)
}

function submitDomain() {
	if (!selectedAccountId.value || !canCreateDomain.value) return
	const target: DomainTargetInput = {
		type: domainForm.targetType,
		rootMode: domainForm.rootMode,
	}
	if (domainForm.rootMode === 'custom') target.documentRoot = domainForm.documentRoot
	if (domainForm.targetType === 'php') target.phpVersion = domainForm.phpVersion
	const cachePreset = domainForm.targetType === 'php' ? domainForm.cachePreset : 'disabled'
	emit('createDomain', {
    accountId: selectedAccountId.value,
    name: domainForm.name,
    canonicalMode: domainForm.canonicalMode,
		target,
    disableTls: !domainForm.tls,
    tlsMode: domainForm.tls ? 'acme' : undefined,
    wafMode: domainForm.wafMode,
    cachePreset,
  })
}

function runDomainAction(domain: Domain, action: 'suspend' | 'resume' | 'remove') {
  if (!selectedAccountId.value) return
  if (action === 'remove' && !window.confirm(t('domains.confirmRemove', { domain: domain.name.display }))) return
  emit('domainAction', { accountId: selectedAccountId.value, domainId: domain.id, action })
}

function manageWAFExceptions(domain: Domain) {
  if (!selectedAccountId.value) return
  selectedWAFDomainId.value = domain.id
  emit('loadWAFExceptions', { accountId: selectedAccountId.value, domainId: domain.id })
}

function submitWAFException() {
  if (!selectedAccountId.value || !selectedWAFDomain.value ||
      (!wafExceptionForm.requestPath && !wafExceptionForm.parameter)) return
  const expiry = new Date(wafExceptionForm.expiresAt)
  if (!Number.isFinite(expiry.getTime())) return
  emit('createWAFException', {
    accountId: selectedAccountId.value,
    domainId: selectedWAFDomain.value.id,
    ruleId: wafExceptionForm.ruleId,
    requestPath: wafExceptionForm.requestPath || undefined,
    parameter: wafExceptionForm.parameter || undefined,
    expiresAt: expiry.toISOString(),
  })
}

function removeWAFException(exception: DomainWAFException) {
  if (!selectedAccountId.value || !selectedWAFDomain.value ||
      !window.confirm(t('wafExceptions.confirmRemove'))) return
  emit('removeWAFException', {
    accountId: selectedAccountId.value,
    domainId: selectedWAFDomain.value.id,
    exceptionId: exception.id,
  })
}
</script>

<template>
  <div class="admin-content">
    <div class="content-toolbar">
      <p v-if="noticeCode" class="inline-feedback success" role="status" aria-live="polite">{{ t(`notices.${noticeCode}`) }}</p>
      <p v-if="errorCode" class="inline-feedback error" role="alert">{{ t(`errors.${errorCode}`) }}</p>
      <button class="secondary-action" type="button" :disabled="loading" @click="emit('refresh')">
        {{ loading ? t('common.loading') : t('common.refresh') }}
      </button>
    </div>

    <template v-if="page === 'overview'">
      <section class="metric-grid" :aria-label="t('dashboard.summary')">
        <article class="metric-card"><span>{{ t('dashboard.packages') }}</span><strong>{{ formatNumber(packages.length, activeLocale) }}</strong><small>{{ t('dashboard.packageHint') }}</small></article>
        <article class="metric-card"><span>{{ t('dashboard.accounts') }}</span><strong>{{ formatNumber(accounts.length, activeLocale) }}</strong><small>{{ t('dashboard.accountHint') }}</small></article>
        <article class="metric-card"><span>{{ t('dashboard.activeOperations') }}</span><strong>{{ formatNumber(activeOperations.length, activeLocale) }}</strong><small>{{ t('dashboard.operationHint') }}</small></article>
        <article class="metric-card"><span>{{ t('dashboard.services') }}</span><strong>{{ formatNumber(healthyServices, activeLocale) }}</strong><small>{{ platformLabel }}</small></article>
      </section>
      <section class="dashboard-grid admin-dashboard">
        <article class="panel data-panel">
          <div class="panel-heading"><h2>{{ t('operations.recent') }}</h2></div>
          <div v-if="operations.length === 0" class="empty-state"><strong>{{ t('operations.empty') }}</strong><p>{{ t('operations.emptyBody') }}</p></div>
          <ul v-else class="compact-list">
            <li v-for="operation in operations.slice(0, 5)" :key="operation.id">
              <span><strong>{{ operation.kind }}</strong><small>{{ displayDate(operation.updatedAt) }}</small></span>
              <span class="state-badge" :data-state="operation.status">{{ t(`states.${operation.status}`) }}</span>
            </li>
          </ul>
        </article>
        <article class="panel data-panel">
          <div class="panel-heading"><h2>{{ t('build.title') }}</h2></div>
          <dl class="detail-list">
            <div><dt>{{ t('build.version') }}</dt><dd>{{ build?.version ?? t('common.notAvailable') }}</dd></div>
            <div><dt>{{ t('build.commit') }}</dt><dd><code>{{ build?.commit ?? '—' }}</code></dd></div>
            <div><dt>{{ t('build.date') }}</dt><dd>{{ build?.buildDate === 'unknown' ? t('common.notAvailable') : displayDate(build?.buildDate) }}</dd></div>
            <div><dt>{{ t('status.api') }}</dt><dd><span class="state-badge" :data-state="health">{{ t(`states.${health}`) }}</span></dd></div>
          </dl>
        </article>
      </section>
    </template>

    <section v-else-if="page === 'packages'" class="management-grid">
      <form class="panel management-form" @submit.prevent="submitPackage">
        <div class="panel-heading"><h2>{{ t('packages.create') }}</h2></div>
        <label><span>{{ t('common.name') }}</span><input v-model="packageForm.name" required maxlength="120"></label>
        <label><span>{{ t('common.slug') }}</span><input v-model="packageForm.slug" required pattern="[a-z0-9]+(?:-[a-z0-9]+)*" spellcheck="false"></label>
        <div class="form-columns">
          <label><span>{{ t('packages.maxDomains') }}</span><input v-model.number="packageForm.maxDomains" required type="number" min="0"></label>
          <label><span>{{ t('packages.maxDatabases') }}</span><input v-model.number="packageForm.maxDatabases" required type="number" min="0"></label>
          <label><span>{{ t('packages.maxDatabaseUsers') }}</span><input v-model.number="packageForm.maxDatabaseUsers" required type="number" min="0"></label>
          <label><span>{{ t('packages.maxScheduledJobs') }}</span><input v-model.number="packageForm.maxScheduledJobs" required type="number" min="0" max="1000"></label>
          <label><span>{{ t('packages.cpu') }}</span><input v-model.number="packageForm.cpuQuotaPercent" type="number" min="1"></label>
          <label><span>{{ t('packages.memory') }}</span><input v-model.number="packageForm.memoryGiB" type="number" min="1"></label>
          <label><span>{{ t('packages.storage') }}</span><input v-model.number="packageForm.storageGiB" type="number" min="1"></label>
          <label><span>{{ t('packages.backupStorage') }}</span><input v-model.number="packageForm.backupStorageGiB" type="number" min="1" max="1024"></label>
          <label><span>{{ t('packages.bandwidth') }}</span><input v-model.number="packageForm.bandwidthGiB" type="number" min="1"></label>
        </div>
        <fieldset class="choice-fieldset">
          <legend>{{ t('packages.phpVersions') }}</legend>
          <p>{{ managedPHPVersions.length > 0 ? t('packages.phpVersionsHint') : t('packages.noManagedPHP') }}</p>
          <label v-for="version in managedPHPVersions" :key="version" class="check-field">
            <input v-model="packageForm.allowedPhpVersions" type="checkbox" :value="version">
            <span>{{ t('php.version', { version }) }}</span>
          </label>
        </fieldset>
        <label class="check-field"><input v-model="packageForm.wafExceptions" type="checkbox"><span>{{ t('packages.wafExceptions') }}</span></label>
        <button class="primary-action" type="submit" :disabled="actionBusy">{{ t('packages.createAction') }}</button>
      </form>
      <div class="resource-list">
        <article v-for="item in packages" :key="item.id" class="panel resource-card">
          <header><div><h2>{{ item.name }}</h2><code>{{ item.slug }}</code></div><span class="state-badge" :data-state="item.hostReady ? item.status : 'pending'">{{ item.hostReady ? t(`states.${item.status}`) : t('states.provisioning') }}</span></header>
          <dl class="detail-list compact">
            <div><dt>{{ t('packages.domains') }}</dt><dd>{{ formatNumber(item.limits.maxDomains, activeLocale) }}</dd></div>
            <div><dt>{{ t('packages.databases') }}</dt><dd>{{ formatNumber(item.limits.maxDatabases, activeLocale) }}</dd></div>
            <div><dt>{{ t('packages.databaseUsers') }}</dt><dd>{{ formatNumber(item.limits.maxDatabaseUsers, activeLocale) }}</dd></div>
            <div><dt>{{ t('packages.scheduledJobs') }}</dt><dd>{{ formatNumber(item.limits.maxScheduledJobs, activeLocale) }}</dd></div>
            <div><dt>{{ t('packages.cpuShort') }}</dt><dd>{{ item.limits.cpuQuotaPercent ? formatPercent(item.limits.cpuQuotaPercent / 100, activeLocale) : t('common.unlimited') }}</dd></div>
            <div><dt>{{ t('packages.memoryShort') }}</dt><dd>{{ item.limits.memoryBytes ? formatBytes(item.limits.memoryBytes, activeLocale) : t('common.unlimited') }}</dd></div>
            <div><dt>{{ t('packages.storageShort') }}</dt><dd>{{ item.limits.storageBytes ? formatBytes(item.limits.storageBytes, activeLocale) : t('common.unlimited') }}</dd></div>
            <div><dt>{{ t('packages.backupStorageShort') }}</dt><dd>{{ item.limits.backupStorageBytes ? formatBytes(item.limits.backupStorageBytes, activeLocale) : formatBytes(20 * (1024 ** 3), activeLocale) }}</dd></div>
            <div><dt>{{ t('packages.phpVersions') }}</dt><dd>{{ item.limits.allowedPhpVersions.length > 0 ? item.limits.allowedPhpVersions.join(', ') : t('packages.staticOnly') }}</dd></div>
          </dl>
        </article>
        <div v-if="packages.length === 0" class="panel empty-state"><strong>{{ t('packages.empty') }}</strong><p>{{ t('packages.emptyBody') }}</p></div>
      </div>
    </section>

    <section v-else-if="page === 'accounts'" class="management-grid">
      <form class="panel management-form" @submit.prevent="submitAccount">
        <div class="panel-heading"><h2>{{ t('accounts.create') }}</h2></div>
        <label><span>{{ t('common.name') }}</span><input v-model="accountForm.name" required maxlength="120"></label>
        <label><span>{{ t('common.slug') }}</span><input v-model="accountForm.slug" required pattern="[a-z0-9]+(?:-[a-z0-9]+)*" spellcheck="false"></label>
        <label><span>{{ t('accounts.package') }}</span><select v-model="accountForm.packageId" required><option v-for="item in packages" :key="item.id" :value="item.id">{{ item.name }}</option></select></label>
        <label><span>{{ t('accounts.ownerIdentity') }}</span><input v-model="accountForm.ownerIdentityId" :placeholder="t('accounts.currentAdministrator')" spellcheck="false"><small>{{ t('accounts.ownerHint') }}</small></label>
        <button class="primary-action" type="submit" :disabled="actionBusy || packages.length === 0">{{ t('accounts.createAction') }}</button>
      </form>
      <div class="resource-list">
        <article v-for="item in accounts" :key="item.id" class="panel resource-card">
          <header><div><h2>{{ item.name }}</h2><code>{{ item.slug }}</code></div><span class="state-badge" :data-state="item.status">{{ t(`states.${item.status}`) }}</span></header>
          <p>{{ t('accounts.assignedPackage', { package: item.packageName ?? t('common.notAvailable'), revision: item.packageRevision ?? 0 }) }}</p>
          <small>{{ t('common.updatedAt', { date: displayDate(item.updatedAt) }) }}</small>
        </article>
        <div v-if="accounts.length === 0" class="panel empty-state"><strong>{{ t('accounts.empty') }}</strong><p>{{ t('accounts.emptyBody') }}</p></div>
      </div>
    </section>

    <section v-else-if="page === 'domains'" class="domain-workspace">
      <label class="account-picker"><span>{{ t('domains.account') }}</span><select :value="selectedAccountId" @change="changeAccount"><option v-for="item in accounts" :key="item.id" :value="item.id">{{ item.name }}</option></select></label>
      <div v-if="accounts.length === 0" class="panel empty-state"><strong>{{ t('domains.noAccount') }}</strong><p>{{ t('domains.noAccountBody') }}</p></div>
      <div v-else class="management-grid">
        <p v-if="selectedAccount && !selectedAccount.hostReady" class="inline-feedback" role="status">{{ t('account.provisioningReadOnly') }}</p>
        <form class="panel management-form" @submit.prevent="submitDomain">
          <div class="panel-heading"><h2>{{ t('domains.create') }}</h2></div>
          <label><span>{{ t('domains.name') }}</span><input v-model="domainForm.name" required inputmode="url" spellcheck="false" :disabled="!selectedAccount?.hostReady"></label>
          <label><span>{{ t('domains.canonical') }}</span><select v-model="domainForm.canonicalMode" :disabled="!selectedAccount?.hostReady"><option value="serve_both">{{ t('domains.serveBoth') }}</option><option value="prefer_apex">{{ t('domains.preferApex') }}</option><option value="prefer_www">{{ t('domains.preferWWW') }}</option></select></label>
          <label><span>{{ t('domains.targetType') }}</span><select v-model="domainForm.targetType" :disabled="!selectedAccount?.hostReady"><option value="static">{{ t('domains.staticTarget') }}</option><option value="php" :disabled="availablePHPVersions.length === 0">{{ t('domains.phpTarget') }}</option></select><small v-if="availablePHPVersions.length === 0">{{ t('php.notIncluded') }}</small></label>
          <label v-if="domainForm.targetType === 'php'"><span>{{ t('php.versionLabel') }}</span><select v-model="domainForm.phpVersion" required :disabled="!selectedAccount?.hostReady"><option v-for="version in availablePHPVersions" :key="version" :value="version">{{ t('php.version', { version }) }}</option></select></label>
          <label><span>{{ t('domains.rootMode') }}</span><select v-model="domainForm.rootMode" :disabled="!selectedAccount?.hostReady"><option value="default">{{ t('domains.defaultRoot') }}</option><option value="custom">{{ t('domains.customRoot') }}</option></select></label>
          <label v-if="domainForm.rootMode === 'custom'"><span>{{ t('domains.documentRoot') }}</span><input v-model="domainForm.documentRoot" required spellcheck="false" :disabled="!selectedAccount?.hostReady"></label>
          <label class="check-field"><input v-model="domainForm.tls" type="checkbox" :disabled="!selectedAccount?.hostReady"><span>{{ t('domains.enableTLS') }}</span></label>
          <label><span>{{ t('domains.wafMode') }}</span><select v-model="domainForm.wafMode" :disabled="!selectedAccount?.hostReady"><option value="off">{{ t('waf.off') }}</option><option value="detection_only">{{ t('waf.detection_only') }}</option><option value="blocking_pl1">{{ t('waf.blocking_pl1') }}</option></select><small>{{ t('waf.hint') }}</small></label>
          <label><span>{{ t('cache.preset') }}</span><select v-model="domainForm.cachePreset" :disabled="!selectedAccount?.hostReady || domainForm.targetType !== 'php'"><option value="disabled">{{ t('cache.disabled') }}</option><option value="respect_origin">{{ t('cache.respect_origin') }}</option><option value="wordpress">{{ t('cache.wordpress') }}</option></select><small>{{ t('cache.hint') }}</small></label>
          <button class="primary-action" type="submit" :disabled="actionBusy || !canCreateDomain">{{ t('domains.createAction') }}</button>
        </form>
        <div class="resource-list">
          <article v-for="domain in domains" :key="domain.id" class="panel resource-card domain-card">
            <header><div><h2>{{ domain.name.display }}</h2><code>{{ domain.target.type === 'php' ? t('php.domainSummary', { version: domain.target.phpVersion, root: domain.target.documentRoot?.relativePath ?? domain.target.type }) : domain.target.documentRoot?.relativePath ?? domain.target.type }}</code></div><span class="state-badge" :data-state="domain.status">{{ t(`states.${domain.status}`) }}</span></header>
            <p>{{ domain.tls.enabled ? t('domains.tlsState', { state: domain.tls.issuanceStatus }) : t('domains.tlsDisabled') }} · {{ t('domains.wafState', { mode: t(`waf.${domainWAFMode(domain)}`) }) }} · {{ t('cache.state', { preset: t(`cache.${domainCachePreset(domain)}`) }) }}</p>
            <div class="card-actions">
              <button v-if="canUseWAFExceptions && domainWAFMode(domain) !== 'off'" class="secondary-action" type="button" :disabled="actionBusy || !selectedAccount?.hostReady" @click="manageWAFExceptions(domain)">{{ t('wafExceptions.manage') }}</button>
              <button v-if="domain.status === 'suspended'" class="secondary-action" type="button" :disabled="actionBusy || !selectedAccount?.hostReady" @click="runDomainAction(domain, 'resume')">{{ t('common.resume') }}</button>
              <button v-else class="secondary-action" type="button" :disabled="actionBusy || !selectedAccount?.hostReady" @click="runDomainAction(domain, 'suspend')">{{ t('common.suspend') }}</button>
              <button class="danger-action" type="button" :disabled="actionBusy || !selectedAccount?.hostReady" @click="runDomainAction(domain, 'remove')">{{ t('common.remove') }}</button>
            </div>
          </article>
          <div v-if="domains.length === 0" class="panel empty-state"><strong>{{ t('domains.empty') }}</strong><p>{{ t('domains.emptyBody') }}</p></div>
        </div>
        <section v-if="selectedWAFDomain" class="panel management-form" aria-labelledby="waf-exception-title">
          <div class="panel-heading"><div><p class="eyebrow">{{ t('wafExceptions.eyebrow') }}</p><h2 id="waf-exception-title">{{ t('wafExceptions.title', { domain: selectedWAFDomain.name.display }) }}</h2></div></div>
          <p class="form-hint">{{ t('wafExceptions.hint') }}</p>
          <form class="management-form" @submit.prevent="submitWAFException">
            <label><span>{{ t('wafExceptions.ruleId') }}</span><input v-model.number="wafExceptionForm.ruleId" required type="number" min="920000" max="944999"></label>
            <label><span>{{ t('wafExceptions.exactPath') }}</span><input v-model="wafExceptionForm.requestPath" maxlength="512" :placeholder="t('wafExceptions.pathPlaceholder')" spellcheck="false"></label>
            <label><span>{{ t('wafExceptions.parameter') }}</span><input v-model="wafExceptionForm.parameter" maxlength="128" :placeholder="t('wafExceptions.parameterPlaceholder')" spellcheck="false"></label>
            <label><span>{{ t('wafExceptions.expiresAt') }}</span><input v-model="wafExceptionForm.expiresAt" required type="datetime-local"></label>
            <button class="primary-action" type="submit" :disabled="actionBusy || (!wafExceptionForm.requestPath && !wafExceptionForm.parameter)">{{ t('wafExceptions.create') }}</button>
          </form>
          <div v-if="wafExceptions.length" class="responsive-table"><table><thead><tr><th>{{ t('wafExceptions.ruleId') }}</th><th>{{ t('wafExceptions.scope') }}</th><th>{{ t('wafExceptions.expiresAt') }}</th><th>{{ t('common.actions') }}</th></tr></thead><tbody><tr v-for="exception in wafExceptions" :key="exception.id"><td><code>{{ exception.ruleId }}</code></td><td><code>{{ exception.requestPath || t('wafExceptions.allPaths') }}</code><small v-if="exception.parameter">{{ t('wafExceptions.parameterValue', { parameter: exception.parameter }) }}</small></td><td>{{ displayDate(exception.expiresAt) }}</td><td><button class="danger-action" type="button" :disabled="actionBusy" @click="removeWAFException(exception)">{{ t('common.remove') }}</button></td></tr></tbody></table></div>
          <div v-else class="empty-state"><strong>{{ t('wafExceptions.empty') }}</strong><p>{{ t('wafExceptions.emptyBody') }}</p></div>
        </section>
      </div>
    </section>

    <section v-else-if="page === 'services'" class="panel table-panel">
      <div class="host-summary"><div><span>{{ t('services.operatingSystem') }}</span><strong>{{ platformLabel }}</strong></div><div><span>{{ t('services.kernel') }}</span><strong>{{ capabilities?.platform.kernelRelease ?? t('common.notAvailable') }}</strong></div><div><span>{{ t('services.architecture') }}</span><strong>{{ capabilities?.platform.architecture ?? t('common.notAvailable') }}</strong></div></div>
      <div class="responsive-table"><table><thead><tr><th>{{ t('services.service') }}</th><th>{{ t('services.unit') }}</th><th>{{ t('services.activeState') }}</th><th>{{ t('common.status') }}</th></tr></thead><tbody><tr v-for="service in capabilities?.services ?? []" :key="service.key"><td><strong>{{ service.key }}</strong></td><td><code>{{ service.unit }}</code></td><td>{{ service.activeState }} / {{ service.subState }}</td><td><span class="state-badge" :data-state="service.availability.status">{{ t(`states.${service.availability.status}`) }}</span></td></tr></tbody></table></div>
      <div v-if="!capabilities" class="empty-state"><strong>{{ t('services.unavailable') }}</strong><p>{{ t('services.unavailableBody') }}</p></div>
    </section>

    <section v-else-if="page === 'operations'" class="panel table-panel">
      <div class="responsive-table"><table><thead><tr><th>{{ t('operations.kind') }}</th><th>{{ t('common.status') }}</th><th>{{ t('operations.progress') }}</th><th>{{ t('operations.updated') }}</th></tr></thead><tbody><tr v-for="operation in operations" :key="operation.id"><td><strong>{{ operation.kind }}</strong><small><code>{{ operation.id }}</code></small></td><td><span class="state-badge" :data-state="operation.status">{{ t(`states.${operation.status}`) }}</span></td><td><progress max="100" :value="operation.progressPercent" :aria-label="t('operations.progress')">{{ formatPercent(operation.progressPercent / 100, activeLocale) }}</progress><small>{{ operation.stage }}</small></td><td>{{ displayDate(operation.updatedAt) }}</td></tr></tbody></table></div>
      <div v-if="operations.length === 0" class="empty-state"><strong>{{ t('operations.empty') }}</strong><p>{{ t('operations.emptyBody') }}</p></div>
    </section>

    <section v-else-if="page === 'audit'" class="panel table-panel">
      <div class="responsive-table"><table><thead><tr><th>{{ t('audit.event') }}</th><th>{{ t('audit.target') }}</th><th>{{ t('audit.result') }}</th><th>{{ t('audit.occurred') }}</th></tr></thead><tbody><tr v-for="event in auditEvents" :key="event.id"><td><strong>{{ event.action }}</strong><small><code>#{{ event.sequence }}</code></small></td><td>{{ event.targetType }}<small v-if="event.targetId"><code>{{ event.targetId }}</code></small></td><td><span class="state-badge" :data-state="event.result">{{ t(`states.${event.result}`) }}</span></td><td>{{ displayDate(event.occurredAt) }}</td></tr></tbody></table></div>
      <div v-if="auditEvents.length === 0" class="empty-state"><strong>{{ t('audit.empty') }}</strong><p>{{ t('audit.emptyBody') }}</p></div>
    </section>

    <section v-else-if="page === 'updates'" class="panel update-panel">
      <header class="update-heading">
        <div><p class="eyebrow">{{ t('updates.channel') }}</p><h2>{{ t('updates.title') }}</h2><p>{{ t('updates.body') }}</p></div>
        <span v-if="updateStatus" class="state-badge" :data-state="updateStatus.updateAvailable ? 'pending' : 'active'">{{ updateStatus.updateAvailable ? t('updates.available') : t('updates.current') }}</span>
      </header>

      <div v-if="updateStatus" class="update-grid">
        <dl class="detail-list update-details">
          <div><dt>{{ t('updates.currentVersion') }}</dt><dd><code>{{ updateStatus.currentVersion }}</code></dd></div>
          <div><dt>{{ t('updates.latestVersion') }}</dt><dd><a v-if="updateStatus.latestRelease" :href="updateStatus.latestRelease.url" target="_blank" rel="noopener noreferrer"><code>{{ updateStatus.latestRelease.tag }}</code></a><span v-else>{{ t('common.notAvailable') }}</span></dd></div>
          <div><dt>{{ t('updates.releaseIntegrity') }}</dt><dd>{{ updateStatus.latestRelease?.immutable ? t('updates.immutable') : t('common.notAvailable') }}</dd></div>
          <div><dt>{{ t('updates.lastSuccessfulCheck') }}</dt><dd>{{ displayDate(updateStatus.lastSuccessfulAt) }}</dd></div>
          <div><dt>{{ t('updates.nextCheck') }}</dt><dd>{{ updateStatus.automaticChecks ? displayDate(updateStatus.nextAutomaticCheckAt) : t('updates.disabled') }}</dd></div>
          <div><dt>{{ t('updates.checkInterval') }}</dt><dd>{{ t('updates.hours', { count: formatNumber(updateStatus.checkIntervalSeconds / 3600, activeLocale) }) }}</dd></div>
        </dl>

        <form class="management-form update-policy" @submit.prevent="submitUpdatePolicy">
          <label><span>{{ t('updates.releaseChannel') }}</span><select v-model="updateForm.channel"><option value="stable">{{ t('updates.stable') }}</option><option value="beta">{{ t('updates.beta') }}</option></select></label>
          <p class="form-hint">{{ updateForm.channel === 'stable' ? t('updates.stableHint') : t('updates.betaHint') }}</p>
          <label class="check-field"><input v-model="updateForm.automaticChecks" type="checkbox"><span>{{ t('updates.automaticChecks') }}</span></label>
          <p class="form-hint">{{ t('updates.automaticChecksHint') }}</p>
          <div class="form-actions"><button class="primary-action" type="submit" :disabled="actionBusy || !updatePolicyChanged">{{ t('updates.savePolicy') }}</button><button class="secondary-action" type="button" :disabled="actionBusy" @click="emit('checkUpdates')">{{ t('updates.checkNow') }}</button></div>
        </form>
      </div>

      <p v-if="updateStatus?.lastErrorCode" class="inline-feedback error" role="alert">{{ t(`errors.${updateStatus.lastErrorCode}`) }}<span v-if="updateStatus.rateLimitResetAt"> {{ t('updates.retryAfter', { date: displayDate(updateStatus.rateLimitResetAt) }) }}</span></p>
      <p v-if="updateStatus" class="update-safety-note">{{ t('updates.functionalUpdatesOff') }}</p>
      <div v-else class="empty-state"><strong>{{ t('updates.unavailable') }}</strong><p>{{ t('updates.unavailableBody') }}</p></div>
    </section>

    <div v-else class="management-grid">
      <section class="panel settings-panel">
        <div><p class="eyebrow">{{ t('settings.identity') }}</p><h2>{{ session.identity.displayName }}</h2><p>{{ session.identity.email }}</p></div>
        <dl class="detail-list"><div><dt>{{ t('settings.authenticationLevel') }}</dt><dd>{{ session.authenticationLevel }}</dd></div><div><dt>{{ t('settings.sessionExpires') }}</dt><dd>{{ displayDate(session.expiresAt) }}</dd></div><div><dt>{{ t('settings.locale') }}</dt><dd>{{ t(`localeNames.${activeLocale}`) }}</dd></div></dl>
        <button class="danger-action" type="button" :disabled="actionBusy" @click="emit('logout')">{{ t('auth.signOut') }}</button>
      </section>

      <section class="panel settings-panel acme-settings">
        <div><p class="eyebrow">{{ t('acme.eyebrow') }}</p><h2>{{ t('acme.title') }}</h2><p>{{ t('acme.body') }}</p></div>
        <dl v-if="productionACMEAccount" class="detail-list">
          <div><dt>{{ t('acme.environment') }}</dt><dd>{{ t('acme.production') }}</dd></div>
          <div><dt>{{ t('common.status') }}</dt><dd><span class="state-badge" :data-state="productionACMEAccount.status">{{ t(`states.${productionACMEAccount.status}`) }}</span></dd></div>
          <div><dt>{{ t('acme.contactEmail') }}</dt><dd>{{ productionACMEAccount.contactEmail }}</dd></div>
          <div><dt>{{ t('acme.registeredAt') }}</dt><dd>{{ displayDate(productionACMEAccount.registeredAt) }}</dd></div>
        </dl>
        <p v-else-if="acmeRegistrationPending" class="inline-feedback" role="status">{{ t('acme.registrationPending') }}</p>
        <form v-else class="management-form" @submit.prevent="submitACMEAccount">
          <label><span>{{ t('acme.contactEmail') }}</span><input v-model="acmeForm.contactEmail" required type="email" autocomplete="email" maxlength="320"></label>
          <label class="check-field"><input v-model="acmeForm.termsAccepted" required type="checkbox"><span>{{ t('acme.acceptTerms') }}</span></label>
          <p class="form-hint">{{ t('acme.productionHint') }}</p>
          <button class="primary-action" type="submit" :disabled="actionBusy || !acmeForm.termsAccepted">{{ t('acme.registerAction') }}</button>
        </form>
      </section>
    </div>
  </div>
</template>
