<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AccountContent from './AccountContent.vue'
import { accountNavigation, type AccountPageKey } from './account'
import AdminContent from './AdminContent.vue'
import { platformNavigation, type AdminPageKey, workspaceNavigation } from './admin'
import {
  api,
  ApiError,
  type AccountPHPStatus,
  type ACMEAccount,
  type AuditEvent,
  type BootstrapStatus,
  type BuildInfo,
  type DatabaseCredential,
  type DatabaseWorkspace,
  type Domain,
  type DomainWAFException,
  type DomainTargetInput,
  type FileListing,
  type HostCapabilities,
  type HostingAccount,
  type HostingPackage,
  type ManagedSession,
  type Operation,
  type PackageLimits,
  type SelfServiceContext,
  type Session,
  type TLSCertificate,
  type UpdateStatus,
} from './api'
import AuthView from './AuthView.vue'
import { isSupportedLocale } from './i18n'
import { submitPHPMyAdminHandoff } from './phpmyadmin'

type Health = 'loading' | 'healthy' | 'unavailable'
type ApplicationState = 'checking' | 'bootstrap' | 'login' | 'authenticated' | 'fatal'
type ConsoleMode = 'administrator' | 'account'
type PageKey = AdminPageKey | AccountPageKey

const { locale, t } = useI18n()
const applicationState = ref<ApplicationState>('checking')
const bootstrapStatus = ref<BootstrapStatus | null>(null)
const session = ref<Session | null>(null)
const selfContext = ref<SelfServiceContext | null>(null)
const consoleMode = ref<ConsoleMode>('administrator')
const apiHealth = ref<Health>('loading')
const build = ref<BuildInfo | null>(null)
const packages = ref<HostingPackage[]>([])
const accounts = ref<HostingAccount[]>([])
const domains = ref<Domain[]>([])
const wafExceptions = ref<DomainWAFException[]>([])
const accountPHP = ref<AccountPHPStatus | null>(null)
const databaseWorkspace = ref<DatabaseWorkspace>({ databases: [], users: [], grants: [] })
const databaseCredential = ref<DatabaseCredential | null>(null)
const fileListing = ref<FileListing>({ path: '', entries: [], omittedEntries: 0 })
const operations = ref<Operation[]>([])
const accountOperation = ref<Operation | null>(null)
const certificateHistory = ref<Record<string, TLSCertificate[]>>({})
const certificateHistoryLoadingDomainId = ref('')
const auditEvents = ref<AuditEvent[]>([])
const managedSessions = ref<ManagedSession[]>([])
const capabilities = ref<HostCapabilities | null>(null)
const updateStatus = ref<UpdateStatus | null>(null)
const acmeAccounts = ref<ACMEAccount[]>([])
const selectedOwnerAccountId = ref('')
const dataLoading = ref(false)
const actionBusy = ref(false)
const errorCode = ref('')
const noticeCode = ref('')
const activeAdminPage = ref<AdminPageKey>('overview')
const activeAccountPage = ref<AccountPageKey>('overview')
const mobileNavigationOpen = ref(false)
const isNarrow = ref(false)
const sidebar = ref<HTMLElement | null>(null)
const menuButton = ref<HTMLButtonElement | null>(null)
const mainContent = ref<HTMLElement | null>(null)
const pageHeading = ref<HTMLHeadingElement | null>(null)
let navigationMedia: MediaQueryList | null = null
let accountOperationTimer: ReturnType<typeof setTimeout> | null = null
let accountOperationGeneration = 0

const accountOperationPollInterval = 1000

const activePage = computed<PageKey>(() => consoleMode.value === 'administrator' ? activeAdminPage.value : activeAccountPage.value)
const healthLabel = computed(() => t(`states.${apiHealth.value}`))
const activePageTitle = computed(() => t(`navigation.${activePage.value}`))
const activePageDescription = computed(() => consoleMode.value === 'administrator'
  ? t(`pages.${activeAdminPage.value}`)
  : t(`accountPages.${activeAccountPage.value}`))
const identityName = computed(() => session.value?.identity.displayName ?? t('topbar.user'))
const identityEmail = computed(() => session.value?.identity.email ?? '')
const canSwitchWorkspace = computed(() => consoleMode.value === 'administrator'
  ? (selfContext.value?.accounts.length ?? 0) > 0
  : selfContext.value?.platformAdministrator === true)
const topbarContext = computed(() => consoleMode.value === 'administrator'
  ? t('topbar.adminContext') : t('topbar.accountContext'))

function errorIdentifier(error: unknown): string {
  return error instanceof ApiError ? error.code : 'request_failed'
}

function setLocale(event: Event) {
  const nextLocale = (event.target as HTMLSelectElement).value
  if (isSupportedLocale(nextLocale)) locale.value = nextLocale
}

async function focusMainContent() {
  await nextTick()
  mainContent.value?.focus()
}

function updateNarrowState(event: MediaQueryList | MediaQueryListEvent) {
  isNarrow.value = event.matches
  if (!event.matches) mobileNavigationOpen.value = false
}

async function openNavigation() {
  mobileNavigationOpen.value = true
  await nextTick()
  sidebar.value?.querySelector<HTMLElement>('.sidebar-close')?.focus()
}

async function closeNavigation(restoreFocus = true) {
  if (!mobileNavigationOpen.value) return
  mobileNavigationOpen.value = false
  await nextTick()
  if (restoreFocus) menuButton.value?.focus()
}

async function selectPage(page: PageKey) {
  if (consoleMode.value === 'administrator') activeAdminPage.value = page as AdminPageKey
  else activeAccountPage.value = page as AccountPageKey
  errorCode.value = ''
  noticeCode.value = ''
  await closeNavigation(false)
  await nextTick()
  pageHeading.value?.focus()
}

function handleSidebarKeydown(event: KeyboardEvent) {
  if (!isNarrow.value || !mobileNavigationOpen.value) return
  if (event.key === 'Escape') {
    event.preventDefault()
    void closeNavigation()
    return
  }
  if (event.key !== 'Tab' || !sidebar.value) return
  const focusable = Array.from(sidebar.value.querySelectorAll<HTMLElement>(
    'button:not([disabled]), a[href], select:not([disabled]), [tabindex]:not([tabindex="-1"])',
  )).filter((element) => !element.hasAttribute('hidden'))
  const first = focusable[0]
  const last = focusable[focusable.length - 1]
  if (!first || !last) return
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault()
    first.focus()
  }
}

async function initialize() {
  void api.health().then(() => { apiHealth.value = 'healthy' }).catch(() => { apiHealth.value = 'unavailable' })
  void api.build().then((value) => { build.value = value }).catch(() => { build.value = null })
  try {
    bootstrapStatus.value = await api.bootstrapStatus()
    if (bootstrapStatus.value.required) {
      applicationState.value = 'bootstrap'
      return
    }
    try {
      await acceptSession(await api.session())
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        applicationState.value = 'login'
        return
      }
      throw error
    }
  } catch (error) {
    errorCode.value = errorIdentifier(error)
    applicationState.value = 'fatal'
  }
}

async function acceptSession(value: Session) {
  session.value = value
  if (isSupportedLocale(value.identity.locale)) locale.value = value.identity.locale
  try {
    selfContext.value = await api.selfContext()
  } catch (error) {
    session.value = null
    if (error instanceof ApiError && error.status === 401) {
      applicationState.value = 'login'
      return
    }
    errorCode.value = errorIdentifier(error)
    applicationState.value = 'fatal'
    return
  }
  consoleMode.value = selfContext.value.platformAdministrator ? 'administrator' : 'account'
  selectedOwnerAccountId.value = selfContext.value.accounts[0]?.id ?? ''
  applicationState.value = 'authenticated'
  await nextTick()
  await refreshCurrentData()
}

async function refreshCurrentData() {
  if (consoleMode.value === 'administrator') await refreshAdminData()
  else await refreshAccountData()
}

async function refreshAdminData() {
  dataLoading.value = true
  errorCode.value = ''
  const results = await Promise.allSettled([
    api.packages(), api.accounts(), api.operations(), api.auditEvents(), api.acmeAccounts(), api.hostCapabilities(),
    api.updateStatus(),
  ])
  if (results[0].status === 'fulfilled') packages.value = results[0].value
  if (results[1].status === 'fulfilled') accounts.value = results[1].value
  if (results[2].status === 'fulfilled') operations.value = results[2].value
  if (results[3].status === 'fulfilled') auditEvents.value = results[3].value
  if (results[4].status === 'fulfilled') acmeAccounts.value = results[4].value
  if (results[5].status === 'fulfilled') capabilities.value = results[5].value
  else capabilities.value = null
  if (results[6].status === 'fulfilled') updateStatus.value = results[6].value
  else updateStatus.value = null
  handleDataFailures(results.slice(0, 5))
  dataLoading.value = false
}

async function refreshAccountData() {
  dataLoading.value = true
  errorCode.value = ''
  const results = await Promise.allSettled([api.selfContext(), api.sessions()])
  if (results[0].status === 'fulfilled') {
    selfContext.value = results[0].value
    if (!results[0].value.accounts.some((account) => account.id === selectedOwnerAccountId.value)) {
      selectedOwnerAccountId.value = results[0].value.accounts[0]?.id ?? ''
    }
  }
  if (results[1].status === 'fulfilled') managedSessions.value = results[1].value
  if (!handleDataFailures(results)) {
    if (selectedOwnerAccountId.value) await loadOwnerAccountResources(selectedOwnerAccountId.value)
    else {
      domains.value = []
      accountPHP.value = null
      databaseWorkspace.value = { databases: [], users: [], grants: [] }
      fileListing.value = { path: '', entries: [], omittedEntries: 0 }
      databaseCredential.value = null
    }
  }
  dataLoading.value = false
}

function handleDataFailures(results: PromiseSettledResult<unknown>[]): boolean {
  const authenticationFailure = results.find((result) => (
    result.status === 'rejected' && result.reason instanceof ApiError && result.reason.status === 401
  ))
  if (authenticationFailure) {
    clearAuthenticatedState()
    applicationState.value = 'login'
    return true
  }
  const primaryFailure = results.find((result) => result.status === 'rejected')
  if (primaryFailure?.status === 'rejected') errorCode.value = errorIdentifier(primaryFailure.reason)
  return false
}

async function loadDomains(accountId: string) {
  accountPHP.value = null
	wafExceptions.value = []
  const results = await Promise.allSettled([api.domains(accountId), api.accountPHP(accountId)])
  if (results[0].status === 'fulfilled') domains.value = results[0].value
  else domains.value = []
  if (results[1].status === 'fulfilled') accountPHP.value = results[1].value
  handleDataFailures(results)
}

async function loadWAFExceptions(input: { accountId: string; domainId: string }) {
  try {
    wafExceptions.value = await api.wafExceptions(input.accountId, input.domainId)
  } catch (error) {
    wafExceptions.value = []
    errorCode.value = errorIdentifier(error)
  }
}

async function createWAFException(input: {
  accountId: string; domainId: string; ruleId: number; requestPath?: string; parameter?: string; expiresAt: string
}) {
  await runAction(async () => {
    const { accountId, domainId, ...request } = input
    await api.createWAFException(accountId, domainId, request)
    operations.value = await api.operations()
    noticeCode.value = 'wafExceptionQueued'
  })
}

async function removeWAFException(input: { accountId: string; domainId: string; exceptionId: string }) {
  await runAction(async () => {
    await api.removeWAFException(input.accountId, input.domainId, input.exceptionId)
    operations.value = await api.operations()
    noticeCode.value = 'wafExceptionRemovalQueued'
  })
}

async function loadOwnerAccountResources(accountId: string) {
	const account = selfContext.value?.accounts.find((item) => item.id === accountId)
  const results = await Promise.allSettled([
    api.domains(accountId), api.accountPHP(accountId), api.databaseWorkspace(accountId),
		account?.membershipRole === 'auditor' ? Promise.resolve(null) : api.files(accountId),
  ])
  if (selectedOwnerAccountId.value !== accountId) return
  if (results[0].status === 'fulfilled') domains.value = results[0].value
  else domains.value = []
  if (results[1].status === 'fulfilled') accountPHP.value = results[1].value
  else accountPHP.value = null
  if (results[2].status === 'fulfilled') databaseWorkspace.value = results[2].value
  else databaseWorkspace.value = { databases: [], users: [], grants: [] }
	if (results[3].status === 'fulfilled' && results[3].value) fileListing.value = results[3].value
	else fileListing.value = { path: '', entries: [], omittedEntries: 0 }
  handleDataFailures(results)
}

async function loadFiles(input: { accountId: string; path: string; cursor?: string }) {
	try {
		const listing = await api.files(input.accountId, input.path, input.cursor)
		if (selectedOwnerAccountId.value !== input.accountId) return
		fileListing.value = input.cursor && fileListing.value.path === listing.path
			? { ...listing, entries: [...fileListing.value.entries, ...listing.entries], omittedEntries: fileListing.value.omittedEntries + listing.omittedEntries }
			: listing
		errorCode.value = ''
	} catch (error) {
		errorCode.value = errorIdentifier(error)
	}
}

async function selectOwnerAccount(accountId: string) {
  stopAccountOperationTracking(true)
  certificateHistory.value = {}
  certificateHistoryLoadingDomainId.value = ''
  selectedOwnerAccountId.value = accountId
  databaseCredential.value = null
  fileListing.value = { path: '', entries: [], omittedEntries: 0 }
  errorCode.value = ''
  noticeCode.value = ''
  await loadOwnerAccountResources(accountId)
}

async function switchWorkspace() {
  if (!canSwitchWorkspace.value) return
  stopAccountOperationTracking(true)
  certificateHistory.value = {}
  certificateHistoryLoadingDomainId.value = ''
  consoleMode.value = consoleMode.value === 'administrator' ? 'account' : 'administrator'
  errorCode.value = ''
  noticeCode.value = ''
  await closeNavigation(false)
  await refreshCurrentData()
  await nextTick()
  pageHeading.value?.focus()
}

async function createPackage(input: { name: string; slug: string; limits: PackageLimits }) {
  await runAction(async () => {
    await api.createPackage(input)
    packages.value = await api.packages()
    noticeCode.value = 'packageCreated'
  })
}

async function createAccount(input: { name: string; slug: string; packageId: string; ownerIdentityId?: string }) {
  await runAction(async () => {
    await api.createAccount(input)
    accounts.value = await api.accounts()
    selfContext.value = await api.selfContext()
    noticeCode.value = 'accountCreated'
  })
}

async function registerACMEAccount(input: {
  environment: 'letsencrypt-production'
  contactEmail: string
  termsAccepted: boolean
}) {
  await runAction(async () => {
    await api.registerACMEAccount(input)
    acmeAccounts.value = await api.acmeAccounts()
    operations.value = await api.operations()
    noticeCode.value = 'acmeAccountQueued'
  })
}

async function createDomain(input: {
  accountId: string
  name: string
  canonicalMode: Domain['canonicalMode']
  target: DomainTargetInput
  disableTls: boolean
  tlsMode?: 'acme'
  wafMode: Domain['waf']['mode']
  cachePreset: NonNullable<Domain['cache']>['preset']
}) {
  await runAction(async () => {
    const { accountId, ...request } = input
    const operation = await api.createDomain(accountId, request)
    noticeCode.value = 'domainQueued'
    if (consoleMode.value === 'administrator') {
      operations.value = await api.operations()
      await loadDomains(accountId)
    } else {
      trackAccountOperation(accountId, operation.operationId)
    }
  })
}

async function createDatabase(input: {
  accountId: string
  databaseAlias: string
  existingUserId?: string
  newUserAlias?: string
  preset: 'read_only' | 'read_write'
}) {
  await runAction(async () => {
    const { accountId, ...request } = input
    const operation = await api.createDatabase(accountId, request)
    databaseCredential.value = null
    noticeCode.value = 'databaseQueued'
    trackAccountOperation(accountId, operation.operationId)
  })
}

async function revealDatabaseCredential(input: { accountId: string; userId: string }) {
  await runAction(async () => {
    databaseCredential.value = await api.revealDatabaseCredential(input.accountId, input.userId)
    databaseWorkspace.value = await api.databaseWorkspace(input.accountId)
    noticeCode.value = 'databaseCredentialRevealed'
  })
}

async function launchPHPMyAdmin(input: { accountId: string; userId: string }) {
  await runAction(async () => {
    const handoff = await api.phpMyAdminHandoff(input.accountId, input.userId)
    submitPHPMyAdminHandoff(handoff)
  })
}

async function rotateDatabaseCredential(input: { accountId: string; userId: string }) {
  await runAction(async () => {
    const operation = await api.rotateDatabaseCredential(input.accountId, input.userId)
    databaseCredential.value = null
    noticeCode.value = 'databaseCredentialRotationQueued'
    trackAccountOperation(input.accountId, operation.operationId)
  })
}

async function deleteDatabaseTarget(input: {
  accountId: string; targetKind: 'database' | 'user'; targetId: string; confirmation: string
}) {
  await runAction(async () => {
    const operation = await api.deleteDatabaseTarget(
      input.accountId, input.targetKind, input.targetId, input.confirmation,
    )
    databaseCredential.value = null
    noticeCode.value = 'databaseDeletionQueued'
    trackAccountOperation(input.accountId, operation.operationId)
  })
}

async function updateDomain(input: {
  accountId: string
  domainId: string
  canonicalMode: Domain['canonicalMode']
  target: DomainTargetInput
  wafMode: Domain['waf']['mode']
  cachePreset: NonNullable<Domain['cache']>['preset']
}) {
  await runAction(async () => {
    const { accountId, domainId, ...request } = input
    const operation = await api.updateDomain(accountId, domainId, request)
    noticeCode.value = 'domainUpdated'
    trackAccountOperation(accountId, operation.operationId)
  })
}

async function runDomainAction(input: {
  accountId: string; domainId: string; action: 'suspend' | 'resume' | 'remove'
}) {
  await runAction(async () => {
    const operation = await api.domainAction(input.accountId, input.domainId, input.action)
    noticeCode.value = 'domainQueued'
    if (consoleMode.value === 'administrator') {
      operations.value = await api.operations()
      await loadDomains(input.accountId)
    } else {
      trackAccountOperation(input.accountId, operation.operationId)
    }
  })
}

async function issueCertificate(input: { accountId: string; domainId: string }) {
  await runAction(async () => {
    const operation = await api.issueCertificate(input.accountId, input.domainId)
    noticeCode.value = 'certificateQueued'
    trackAccountOperation(input.accountId, operation.operationId, input.domainId)
  })
}

async function loadCertificateHistory(input: { accountId: string; domainId: string }) {
  certificateHistoryLoadingDomainId.value = input.domainId
  try {
    const certificates = await api.certificates(input.accountId, input.domainId)
    if (selectedOwnerAccountId.value === input.accountId) {
      certificateHistory.value = { ...certificateHistory.value, [input.domainId]: certificates }
    }
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      clearAuthenticatedState()
      applicationState.value = 'login'
      return
    }
    errorCode.value = errorIdentifier(error)
  } finally {
    if (certificateHistoryLoadingDomainId.value === input.domainId) {
      certificateHistoryLoadingDomainId.value = ''
    }
  }
}

function stopAccountOperationTracking(clear = false) {
  accountOperationGeneration++
  if (accountOperationTimer !== null) clearTimeout(accountOperationTimer)
  accountOperationTimer = null
  if (clear) accountOperation.value = null
}

function accountOperationTerminal(status: Operation['status']): boolean {
  return status === 'succeeded' || status === 'failed' || status === 'cancelled'
}

async function refreshTrackedAccount(accountId: string, generation: number, certificateDomainId = '') {
  const [context, accountDomains, php, databases, certificates] = await Promise.allSettled([
    api.selfContext(), api.domains(accountId), api.accountPHP(accountId),
    api.databaseWorkspace(accountId),
    certificateDomainId ? api.certificates(accountId, certificateDomainId) : Promise.resolve(null),
  ])
  if (generation !== accountOperationGeneration) return
  if (context.status === 'fulfilled') selfContext.value = context.value
  if (accountDomains.status === 'fulfilled' && selectedOwnerAccountId.value === accountId) {
    domains.value = accountDomains.value
  }
  if (php.status === 'fulfilled' && selectedOwnerAccountId.value === accountId) {
    accountPHP.value = php.value
  }
  if (databases.status === 'fulfilled' && selectedOwnerAccountId.value === accountId) {
    databaseWorkspace.value = databases.value
  }
  if (certificates.status === 'fulfilled' && certificates.value && selectedOwnerAccountId.value === accountId) {
    certificateHistory.value = {
      ...certificateHistory.value, [certificateDomainId]: certificates.value,
    }
  }
}

function trackAccountOperation(accountId: string, operationId: string, certificateDomainId = '') {
  stopAccountOperationTracking()
  const generation = accountOperationGeneration

  const poll = async () => {
    try {
      const operation = await api.accountOperation(accountId, operationId)
      if (generation !== accountOperationGeneration) return
      accountOperation.value = operation
      if (accountOperationTerminal(operation.status)) {
        await refreshTrackedAccount(
          accountId, generation,
          operation.kind === 'tls.certificate.lifecycle' ? certificateDomainId : '',
        )
        if (generation !== accountOperationGeneration) return
        if (operation.status === 'succeeded') {
          errorCode.value = ''
          noticeCode.value = 'operationSucceeded'
        } else {
          noticeCode.value = ''
          errorCode.value = 'operation_failed'
        }
        return
      }
      accountOperationTimer = setTimeout(() => { void poll() }, accountOperationPollInterval)
    } catch (error) {
      if (generation !== accountOperationGeneration) return
      if (error instanceof ApiError && error.status === 401) {
        clearAuthenticatedState()
        applicationState.value = 'login'
        return
      }
      errorCode.value = 'operation_status_unavailable'
    }
  }
  void poll()
}

async function updateProfile(input: { email: string; displayName: string; locale: 'en' | 'de' }) {
  await runAction(async () => {
    const identity = await api.updateProfile(input)
    if (session.value) session.value = { ...session.value, identity }
    locale.value = identity.locale
    noticeCode.value = 'profileUpdated'
  })
}

async function updatePolicy(input: { channel: UpdateStatus['channel']; automaticChecks: boolean }) {
  await runAction(async () => {
    updateStatus.value = await api.updatePolicy(input)
    noticeCode.value = 'updatePolicySaved'
  })
}

async function checkUpdates() {
  await runAction(async () => {
    updateStatus.value = await api.checkUpdates()
    noticeCode.value = 'updateCheckCompleted'
  })
}

async function revokeManagedSession(sessionId: string) {
  await runAction(async () => {
    const current = managedSessions.value.some((item) => item.id === sessionId && item.current)
    await api.revokeSession(sessionId)
    if (current) {
      clearAuthenticatedState()
      applicationState.value = 'login'
      return
    }
    managedSessions.value = await api.sessions()
    noticeCode.value = 'sessionRevoked'
  })
}

async function revokeOtherSessions() {
  await runAction(async () => {
    const result = await api.revokeOtherSessions()
    managedSessions.value = await api.sessions()
    noticeCode.value = result.revoked > 0 ? 'sessionsRevoked' : 'noSessionsRevoked'
  })
}

async function runAction(action: () => Promise<void>) {
  actionBusy.value = true
  errorCode.value = ''
  noticeCode.value = ''
  try {
    await action()
  } catch (error) {
    errorCode.value = errorIdentifier(error)
  } finally {
    actionBusy.value = false
  }
}

function clearAuthenticatedState() {
  stopAccountOperationTracking(true)
  session.value = null
  selfContext.value = null
  packages.value = []
  accounts.value = []
  domains.value = []
  accountPHP.value = null
  databaseWorkspace.value = { databases: [], users: [], grants: [] }
  fileListing.value = { path: '', entries: [], omittedEntries: 0 }
  databaseCredential.value = null
  operations.value = []
  certificateHistory.value = {}
  certificateHistoryLoadingDomainId.value = ''
  auditEvents.value = []
  managedSessions.value = []
  acmeAccounts.value = []
  capabilities.value = null
  updateStatus.value = null
}

async function logout() {
  await runAction(async () => {
    await api.logout()
    clearAuthenticatedState()
    applicationState.value = 'login'
  })
}

function openIdentityPage() {
  void selectPage(consoleMode.value === 'administrator' ? 'settings' : 'profile')
}

watch(locale, (nextLocale) => {
  document.documentElement.lang = nextLocale
  document.title = applicationState.value === 'authenticated' ? `${activePageTitle.value} · Stackfort` : 'Stackfort'
  window.localStorage.setItem('stackfort.locale', nextLocale)
}, { immediate: true })

watch([activeAdminPage, activeAccountPage, consoleMode], () => {
  if (applicationState.value === 'authenticated') document.title = `${activePageTitle.value} · Stackfort`
  if (activeAccountPage.value !== 'databases') databaseCredential.value = null
})

watch(mobileNavigationOpen, (open) => {
  document.body.classList.toggle('navigation-open', open && isNarrow.value)
})

onMounted(() => {
  navigationMedia = window.matchMedia('(max-width: 900px)')
  updateNarrowState(navigationMedia)
  navigationMedia.addEventListener('change', updateNarrowState)
  void initialize()
})

onBeforeUnmount(() => {
  stopAccountOperationTracking()
  navigationMedia?.removeEventListener('change', updateNarrowState)
  document.body.classList.remove('navigation-open')
})
</script>

<template>
  <main v-if="applicationState === 'checking'" class="loading-screen" tabindex="-1"><span class="loading-mark" aria-hidden="true"></span><p role="status">{{ t('common.initializing') }}</p></main>
  <main v-else-if="applicationState === 'fatal'" class="loading-screen"><section class="fatal-card" role="alert"><h1>{{ t('errors.initializationTitle') }}</h1><p>{{ t(`errors.${errorCode}`) }}</p><button class="primary-action" type="button" @click="applicationState = 'checking'; initialize()">{{ t('common.retry') }}</button></section></main>
  <AuthView v-else-if="applicationState === 'bootstrap' || applicationState === 'login'" :initial-mode="applicationState" :bootstrap-status="bootstrapStatus" @authenticated="acceptSession" />

  <template v-else-if="session && selfContext">
    <a class="skip-link" href="#main-content" @click.prevent="focusMainContent">{{ t('accessibility.skipToContent') }}</a>
    <div class="app-shell">
      <aside id="primary-navigation" ref="sidebar" class="sidebar" :class="{ open: mobileNavigationOpen }" :aria-hidden="isNarrow && !mobileNavigationOpen ? 'true' : undefined" :inert="isNarrow && !mobileNavigationOpen" @keydown="handleSidebarKeydown">
        <div class="sidebar-header"><div class="brand" :aria-label="t('brand.name')"><span class="brand-mark" aria-hidden="true"><svg viewBox="0 0 40 40"><path d="M20 3 35 11v18L20 37 5 29V11L20 3Z"/><path class="brand-cut" d="m12 14 8-4 8 4-8 4-8-4Zm0 7 8 4 8-4v7l-8 5-8-5v-7Z"/></svg></span><span><strong>{{ t('brand.name') }}</strong><small>{{ t('brand.tagline') }}</small></span></div><button class="sidebar-close" type="button" :aria-label="t('topbar.closeMenu')" @click="closeNavigation()"><span aria-hidden="true">×</span></button></div>
        <button v-if="canSwitchWorkspace" class="workspace-switch" type="button" @click="switchWorkspace"><span class="nav-icon" :data-icon="consoleMode === 'administrator' ? 'globe' : 'server'" aria-hidden="true"></span><span><small>{{ t('workspace.switchTo') }}</small><strong>{{ consoleMode === 'administrator' ? t('workspace.account') : t('workspace.administration') }}</strong></span><span aria-hidden="true">↔</span></button>
        <nav :aria-label="t('sidebar.primaryNavigation')">
          <template v-if="consoleMode === 'administrator'">
            <section class="nav-section" aria-labelledby="workspace-navigation-heading"><h2 id="workspace-navigation-heading" class="nav-label">{{ t('sidebar.workspace') }}</h2><button v-for="item in workspaceNavigation" :key="item.key" type="button" class="nav-item" :class="{ active: item.key === activeAdminPage }" :aria-current="item.key === activeAdminPage ? 'page' : undefined" @click="selectPage(item.key)"><span class="nav-icon" :data-icon="item.icon" aria-hidden="true"></span><span>{{ t(`navigation.${item.key}`) }}</span></button></section>
            <section class="nav-section platform-section" aria-labelledby="platform-navigation-heading"><h2 id="platform-navigation-heading" class="nav-label">{{ t('sidebar.platform') }}</h2><button v-for="item in platformNavigation" :key="item.key" type="button" class="nav-item" :class="{ active: item.key === activeAdminPage }" :aria-current="item.key === activeAdminPage ? 'page' : undefined" @click="selectPage(item.key)"><span class="nav-icon" :data-icon="item.icon" aria-hidden="true"></span><span>{{ t(`navigation.${item.key}`) }}</span></button></section>
          </template>
          <section v-else class="nav-section" aria-labelledby="account-navigation-heading"><h2 id="account-navigation-heading" class="nav-label">{{ t('sidebar.account') }}</h2><button v-for="item in accountNavigation" :key="item.key" type="button" class="nav-item" :class="{ active: item.key === activeAccountPage }" :aria-current="item.key === activeAccountPage ? 'page' : undefined" @click="selectPage(item.key)"><span class="nav-icon" :data-icon="item.icon" aria-hidden="true"></span><span>{{ t(`navigation.${item.key}`) }}</span></button></section>
        </nav>
        <button class="identity-summary" type="button" :aria-label="t('topbar.openProfile')" @click="openIdentityPage"><span class="avatar" aria-hidden="true">{{ identityName.slice(0, 1).toUpperCase() }}</span><span><strong>{{ identityName }}</strong><small>{{ identityEmail }}</small></span><span class="identity-chevron" aria-hidden="true">›</span></button>
      </aside>

      <button v-if="isNarrow && mobileNavigationOpen" class="navigation-backdrop" type="button" :aria-label="t('topbar.closeMenu')" @click="closeNavigation()"></button>

      <div class="site-frame" :inert="isNarrow && mobileNavigationOpen">
        <header class="topbar"><button ref="menuButton" class="mobile-menu" type="button" :aria-label="t('topbar.openMenu')" :aria-expanded="mobileNavigationOpen" aria-controls="primary-navigation" @click="openNavigation"><span aria-hidden="true"></span><span aria-hidden="true"></span><span aria-hidden="true"></span></button><p class="context-label">{{ topbarContext }}</p><div class="topbar-actions"><div class="api-pill" :data-health="apiHealth" role="status" aria-live="polite"><span class="status-dot" aria-hidden="true"></span><span>{{ t('status.api') }}: {{ healthLabel }}</span></div><label class="language-select" for="language"><span>{{ t('topbar.language') }}</span><select id="language" :value="locale" @change="setLocale"><option value="en">{{ t('localeNames.en') }}</option><option value="de">{{ t('localeNames.de') }}</option></select></label></div></header>
        <main id="main-content" ref="mainContent" class="main-content" tabindex="-1"><div class="page"><header class="page-heading"><p class="eyebrow">{{ consoleMode === 'administrator' ? t('overview.eyebrow') : t('account.eyebrow') }}</p><h1 ref="pageHeading" class="page-title" tabindex="-1">{{ activePageTitle }}</h1><p>{{ activePageDescription }}</p></header><AdminContent v-if="consoleMode === 'administrator'" :page="activeAdminPage" :session="session" :health="apiHealth" :build="build" :packages="packages" :accounts="accounts" :domains="domains" :waf-exceptions="wafExceptions" :operations="operations" :audit-events="auditEvents" :capabilities="capabilities" :update-status="updateStatus" :php-status="accountPHP" :acme-accounts="acmeAccounts" :loading="dataLoading" :action-busy="actionBusy" :error-code="errorCode" :notice-code="noticeCode" @refresh="refreshAdminData" @create-package="createPackage" @create-account="createAccount" @register-acme-account="registerACMEAccount" @select-account="loadDomains" @create-domain="createDomain" @domain-action="runDomainAction" @load-waf-exceptions="loadWAFExceptions" @create-waf-exception="createWAFException" @remove-waf-exception="removeWAFException" @update-policy="updatePolicy" @check-updates="checkUpdates" @logout="logout" /><AccountContent v-else :page="activeAccountPage" :session="session" :accounts="selfContext.accounts" :selected-account-id="selectedOwnerAccountId" :domains="domains" :php-status="accountPHP" :database-workspace="databaseWorkspace" :database-credential="databaseCredential" :file-listing="fileListing" :operation="accountOperation" :certificate-history="certificateHistory" :certificate-history-loading-domain-id="certificateHistoryLoadingDomainId" :sessions="managedSessions" :health="apiHealth" :loading="dataLoading" :action-busy="actionBusy" :error-code="errorCode" :notice-code="noticeCode" @refresh="refreshAccountData" @select-account="selectOwnerAccount" @load-files="loadFiles" @create-domain="createDomain" @create-database="createDatabase" @reveal-database-credential="revealDatabaseCredential" @launch-php-my-admin="launchPHPMyAdmin" @rotate-database-credential="rotateDatabaseCredential" @delete-database-target="deleteDatabaseTarget" @dismiss-database-credential="databaseCredential = null" @update-domain="updateDomain" @domain-action="runDomainAction" @issue-certificate="issueCertificate" @load-certificate-history="loadCertificateHistory" @update-profile="updateProfile" @revoke-session="revokeManagedSession" @revoke-other-sessions="revokeOtherSessions" @logout="logout" /></div></main>
      </div>
    </div>
  </template>
</template>
