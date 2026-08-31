<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AccountPageKey } from './account'
import { api, ApiError } from './api'
import type {
  AccountPHPStatus, AccountWorkspace, BackupRecord, BackupRepositoryStatus, CacheStatus, DatabaseCredential, DatabaseWorkspace, Domain, DomainTargetInput,
  FileEntry, FileListing, FileTrashEntry, HostingLogPage, ManagedDatabaseUser, ManagedSession, Operation, Session, TLSCertificate,
	ScheduledJob, ScheduledJobInput, ScheduledJobSchedule,
	WAFEventPage,
} from './api'
import { formatBytes, formatDateTime, formatDuration, formatNumber, formatPercent } from './formatting'
import { isSupportedLocale, type SupportedLocale } from './i18n'

const props = defineProps<{
  page: AccountPageKey
  session: Session
  accounts: AccountWorkspace[]
  selectedAccountId: string
  domains: Domain[]
  phpStatus: AccountPHPStatus | null
  databaseWorkspace: DatabaseWorkspace
  databaseCredential: DatabaseCredential | null
  fileListing: FileListing
  operation: Operation | null
  certificateHistory: Record<string, TLSCertificate[]>
  certificateHistoryLoadingDomainId: string
  sessions: ManagedSession[]
  health: 'loading' | 'healthy' | 'unavailable'
  loading: boolean
  actionBusy: boolean
  errorCode: string
  noticeCode: string
}>()

const emit = defineEmits<{
  refresh: []
  selectAccount: [accountId: string]
  loadFiles: [input: { accountId: string; path: string; cursor?: string }]
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
  updateDomain: [input: {
    accountId: string
    domainId: string
    canonicalMode: Domain['canonicalMode']
    target: DomainTargetInput
    wafMode: Domain['waf']['mode']
    cachePreset: NonNullable<Domain['cache']>['preset']
  }]
  domainAction: [input: { accountId: string; domainId: string; action: 'suspend' | 'resume' | 'remove' }]
  issueCertificate: [input: { accountId: string; domainId: string }]
  createDatabase: [input: {
    accountId: string
    databaseAlias: string
    existingUserId?: string
    newUserAlias?: string
    preset: 'read_only' | 'read_write'
  }]
  revealDatabaseCredential: [input: { accountId: string; userId: string }]
  launchPhpMyAdmin: [input: { accountId: string; userId: string }]
  rotateDatabaseCredential: [input: { accountId: string; userId: string }]
  deleteDatabaseTarget: [input: {
    accountId: string; targetKind: 'database' | 'user'; targetId: string; confirmation: string
  }]
  dismissDatabaseCredential: []
  loadCertificateHistory: [input: { accountId: string; domainId: string }]
  updateProfile: [input: { email: string; displayName: string; locale: 'en' | 'de' }]
  revokeSession: [sessionId: string]
  revokeOtherSessions: []
  logout: []
}>()

const { locale, t } = useI18n()
const domainForm = reactive({
  name: '', canonicalMode: 'serve_both' as Domain['canonicalMode'],
  targetType: 'static' as 'static' | 'php', phpVersion: '',
  rootMode: 'default' as 'default' | 'custom', documentRoot: '', tls: true,
  wafMode: 'off' as Domain['waf']['mode'],
  cachePreset: 'disabled' as NonNullable<Domain['cache']>['preset'],
})
const profileForm = reactive({ email: '', displayName: '', locale: 'en' as 'en' | 'de' })
const editingDomainId = ref('')
const certificateHistoryDomainId = ref('')
const editForm = reactive({
  canonicalMode: 'serve_both' as Domain['canonicalMode'],
  targetType: 'static' as 'static' | 'php', phpVersion: '',
  rootMode: 'default' as 'default' | 'custom', documentRoot: '',
  wafMode: 'off' as Domain['waf']['mode'],
  cachePreset: 'disabled' as NonNullable<Domain['cache']>['preset'],
})
const databaseWizard = reactive({
  step: 1, databaseAlias: '', userMode: 'new' as 'new' | 'existing',
  existingUserId: '', newUserAlias: '', preset: 'read_write' as 'read_only' | 'read_write',
})
const selectedUploadFile = ref<File | null>(null)
const fileNodeName = ref('')
const fileMutationBusy = ref(false)
const fileMutationFeedback = ref('')
const fileMutationFailed = ref(false)
const uploadProgress = ref(0)
const activeUploadId = ref('')
const trashOpen = ref(false)
const trashLoading = ref(false)
const trashEntries = ref<FileTrashEntry[]>([])
const trashNext = ref('')
const backupScope = ref<BackupRecord['scope']>('account_files')
const backupSourcePath = ref('public_html')
const backups = ref<BackupRecord[]>([])
const backupsNext = ref('')
const backupsLoading = ref(false)
const backupBusy = ref(false)
const backupFeedback = ref('')
const backupFailed = ref(false)
const backupRepository = ref<BackupRepositoryStatus | null>(null)
const selectedBackupFile = ref<File | null>(null)
const backupUploadProgress = ref(0)
const activeBackupUploadId = ref('')
const logKind = ref<'access' | 'error'>('access')
const logDomainId = ref('')
const logPage = ref<HostingLogPage | null>(null)
const logsLoading = ref(false)
const logsFailed = ref(false)
const wafEventPage = ref<WAFEventPage | null>(null)
const wafEventsLoading = ref(false)
const wafEventsFailed = ref(false)
const cacheStatusByDomain = reactive<Record<string, CacheStatus>>({})
const cacheBusyDomainId = ref('')
const cachePathPrefix = reactive<Record<string, string>>({})
const cacheFeedback = reactive<Record<string, 'queued' | 'failed' | ''>>({})
const scheduledJobList = ref<ScheduledJob[]>([])
const scheduledJobsLoading = ref(false)
const scheduledJobBusy = ref(false)
const scheduledJobFeedback = ref('')
const scheduledJobFailed = ref(false)
const editingScheduledJobId = ref('')
const scheduledJobForm = reactive({
	name: '', runtime: 'shell' as ScheduledJob['runtime'], scriptPath: 'jobs/task.sh', phpVersion: '',
	scheduleKind: 'hourly' as ScheduledJobSchedule['kind'], intervalMinutes: 15,
	hourUtc: 0, minuteUtc: 0, weekday: 'mon' as NonNullable<ScheduledJobSchedule['weekday']>, enabled: true,
})
let backupUploadAbortController: AbortController | null = null
let backupUploadCancelRequested = false
let uploadAbortController: AbortController | null = null
let uploadCancelRequested = false

const activeLocale = computed<SupportedLocale>(() => isSupportedLocale(locale.value) ? locale.value : 'en')
const selectedAccount = computed(() => props.accounts.find((item) => item.id === props.selectedAccountId) ?? null)
const canManageDomains = computed(() => selectedAccount.value?.hostReady && selectedAccount.value.status === 'active' && (
  selectedAccount.value.membershipRole === 'owner' || selectedAccount.value.membershipRole === 'member'
))
const canRemoveDomains = computed(() => selectedAccount.value?.hostReady && selectedAccount.value.status === 'active' && selectedAccount.value.membershipRole === 'owner')
const domainPercent = computed(() => {
  const account = selectedAccount.value
  if (!account || account.effectiveLimits.maxDomains <= 0) return 0
  return Math.min(1, account.usage.domains / account.effectiveLimits.maxDomains)
})
const activeTLS = computed(() => props.domains.filter((item) => item.tls.issuanceStatus === 'active').length)
const otherSessions = computed(() => props.sessions.filter((item) => !item.current).length)
const trackedOperation = computed(() => (
  props.operation?.accountId === props.selectedAccountId ? props.operation : null
))
const availablePHPVersions = computed(() => props.phpStatus?.availableVersions ?? [])
const canCreateDomain = computed(() => canManageDomains.value && (
  domainForm.targetType === 'static' || availablePHPVersions.value.includes(domainForm.phpVersion)
))
const canSubmitEdit = computed(() => canManageDomains.value && (
  editForm.targetType === 'static' || availablePHPVersions.value.includes(editForm.phpVersion)
))
const canManageDatabases = computed(() => canManageDomains.value && (
  selectedAccount.value?.effectiveLimits.maxDatabases ?? 0
) > 0)
const canDeleteDatabases = computed(() => canRemoveDomains.value)
const canBrowseFiles = computed(() => selectedAccount.value?.hostReady === true && selectedAccount.value.membershipRole !== 'auditor')
const canMutateFiles = computed(() => canBrowseFiles.value && selectedAccount.value?.status === 'active')
const canViewBackups = computed(() => selectedAccount.value?.hostReady === true &&
  selectedAccount.value.membershipRole !== 'auditor')
const canCreateBackups = computed(() => canViewBackups.value && selectedAccount.value?.status === 'active')
const canRestoreBackups = computed(() => canCreateBackups.value && selectedAccount.value?.membershipRole === 'owner')
const canDeleteBackups = computed(() => canRestoreBackups.value)
const canViewLogs = computed(() => selectedAccount.value?.hostReady === true)
const canManageScheduledJobs = computed(() => selectedAccount.value?.hostReady === true &&
	selectedAccount.value.status === 'active' && selectedAccount.value.membershipRole !== 'auditor' &&
	selectedAccount.value.effectiveLimits.maxScheduledJobs > 0)
const activeDatabaseUsers = computed(() => props.databaseWorkspace.users.filter((item) => item.status === 'active'))
const selectedDatabaseUser = computed<ManagedDatabaseUser | null>(() => (
  activeDatabaseUsers.value.find((item) => item.id === databaseWizard.existingUserId) ?? null
))
const databaseAliasValid = computed(() => /^[a-z][a-z0-9_]{0,27}$/.test(databaseWizard.databaseAlias))
const databaseUserStepValid = computed(() => databaseWizard.userMode === 'existing'
  ? selectedDatabaseUser.value !== null
  : /^[a-z][a-z0-9_]{0,27}$/.test(databaseWizard.newUserAlias))

watch(() => props.session.identity, (identity) => {
  profileForm.email = identity.email
  profileForm.displayName = identity.displayName
  profileForm.locale = identity.locale
}, { immediate: true, deep: true })

watch(() => props.selectedAccountId, () => {
  certificateHistoryDomainId.value = ''
  editingDomainId.value = ''
  domainForm.targetType = 'static'
  domainForm.phpVersion = ''
  resetDatabaseWizard()
  emit('dismissDatabaseCredential')
	selectedUploadFile.value = null
	fileNodeName.value = ''
	fileMutationFeedback.value = ''
	trashOpen.value = false
	trashEntries.value = []
	trashNext.value = ''
	backups.value = []
	backupsNext.value = ''
	backupFeedback.value = ''
	backupRepository.value = null
	selectedBackupFile.value = null
	backupUploadProgress.value = 0
	logDomainId.value = ''
	logPage.value = null
	logsFailed.value = false
	wafEventPage.value = null
	wafEventsFailed.value = false
	for (const key of Object.keys(cacheStatusByDomain)) delete cacheStatusByDomain[key]
	for (const key of Object.keys(cachePathPrefix)) delete cachePathPrefix[key]
	for (const key of Object.keys(cacheFeedback)) delete cacheFeedback[key]
	scheduledJobList.value = []
	scheduledJobFeedback.value = ''
	editingScheduledJobId.value = ''
	resetScheduledJobForm()
})

watch([() => props.page, () => props.selectedAccountId], ([page, accountID]) => {
  if (page === 'backups' && accountID) void loadBackups(true)
	if (page === 'logs' && accountID) {
		void loadLogs(true)
		void loadWAFEvents(true)
	}
	if (page === 'jobs' && accountID) void loadScheduledJobs()
}, { immediate: true })

watch(() => props.domains, (domains) => {
	if (logDomainId.value && domains.some((domain) => domain.id === logDomainId.value)) return
	logDomainId.value = domains[0]?.id ?? ''
	logPage.value = null
	wafEventPage.value = null
	if (props.page === 'logs' && logDomainId.value) {
		void loadLogs(true)
		void loadWAFEvents(true)
	}
}, { immediate: true, deep: true })

watch(availablePHPVersions, (versions) => {
  if (domainForm.targetType === 'php' && !versions.includes(domainForm.phpVersion)) {
    domainForm.phpVersion = versions[0] ?? ''
  }
}, { immediate: true })

function displayDate(value?: string): string {
  if (!value) return t('common.notAvailable')
  try {
    return formatDateTime(value, activeLocale.value)
  } catch {
    return t('common.notAvailable')
  }
}

function displayLimit(value?: number, kind: 'bytes' | 'percent' | 'number' = 'number'): string {
  if (value === undefined) return t('common.unlimited')
  if (kind === 'bytes') return formatBytes(value, activeLocale.value)
  if (kind === 'percent') return formatPercent(value / 100, activeLocale.value)
  return formatNumber(value, activeLocale.value)
}

function displayCPUTime(nanoseconds?: number): string {
  return nanoseconds === undefined
    ? t('common.notAvailable')
    : formatDuration(nanoseconds / 1_000_000_000, activeLocale.value)
}

function domainWAFMode(domain: Domain): Domain['waf']['mode'] {
  return domain.waf?.mode ?? 'off'
}

function domainCachePreset(domain: Domain): NonNullable<Domain['cache']>['preset'] {
  return domain.cache?.preset ?? 'disabled'
}

function cacheHitRatio(status: CacheStatus): number {
	const decisions = status.metrics.hits + status.metrics.misses
	return decisions === 0 ? 0 : status.metrics.hits / decisions
}

async function loadCacheStatus(domain: Domain) {
	const account = selectedAccount.value
	if (!account || cacheBusyDomainId.value || domainCachePreset(domain) === 'disabled') return
	cacheBusyDomainId.value = domain.id
	cacheFeedback[domain.id] = ''
	try {
		cacheStatusByDomain[domain.id] = await api.cacheStatus(account.id, domain.id)
		cachePathPrefix[domain.id] ??= '/'
	} catch {
		cacheFeedback[domain.id] = 'failed'
	} finally {
		cacheBusyDomainId.value = ''
	}
}

async function purgeDomainCache(domain: Domain) {
	const account = selectedAccount.value
	if (!account || cacheBusyDomainId.value) return
	cacheBusyDomainId.value = domain.id
	cacheFeedback[domain.id] = ''
	try {
		await api.purgeCache(account.id, domain.id, cachePathPrefix[domain.id] || '/')
		cacheFeedback[domain.id] = 'queued'
	} catch {
		cacheFeedback[domain.id] = 'failed'
	} finally {
		cacheBusyDomainId.value = ''
	}
}

function displayFileMode(mode: number): string {
  return mode.toString(8).padStart(4, '0')
}

async function loadLogs(reset: boolean) {
	const account = selectedAccount.value
	if (!account || !canViewLogs.value || logsLoading.value) return
	if (!logDomainId.value) logDomainId.value = props.domains[0]?.id ?? ''
	if (!logDomainId.value) return
	logsLoading.value = true
	logsFailed.value = false
	try {
		const page = await api.hostingLogs(account.id, logDomainId.value, logKind.value,
			reset ? '' : (logPage.value?.next ?? ''))
		logPage.value = reset || !logPage.value
			? page
			: { ...page, records: [...logPage.value.records, ...page.records] }
	} catch {
		logsFailed.value = true
		if (reset) logPage.value = null
	} finally {
		logsLoading.value = false
	}
}

function changeLogSelection() {
	logPage.value = null
	wafEventPage.value = null
	void loadLogs(true)
	void loadWAFEvents(true)
}

async function loadWAFEvents(reset: boolean) {
	const account = selectedAccount.value
	if (!account || !canViewLogs.value || wafEventsLoading.value) return
	if (!logDomainId.value) logDomainId.value = props.domains[0]?.id ?? ''
	if (!logDomainId.value) return
	wafEventsLoading.value = true
	wafEventsFailed.value = false
	try {
		const page = await api.wafEvents(account.id, logDomainId.value,
			reset ? '' : (wafEventPage.value?.next ?? ''))
		wafEventPage.value = reset || !wafEventPage.value
			? page
			: { ...page, events: [...wafEventPage.value.events, ...page.events] }
	} catch {
		wafEventsFailed.value = true
		if (reset) wafEventPage.value = null
	} finally {
		wafEventsLoading.value = false
	}
}

function resetScheduledJobForm() {
	scheduledJobForm.name = ''
	scheduledJobForm.runtime = 'shell'
	scheduledJobForm.scriptPath = 'jobs/task.sh'
	scheduledJobForm.phpVersion = ''
	scheduledJobForm.scheduleKind = 'hourly'
	scheduledJobForm.intervalMinutes = 15
	scheduledJobForm.hourUtc = 0
	scheduledJobForm.minuteUtc = 0
	scheduledJobForm.weekday = 'mon'
	scheduledJobForm.enabled = true
}

async function loadScheduledJobs() {
	const account = selectedAccount.value
	if (!account || scheduledJobsLoading.value) return
	scheduledJobsLoading.value = true
	try {
		scheduledJobList.value = await api.scheduledJobs(account.id)
		scheduledJobFailed.value = false
	} catch {
		scheduledJobFailed.value = true
		scheduledJobFeedback.value = t('jobs.loadFailed')
	} finally {
		scheduledJobsLoading.value = false
	}
}

function scheduledJobSchedule(): ScheduledJobSchedule {
	switch (scheduledJobForm.scheduleKind) {
		case 'interval':
			return { kind: 'interval', intervalMinutes: scheduledJobForm.intervalMinutes, hourUtc: 0, minuteUtc: 0 }
		case 'hourly':
			return { kind: 'hourly', intervalMinutes: 0, hourUtc: 0, minuteUtc: scheduledJobForm.minuteUtc }
		case 'daily':
			return { kind: 'daily', intervalMinutes: 0, hourUtc: scheduledJobForm.hourUtc, minuteUtc: scheduledJobForm.minuteUtc }
		default:
			return { kind: 'weekly', intervalMinutes: 0, hourUtc: scheduledJobForm.hourUtc,
				minuteUtc: scheduledJobForm.minuteUtc, weekday: scheduledJobForm.weekday }
	}
}

function scheduledJobInput(expectedRevision?: number): ScheduledJobInput {
	return {
		expectedRevision, name: scheduledJobForm.name, runtime: scheduledJobForm.runtime,
		scriptPath: scheduledJobForm.scriptPath,
		phpVersion: scheduledJobForm.runtime === 'php' ? scheduledJobForm.phpVersion : undefined,
		schedule: scheduledJobSchedule(), enabled: scheduledJobForm.enabled,
	}
}

async function submitScheduledJob() {
	const account = selectedAccount.value
	if (!account || !canManageScheduledJobs.value || scheduledJobBusy.value) return
	scheduledJobBusy.value = true
	scheduledJobFailed.value = false
	try {
		let mutation
		if (editingScheduledJobId.value) {
			const current = scheduledJobList.value.find((job) => job.id === editingScheduledJobId.value)
			if (!current) return
			mutation = await api.updateScheduledJob(account.id, current.id, scheduledJobInput(current.revision))
		} else {
			mutation = await api.createScheduledJob(account.id, scheduledJobInput())
		}
		scheduledJobFeedback.value = t('jobs.queued')
		const index = scheduledJobList.value.findIndex((job) => job.id === mutation.job.id)
		if (index >= 0) scheduledJobList.value[index] = mutation.job
		else scheduledJobList.value.push(mutation.job)
		editingScheduledJobId.value = ''
		resetScheduledJobForm()
		void waitForScheduledJob(account.id, mutation.operationId)
	} catch {
		scheduledJobFailed.value = true
		scheduledJobFeedback.value = t('jobs.mutationFailed')
	} finally {
		scheduledJobBusy.value = false
	}
}

function editScheduledJob(job: ScheduledJob) {
	editingScheduledJobId.value = job.id
	scheduledJobForm.name = job.name
	scheduledJobForm.runtime = job.runtime
	scheduledJobForm.scriptPath = job.scriptPath
	scheduledJobForm.phpVersion = job.phpVersion ?? ''
	scheduledJobForm.scheduleKind = job.schedule.kind
	scheduledJobForm.intervalMinutes = job.schedule.intervalMinutes
	scheduledJobForm.hourUtc = job.schedule.hourUtc
	scheduledJobForm.minuteUtc = job.schedule.minuteUtc
	scheduledJobForm.weekday = job.schedule.weekday ?? 'mon'
	scheduledJobForm.enabled = job.enabled
}

async function toggleScheduledJob(job: ScheduledJob) {
	const account = selectedAccount.value
	if (!account || !canManageScheduledJobs.value || scheduledJobBusy.value) return
	scheduledJobBusy.value = true
	try {
		const mutation = await api.updateScheduledJob(account.id, job.id, {
			expectedRevision: job.revision, name: job.name, runtime: job.runtime,
			scriptPath: job.scriptPath, phpVersion: job.phpVersion,
			schedule: job.schedule, enabled: !job.enabled,
		})
		scheduledJobFeedback.value = t('jobs.queued')
		void waitForScheduledJob(account.id, mutation.operationId)
		await loadScheduledJobs()
	} catch {
		scheduledJobFailed.value = true
		scheduledJobFeedback.value = t('jobs.mutationFailed')
	} finally {
		scheduledJobBusy.value = false
	}
}

async function deleteScheduledJob(job: ScheduledJob) {
	const account = selectedAccount.value
	if (!account || !canManageScheduledJobs.value || scheduledJobBusy.value ||
		!window.confirm(t('jobs.deletePrompt', { name: job.name }))) return
	scheduledJobBusy.value = true
	try {
		const mutation = await api.deleteScheduledJob(account.id, job.id, job.revision)
		scheduledJobFeedback.value = t('jobs.queued')
		void waitForScheduledJob(account.id, mutation.operationId)
		await loadScheduledJobs()
	} catch {
		scheduledJobFailed.value = true
		scheduledJobFeedback.value = t('jobs.mutationFailed')
	} finally {
		scheduledJobBusy.value = false
	}
}

async function waitForScheduledJob(accountId: string, operationId: string) {
	for (let attempt = 0; attempt < 90; attempt += 1) {
		await new Promise((resolve) => window.setTimeout(resolve, 1000))
		if (selectedAccount.value?.id !== accountId) return
		try {
			const operation = await api.scheduledJobOperation(accountId, operationId)
			if (!accountOperationTerminal(operation.status)) continue
			await loadScheduledJobs()
			scheduledJobFailed.value = operation.status !== 'succeeded'
			scheduledJobFeedback.value = operation.status === 'succeeded' ? t('jobs.applied') : t('jobs.operationFailed')
			return
		} catch {
			return
		}
	}
}

function formatScheduledJobSchedule(schedule: ScheduledJobSchedule): string {
	if (schedule.kind === 'interval') return t('jobs.everyMinutes', { minutes: schedule.intervalMinutes })
	const time = `${String(schedule.hourUtc).padStart(2, '0')}:${String(schedule.minuteUtc).padStart(2, '0')} UTC`
	if (schedule.kind === 'hourly') return t('jobs.hourlyAt', { minute: String(schedule.minuteUtc).padStart(2, '0') })
	if (schedule.kind === 'weekly') return t('jobs.weeklyAt', { weekday: t(`jobs.weekdays.${schedule.weekday}`), time })
	return t('jobs.dailyAt', { time })
}

function openFileDirectory(entry: FileEntry) {
  if (!selectedAccount.value || !canBrowseFiles.value || entry.type !== 'directory') return
  const path = props.fileListing.path ? `${props.fileListing.path}/${entry.name}` : entry.name
  emit('loadFiles', { accountId: selectedAccount.value.id, path })
}

function openParentDirectory() {
  if (!selectedAccount.value || !canBrowseFiles.value || !props.fileListing.path) return
  const components = props.fileListing.path.split('/')
  components.pop()
  emit('loadFiles', { accountId: selectedAccount.value.id, path: components.join('/') })
}

function fileDownloadPath(entry: FileEntry): string {
  if (!selectedAccount.value || entry.type !== 'file') return '#'
  const path = props.fileListing.path ? `${props.fileListing.path}/${entry.name}` : entry.name
  return api.fileDownloadURL(selectedAccount.value.id, path)
}

function loadNextFilePage() {
  if (!selectedAccount.value || !canBrowseFiles.value || !props.fileListing.next) return
  emit('loadFiles', {
    accountId: selectedAccount.value.id, path: props.fileListing.path, cursor: props.fileListing.next,
  })
}

function selectUploadFile(event: Event) {
	const input = event.target as HTMLInputElement
	selectedUploadFile.value = input.files?.[0] ?? null
	fileMutationFeedback.value = ''
}

function uploadStorageKey(accountId: string): string {
	return `stackfort:file-upload:${accountId}`
}

async function uploadSelectedFile() {
	const account = selectedAccount.value
	const file = selectedUploadFile.value
	if (!account || !file || !canMutateFiles.value || fileMutationBusy.value) return
	if (file.size > 4 * 1024 * 1024 * 1024) {
		fileMutationFailed.value = true
		fileMutationFeedback.value = t('files.uploadTooLarge')
		return
	}
	fileMutationBusy.value = true
	fileMutationFailed.value = false
	fileMutationFeedback.value = t('files.uploadPreparing')
	uploadProgress.value = 0
	uploadCancelRequested = false
	uploadAbortController = new AbortController()
	const directory = props.fileListing.path
	const storageKey = uploadStorageKey(account.id)
	let uploadId = ''
	try {
		let upload: Awaited<ReturnType<typeof api.initiateFileUpload>> | null = null
		const stored = sessionStorage.getItem(storageKey)
		if (stored) {
			try {
				const candidate = JSON.parse(stored) as {
					uploadId?: string; directory?: string; name?: string; sizeBytes?: number; lastModified?: number
				}
				if (candidate.uploadId && candidate.directory === directory && candidate.name === file.name &&
					candidate.sizeBytes === file.size && candidate.lastModified === file.lastModified) {
					upload = await api.fileUploadStatus(account.id, candidate.uploadId)
				} else if (candidate.uploadId) {
					await api.cancelFileUpload(account.id, candidate.uploadId).catch(() => undefined)
				}
			} catch {
				sessionStorage.removeItem(storageKey)
			}
		}
		if (!upload) {
			upload = await api.initiateFileUpload(account.id, {
				directory, name: file.name, sizeBytes: file.size,
			})
		}
		uploadId = upload.uploadId
		activeUploadId.value = uploadId
		sessionStorage.setItem(storageKey, JSON.stringify({
			uploadId, directory, name: file.name, sizeBytes: file.size, lastModified: file.lastModified,
		}))
		let offset = upload.receivedBytes
		if (offset > file.size) throw new Error('invalid upload offset')
		uploadProgress.value = file.size === 0 ? 100 : Math.floor((offset / file.size) * 100)
		while (offset < file.size) {
			fileMutationFeedback.value = t('files.uploading')
			const end = Math.min(file.size, offset + (8 * 1024 * 1024))
			const result = await api.uploadFileChunk(
				account.id, uploadId, offset, file.slice(offset, end), uploadAbortController.signal,
			)
			if (result.receivedBytes <= offset || result.receivedBytes > file.size) throw new Error('invalid upload progress')
			offset = result.receivedBytes
			uploadProgress.value = Math.floor((offset / file.size) * 100)
		}
		fileMutationFeedback.value = t('files.uploadValidating')
		await api.completeFileUpload(account.id, uploadId, { directory, name: file.name, sizeBytes: file.size })
		sessionStorage.removeItem(storageKey)
		uploadProgress.value = 100
		fileMutationFeedback.value = t('files.uploadComplete', { name: file.name })
		selectedUploadFile.value = null
		emit('loadFiles', { accountId: account.id, path: directory })
	} catch (error) {
		if (uploadCancelRequested) {
			if (uploadId) await api.cancelFileUpload(account.id, uploadId).catch(() => undefined)
			sessionStorage.removeItem(storageKey)
			fileMutationFailed.value = false
			fileMutationFeedback.value = t('files.uploadCanceled')
		} else {
			fileMutationFailed.value = true
			const detail = error instanceof ApiError ? t(`errors.${error.code}`) : ''
			fileMutationFeedback.value = detail && detail !== `errors.${error instanceof ApiError ? error.code : ''}`
				? `${t('files.uploadFailed')} ${detail}` : t('files.uploadFailedResume')
		}
	} finally {
		fileMutationBusy.value = false
		activeUploadId.value = ''
		uploadAbortController = null
		uploadCancelRequested = false
	}
}

function cancelActiveUpload() {
	if (!fileMutationBusy.value || !activeUploadId.value) return
	uploadCancelRequested = true
	uploadAbortController?.abort()
}

async function createFileNode(type: 'file' | 'directory') {
	const account = selectedAccount.value
	const name = fileNodeName.value
	if (!account || !canMutateFiles.value || fileMutationBusy.value || !name) return
	fileMutationBusy.value = true
	fileMutationFailed.value = false
	fileMutationFeedback.value = t('files.creating')
	try {
		await api.createFileNode(account.id, { directory: props.fileListing.path, name, type })
		fileNodeName.value = ''
		fileMutationFeedback.value = type === 'file'
			? t('files.fileCreated', { name }) : t('files.directoryCreated', { name })
		emit('loadFiles', { accountId: account.id, path: props.fileListing.path })
	} catch (error) {
		fileMutationFailed.value = true
		const detail = error instanceof ApiError ? t(`errors.${error.code}`) : ''
		fileMutationFeedback.value = detail && detail !== `errors.${error instanceof ApiError ? error.code : ''}`
			? detail : t('files.createFailed')
	} finally {
		fileMutationBusy.value = false
	}
}

function fileMutationError(error: unknown, fallback: string): string {
	const detail = error instanceof ApiError ? t(`errors.${error.code}`) : ''
	return detail && detail !== `errors.${error instanceof ApiError ? error.code : ''}` ? detail : t(fallback)
}

async function mutateFileEntry(entry: FileEntry, action: 'rename' | 'move' | 'copy') {
	const account = selectedAccount.value
	if (!account || !canMutateFiles.value || fileMutationBusy.value ||
		(entry.type !== 'file' && entry.type !== 'directory')) return
	const sourceDirectory = props.fileListing.path
	let destinationDirectory = sourceDirectory
	let destinationName = entry.name
	if (action === 'rename') {
		const value = window.prompt(t('files.renamePrompt', { name: entry.name }), entry.name)
		if (value === null || value === entry.name) return
		destinationName = value
	} else {
		const directory = window.prompt(t(action === 'move' ? 'files.movePrompt' : 'files.copyPrompt', { name: entry.name }), sourceDirectory)
		if (directory === null) return
		destinationDirectory = directory
		if (action === 'copy') {
			const name = window.prompt(t('files.copyNamePrompt'), entry.name)
			if (name === null) return
			destinationName = name
		}
	}
	fileMutationBusy.value = true
	fileMutationFailed.value = false
	fileMutationFeedback.value = t(`files.${action}Running`)
	try {
		await api.mutateFileNode(account.id, {
			action, sourceDirectory, sourceName: entry.name, destinationDirectory, destinationName,
		})
		fileMutationFeedback.value = t(`files.${action}Complete`, { name: entry.name })
		emit('loadFiles', { accountId: account.id, path: sourceDirectory })
	} catch (error) {
		fileMutationFailed.value = true
		fileMutationFeedback.value = fileMutationError(error, 'files.operationFailed')
	} finally {
		fileMutationBusy.value = false
	}
}

function archiveFormatForName(name: string): 'zip' | 'tar_gzip' | null {
	if (name.endsWith('.tar.gz')) return 'tar_gzip'
	if (name.endsWith('.zip')) return 'zip'
	return null
}

async function createFileArchive(entry: FileEntry) {
	const account = selectedAccount.value
	if (!account || !canMutateFiles.value || fileMutationBusy.value ||
		(entry.type !== 'file' && entry.type !== 'directory')) return
	const selectedFormat = window.prompt(t('files.archiveFormatPrompt'), 'zip')
	if (selectedFormat === null) return
	const format = selectedFormat === 'zip' ? 'zip' : selectedFormat === 'tar.gz' ? 'tar_gzip' : null
	if (!format) {
		fileMutationFailed.value = true
		fileMutationFeedback.value = t('files.archiveFormatInvalid')
		return
	}
	const suffix = format === 'zip' ? '.zip' : '.tar.gz'
	const destinationName = window.prompt(t('files.archiveNamePrompt', { name: entry.name }), `${entry.name}${suffix}`)
	if (destinationName === null) return
	fileMutationBusy.value = true
	fileMutationFailed.value = false
	fileMutationFeedback.value = t('files.archiveCreating')
	try {
		await api.mutateFileArchive(account.id, {
			action: 'create', format, sourceDirectory: props.fileListing.path, sourceName: entry.name,
			destinationDirectory: props.fileListing.path, destinationName,
		})
		fileMutationFeedback.value = t('files.archiveCreated', { name: destinationName })
		emit('loadFiles', { accountId: account.id, path: props.fileListing.path })
	} catch (error) {
		fileMutationFailed.value = true
		fileMutationFeedback.value = fileMutationError(error, 'files.archiveFailed')
	} finally {
		fileMutationBusy.value = false
	}
}

async function extractFileArchive(entry: FileEntry) {
	const account = selectedAccount.value
	const format = archiveFormatForName(entry.name)
	if (!account || !format || !canMutateFiles.value || fileMutationBusy.value || entry.type !== 'file') return
	const suffixLength = format === 'zip' ? 4 : 7
	const suggestedName = entry.name.slice(0, -suffixLength)
	const destinationName = window.prompt(t('files.extractNamePrompt', { name: entry.name }), suggestedName)
	if (destinationName === null) return
	fileMutationBusy.value = true
	fileMutationFailed.value = false
	fileMutationFeedback.value = t('files.archiveExtracting')
	try {
		await api.mutateFileArchive(account.id, {
			action: 'extract', format, sourceDirectory: props.fileListing.path, sourceName: entry.name,
			destinationDirectory: props.fileListing.path, destinationName,
		})
		fileMutationFeedback.value = t('files.archiveExtracted', { name: destinationName })
		emit('loadFiles', { accountId: account.id, path: props.fileListing.path })
	} catch (error) {
		fileMutationFailed.value = true
		fileMutationFeedback.value = fileMutationError(error, 'files.archiveFailed')
	} finally {
		fileMutationBusy.value = false
	}
}

async function trashFileEntry(entry: FileEntry) {
	const account = selectedAccount.value
	if (!account || !canMutateFiles.value || fileMutationBusy.value ||
		(entry.type !== 'file' && entry.type !== 'directory') ||
		!window.confirm(t('files.trashPrompt', { name: entry.name }))) return
	fileMutationBusy.value = true
	fileMutationFailed.value = false
	fileMutationFeedback.value = t('files.trashRunning')
	try {
		await api.trashFileNode(account.id, { directory: props.fileListing.path, name: entry.name })
		fileMutationFeedback.value = t('files.trashComplete', { name: entry.name })
		emit('loadFiles', { accountId: account.id, path: props.fileListing.path })
		if (trashOpen.value) await loadFileTrash(true)
	} catch (error) {
		fileMutationFailed.value = true
		fileMutationFeedback.value = fileMutationError(error, 'files.operationFailed')
	} finally {
		fileMutationBusy.value = false
	}
}

async function toggleFileTrash() {
	trashOpen.value = !trashOpen.value
	if (trashOpen.value) await loadFileTrash(true)
}

async function loadFileTrash(reset = false) {
	const account = selectedAccount.value
	if (!account || trashLoading.value) return
	trashLoading.value = true
	try {
		const listing = await api.fileTrash(account.id, reset ? '' : trashNext.value)
		trashEntries.value = reset ? listing.trashEntries : [...trashEntries.value, ...listing.trashEntries]
		trashNext.value = listing.next ?? ''
	} catch (error) {
		fileMutationFailed.value = true
		fileMutationFeedback.value = fileMutationError(error, 'files.trashLoadFailed')
	} finally {
		trashLoading.value = false
	}
}

async function restoreTrashEntry(entry: FileTrashEntry) {
	const account = selectedAccount.value
	if (!account || !canMutateFiles.value || fileMutationBusy.value) return
	fileMutationBusy.value = true
	fileMutationFailed.value = false
	fileMutationFeedback.value = t('files.restoreRunning')
	try {
		await api.restoreTrash(account.id, entry.trashId)
		fileMutationFeedback.value = t('files.restoreComplete', { name: entry.name })
		await loadFileTrash(true)
		emit('loadFiles', { accountId: account.id, path: props.fileListing.path })
	} catch (error) {
		fileMutationFailed.value = true
		fileMutationFeedback.value = fileMutationError(error, 'files.operationFailed')
	} finally {
		fileMutationBusy.value = false
	}
}

async function purgeTrashEntry(entry: FileTrashEntry) {
	const account = selectedAccount.value
	if (!account || !canMutateFiles.value || fileMutationBusy.value ||
		!window.confirm(t('files.purgePrompt', { name: entry.name }))) return
	fileMutationBusy.value = true
	fileMutationFailed.value = false
	fileMutationFeedback.value = t('files.purgeRunning')
	try {
		await api.purgeTrash(account.id, entry.trashId)
		fileMutationFeedback.value = t('files.purgeComplete', { name: entry.name })
		await loadFileTrash(true)
	} catch (error) {
		fileMutationFailed.value = true
		fileMutationFeedback.value = fileMutationError(error, 'files.operationFailed')
	} finally {
		fileMutationBusy.value = false
	}
}

async function loadBackups(reset = false) {
	const account = selectedAccount.value
	if (!account || !canViewBackups.value || backupsLoading.value) return
	backupsLoading.value = true
	if (reset) {
		backups.value = []
		backupsNext.value = ''
	}
	try {
		const result = await api.backups(account.id, reset ? '' : backupsNext.value)
		backups.value = reset ? (result.backups ?? []) : [...backups.value, ...(result.backups ?? [])]
		backupsNext.value = result.next ?? ''
		backupRepository.value = result.backupRepository ?? backupRepository.value
	} catch (error) {
		backupFailed.value = true
		backupFeedback.value = fileMutationError(error, 'backups.loadFailed')
	} finally {
		backupsLoading.value = false
	}
}

function backupDownloadPath(item: BackupRecord): string {
	const account = selectedAccount.value
	return account ? api.backupDownloadURL(account.id, item.backupId) : '#'
}

function selectBackupImport(event: Event) {
	const input = event.target as HTMLInputElement
	selectedBackupFile.value = input.files?.[0] ?? null
	backupFeedback.value = ''
}

function backupUploadStorageKey(accountId: string): string { return `stackfort:backup-upload:${accountId}` }

async function importBackup() {
	const account = selectedAccount.value
	const file = selectedBackupFile.value
	if (!account || !file || !canCreateBackups.value || backupBusy.value) return
	if (file.size <= 0 || file.size > 4 * 1024 * 1024 * 1024 || !file.name.endsWith('.tar.gz')) {
		backupFailed.value = true
		backupFeedback.value = t('backups.importInvalid')
		return
	}
	backupBusy.value = true
	backupFailed.value = false
	backupFeedback.value = t('backups.uploadPreparing')
	backupUploadProgress.value = 0
	backupUploadCancelRequested = false
	backupUploadAbortController = new AbortController()
	const storageKey = backupUploadStorageKey(account.id)
	let uploadId = ''
	try {
		const input = { scope: backupScope.value, sourcePath: backupScope.value === 'document_root' ? backupSourcePath.value : undefined,
			sizeBytes: file.size }
		let upload: Awaited<ReturnType<typeof api.initiateBackupUpload>> | null = null
		const stored = sessionStorage.getItem(storageKey)
		if (stored) {
			try {
				const candidate = JSON.parse(stored) as { uploadId?: string; sizeBytes?: number; lastModified?: number }
				if (candidate.uploadId && candidate.sizeBytes === file.size && candidate.lastModified === file.lastModified)
					upload = await api.backupUploadStatus(account.id, candidate.uploadId)
				else if (candidate.uploadId) await api.cancelBackupUpload(account.id, candidate.uploadId).catch(() => undefined)
			} catch { sessionStorage.removeItem(storageKey) }
		}
		if (!upload) upload = await api.initiateBackupUpload(account.id, input)
		uploadId = upload.uploadId ?? ''
		if (!uploadId) throw new Error('missing backup upload id')
		activeBackupUploadId.value = uploadId
		sessionStorage.setItem(storageKey, JSON.stringify({ uploadId, sizeBytes: file.size, lastModified: file.lastModified }))
		let offset = upload.receivedBytes ?? 0
		while (offset < file.size) {
			backupFeedback.value = t('backups.uploading')
			const end = Math.min(file.size, offset + 8 * 1024 * 1024)
			const result = await api.uploadBackupChunk(account.id, uploadId, offset, file.slice(offset, end), backupUploadAbortController.signal)
			const next = result.receivedBytes ?? 0
			if (next <= offset || next > file.size) throw new Error('invalid backup upload progress')
			offset = next
			backupUploadProgress.value = Math.floor((offset / file.size) * 100)
		}
		backupFeedback.value = t('backups.importVerifying')
		await api.completeBackupUpload(account.id, uploadId, input)
		sessionStorage.removeItem(storageKey)
		selectedBackupFile.value = null
		backupUploadProgress.value = 100
		backupFeedback.value = t('backups.imported')
		await loadBackups(true)
	} catch (error) {
		if (backupUploadCancelRequested) {
			if (uploadId) await api.cancelBackupUpload(account.id, uploadId).catch(() => undefined)
			sessionStorage.removeItem(storageKey)
			backupFeedback.value = t('backups.importCanceled')
		} else {
			backupFailed.value = true
			backupFeedback.value = fileMutationError(error, 'backups.importFailedResume')
		}
	} finally {
		backupBusy.value = false
		activeBackupUploadId.value = ''
		backupUploadAbortController = null
		backupUploadCancelRequested = false
	}
}

function cancelBackupImport() {
	if (!backupBusy.value || !activeBackupUploadId.value) return
	backupUploadCancelRequested = true
	backupUploadAbortController?.abort()
}

async function deleteBackup(item: BackupRecord) {
	const account = selectedAccount.value
	if (!account || !canDeleteBackups.value || backupBusy.value) return
	const confirmation = window.prompt(t('backups.deletePrompt', { id: item.backupId }))
	if (confirmation === null) return
	if (confirmation !== item.backupId) {
		window.alert(t('backups.deleteMismatch'))
		return
	}
	backupBusy.value = true
	backupFailed.value = false
	try {
		await api.deleteBackup(account.id, item.backupId, confirmation)
		backupFeedback.value = t('backups.deleted')
		await loadBackups(true)
	} catch (error) {
		backupFailed.value = true
		backupFeedback.value = fileMutationError(error, 'backups.deleteFailed')
	} finally { backupBusy.value = false }
}

async function createBackup() {
	const account = selectedAccount.value
	if (!account || !canCreateBackups.value || backupBusy.value) return
	backupBusy.value = true
	backupFailed.value = false
	backupFeedback.value = t('backups.creating')
	try {
		await api.createBackup(account.id, {
			scope: backupScope.value,
			sourcePath: backupScope.value === 'document_root' ? backupSourcePath.value : undefined,
		})
		backupFeedback.value = t('backups.created')
		await loadBackups(true)
	} catch (error) {
		backupFailed.value = true
		backupFeedback.value = fileMutationError(error, 'backups.createFailed')
	} finally {
		backupBusy.value = false
	}
}

async function verifyBackup(item: BackupRecord) {
	const account = selectedAccount.value
	if (!account || !canViewBackups.value || backupBusy.value) return
	backupBusy.value = true
	backupFailed.value = false
	backupFeedback.value = t('backups.verifying')
	try {
		const result = await api.verifyBackup(account.id, item.backupId)
		if (result.backup) {
			backups.value = backups.value.map((candidate) =>
				candidate.backupId === item.backupId ? result.backup as BackupRecord : candidate)
		}
		backupFeedback.value = t('backups.verified')
	} catch (error) {
		backupFailed.value = true
		backupFeedback.value = fileMutationError(error, 'backups.verifyFailed')
	} finally {
		backupBusy.value = false
	}
}

async function restoreBackup(item: BackupRecord) {
	const account = selectedAccount.value
	if (!account || !canRestoreBackups.value || backupBusy.value) return
	const confirmation = window.prompt(t('backups.restorePrompt', { id: item.backupId }))
	if (confirmation === null) return
	if (confirmation !== item.backupId) {
		window.alert(t('backups.restoreMismatch'))
		return
	}
	backupBusy.value = true
	backupFailed.value = false
	backupFeedback.value = t('backups.restoring')
	try {
		await api.restoreBackup(account.id, item.backupId, confirmation)
		backupFeedback.value = t('backups.restored')
		await loadBackups(true)
	} catch (error) {
		backupFailed.value = true
		backupFeedback.value = fileMutationError(error, 'backups.restoreFailed')
	} finally {
		backupBusy.value = false
	}
}

function targetInput(form: {
  targetType: 'static' | 'php'; phpVersion: string
  rootMode: 'default' | 'custom'; documentRoot: string
}): DomainTargetInput {
	const target: DomainTargetInput = {
		type: form.targetType,
		rootMode: form.rootMode,
	}
	if (form.rootMode === 'custom') target.documentRoot = form.documentRoot
	if (form.targetType === 'php') target.phpVersion = form.phpVersion
	return target
}

function operationLabel(kind: string): string {
  if (kind === 'tls.certificate.lifecycle') return t('operations.certificateLifecycle')
  if (kind === 'domain.lifecycle.apply') return t('operations.domainLifecycle')
  if (kind === 'database.lifecycle') return t('operations.databaseLifecycle')
  return t('operations.accountWork')
}

function resetDatabaseWizard() {
  databaseWizard.step = 1
  databaseWizard.databaseAlias = ''
  databaseWizard.userMode = 'new'
  databaseWizard.existingUserId = ''
  databaseWizard.newUserAlias = ''
  databaseWizard.preset = 'read_write'
}

function nextDatabaseWizardStep() {
  if (databaseWizard.step === 1 && !databaseWizard.newUserAlias) {
    databaseWizard.newUserAlias = databaseWizard.databaseAlias
  }
  if (databaseWizard.step === 2 && databaseWizard.userMode === 'existing' && !databaseWizard.existingUserId) return
  databaseWizard.step = Math.min(4, databaseWizard.step + 1)
}

function previousDatabaseWizardStep() {
  databaseWizard.step = Math.max(1, databaseWizard.step - 1)
}

function submitDatabaseWizard() {
  if (!selectedAccount.value || !canManageDatabases.value || databaseWizard.step !== 4) return
  emit('createDatabase', {
    accountId: selectedAccount.value.id,
    databaseAlias: databaseWizard.databaseAlias,
    existingUserId: databaseWizard.userMode === 'existing' ? databaseWizard.existingUserId : undefined,
    newUserAlias: databaseWizard.userMode === 'new' ? databaseWizard.newUserAlias : undefined,
    preset: databaseWizard.preset,
  })
  resetDatabaseWizard()
}

function revealDatabaseCredential(user: ManagedDatabaseUser) {
  if (selectedAccount.value && canManageDatabases.value && !user.revealed) {
    emit('revealDatabaseCredential', { accountId: selectedAccount.value.id, userId: user.id })
  }
}

function launchPHPMyAdmin(user: ManagedDatabaseUser) {
  if (selectedAccount.value && canManageDatabases.value &&
    props.databaseWorkspace.grants.some((grant) => grant.databaseUserId === user.id && grant.status === 'active')) {
    emit('launchPhpMyAdmin', { accountId: selectedAccount.value.id, userId: user.id })
  }
}

function rotateDatabaseCredential(user: ManagedDatabaseUser) {
  if (!selectedAccount.value || !canManageDatabases.value) return
  if (!window.confirm(t('databases.rotatePrompt', { alias: user.alias }))) return
  emit('rotateDatabaseCredential', { accountId: selectedAccount.value.id, userId: user.id })
}

function deleteDatabaseTarget(targetKind: 'database' | 'user', target: { id: string; alias: string }) {
  if (!selectedAccount.value || !canDeleteDatabases.value) return
  const confirmation = window.prompt(t('databases.deletePrompt', { alias: target.alias }))
  if (confirmation !== target.alias) {
    if (confirmation !== null) window.alert(t('databases.deleteMismatch'))
    return
  }
  emit('deleteDatabaseTarget', {
    accountId: selectedAccount.value.id, targetKind, targetId: target.id, confirmation,
  })
}

function toggleCertificateHistory(domain: Domain) {
  if (certificateHistoryDomainId.value === domain.id) {
    certificateHistoryDomainId.value = ''
    return
  }
  certificateHistoryDomainId.value = domain.id
  if (selectedAccount.value && !Object.hasOwn(props.certificateHistory, domain.id)) {
    emit('loadCertificateHistory', { accountId: selectedAccount.value.id, domainId: domain.id })
  }
}

function changeAccount(event: Event) {
  emit('selectAccount', (event.target as HTMLSelectElement).value)
  editingDomainId.value = ''
}

function submitDomain() {
  if (!selectedAccount.value?.hostReady || !canCreateDomain.value) return
  emit('createDomain', {
    accountId: selectedAccount.value.id,
    name: domainForm.name,
    canonicalMode: domainForm.canonicalMode,
    target: targetInput(domainForm),
    disableTls: !domainForm.tls,
    tlsMode: domainForm.tls ? 'acme' : undefined,
    wafMode: domainForm.wafMode,
    cachePreset: domainForm.targetType === 'php' ? domainForm.cachePreset : 'disabled',
  })
}

function startEditing(domain: Domain) {
  editingDomainId.value = domain.id
  editForm.canonicalMode = domain.canonicalMode
  editForm.targetType = domain.target.type === 'php' ? 'php' : 'static'
  editForm.phpVersion = domain.target.phpVersion ?? availablePHPVersions.value[0] ?? ''
  editForm.documentRoot = domain.target.documentRoot?.relativePath ?? ''
  editForm.rootMode = editForm.documentRoot === 'public_html' ? 'default' : 'custom'
  editForm.wafMode = domainWAFMode(domain)
  editForm.cachePreset = domainCachePreset(domain)
}

function submitDomainEdit(domain: Domain) {
  if (!selectedAccount.value || !canSubmitEdit.value) return
  emit('updateDomain', {
    accountId: selectedAccount.value.id,
    domainId: domain.id,
    canonicalMode: editForm.canonicalMode,
    target: targetInput(editForm),
    wafMode: editForm.wafMode,
    cachePreset: editForm.targetType === 'php' ? editForm.cachePreset : 'disabled',
  })
  editingDomainId.value = ''
}

function runDomainAction(domain: Domain, action: 'suspend' | 'resume' | 'remove') {
  if (!selectedAccount.value || !canManageDomains.value || (action === 'remove' && !canRemoveDomains.value)) return
  if (action === 'remove' && !window.confirm(t('domains.confirmRemove', { domain: domain.name.display }))) return
  emit('domainAction', { accountId: selectedAccount.value.id, domainId: domain.id, action })
}

function issueCertificate(domain: Domain) {
  if (selectedAccount.value && canManageDomains.value) {
    emit('issueCertificate', { accountId: selectedAccount.value.id, domainId: domain.id })
  }
}

function submitProfile() {
  emit('updateProfile', { ...profileForm })
}

function revokeManagedSession(item: ManagedSession) {
  const key = item.current ? 'sessions.confirmCurrent' : 'sessions.confirmRevoke'
  if (window.confirm(t(key))) emit('revokeSession', item.id)
}
</script>

<template>
  <div class="admin-content account-content">
    <div class="content-toolbar">
      <p v-if="noticeCode" class="inline-feedback success" role="status" aria-live="polite">{{ t(`notices.${noticeCode}`) }}</p>
      <p v-if="errorCode" class="inline-feedback error" role="alert">{{ t(`errors.${errorCode}`) }}</p>
      <label v-if="accounts.length > 1" class="account-picker compact-picker">
        <span>{{ t('domains.account') }}</span>
        <select :value="selectedAccountId" @change="changeAccount">
          <option v-for="account in accounts" :key="account.id" :value="account.id">{{ account.name }}</option>
        </select>
      </label>
      <button class="secondary-action" type="button" :disabled="loading" @click="emit('refresh')">
        {{ loading ? t('common.loading') : t('common.refresh') }}
      </button>
    </div>

    <div v-if="accounts.length === 0" class="panel empty-state owner-empty">
      <span class="empty-icon" aria-hidden="true">◇</span>
      <strong>{{ t('account.noAccounts') }}</strong>
      <p>{{ t('account.noAccountsBody') }}</p>
    </div>

    <template v-else-if="selectedAccount">
      <template v-if="page === 'overview'">
        <section class="metric-grid" :aria-label="t('account.summary')">
          <article class="metric-card"><span>{{ t('dashboard.domains') }}</span><strong>{{ formatNumber(selectedAccount.usage.domains, activeLocale) }}</strong><small>{{ t('usage.ofDomains', { limit: formatNumber(selectedAccount.effectiveLimits.maxDomains, activeLocale) }) }}</small></article>
          <article class="metric-card"><span>{{ t('domains.tls') }}</span><strong>{{ formatNumber(activeTLS, activeLocale) }}</strong><small>{{ t('account.activeCertificates') }}</small></article>
          <article class="metric-card"><span>{{ t('accounts.package') }}</span><strong class="metric-text">{{ selectedAccount.packageName }}</strong><small>{{ t('account.packageRevision', { revision: selectedAccount.packageRevision }) }}</small></article>
          <article class="metric-card"><span>{{ t('account.role') }}</span><strong class="metric-text">{{ t(`roles.${selectedAccount.membershipRole}`) }}</strong><small>{{ t(`states.${selectedAccount.status}`) }}</small></article>
        </section>
        <section class="dashboard-grid admin-dashboard">
          <article class="panel data-panel usage-overview">
            <div class="panel-heading"><h2>{{ t('usage.domainUsage') }}</h2><span class="state-badge" :data-state="selectedAccount.status">{{ t(`states.${selectedAccount.status}`) }}</span></div>
            <div class="large-progress"><div><strong>{{ formatNumber(selectedAccount.usage.domains, activeLocale) }}</strong><span>{{ t('usage.ofDomains', { limit: formatNumber(selectedAccount.effectiveLimits.maxDomains, activeLocale) }) }}</span></div><progress max="1" :value="domainPercent" :aria-label="t('usage.domainUsage')">{{ formatPercent(domainPercent, activeLocale) }}</progress></div>
            <p>{{ t('usage.measuredHint') }}</p>
          </article>
          <article class="panel data-panel">
            <div class="panel-heading"><h2>{{ t('account.details') }}</h2></div>
            <dl class="detail-list">
              <div><dt>{{ t('common.name') }}</dt><dd>{{ selectedAccount.name }}</dd></div>
              <div><dt>{{ t('common.slug') }}</dt><dd><code>{{ selectedAccount.slug }}</code></dd></div>
              <div><dt>{{ t('accounts.package') }}</dt><dd>{{ selectedAccount.packageName }}</dd></div>
              <div><dt>{{ t('common.updated') }}</dt><dd>{{ displayDate(selectedAccount.updatedAt) }}</dd></div>
            </dl>
          </article>
        </section>
      </template>

      <section v-else-if="page === 'domains'" class="domain-workspace">
        <div v-if="selectedAccount.status === 'suspended'" class="inline-feedback error" role="status">{{ t('account.suspendedReadOnly') }}</div>
        <div v-else-if="!selectedAccount.hostReady" class="inline-feedback" role="status">{{ t('account.provisioningReadOnly') }}</div>
        <div v-if="trackedOperation" class="panel account-operation" role="status" aria-live="polite">
          <div class="operation-summary">
            <div><span>{{ operationLabel(trackedOperation.kind) }}</span><strong>{{ t(`states.${trackedOperation.status}`) }}</strong></div>
            <span class="state-badge" :data-state="trackedOperation.status">{{ trackedOperation.progressPercent }}%</span>
          </div>
          <progress max="100" :value="trackedOperation.progressPercent" :aria-label="t('operations.progress')">{{ trackedOperation.progressPercent }}%</progress>
          <small>{{ t('operations.attempts', { current: trackedOperation.attemptCount, maximum: trackedOperation.maxAttempts }) }}</small>
        </div>
        <section class="panel php-runtime-panel" aria-labelledby="php-runtime-title">
          <div class="panel-heading">
            <div><p class="eyebrow">{{ t('php.eyebrow') }}</p><h2 id="php-runtime-title">{{ t('php.title') }}</h2></div>
            <span class="state-badge" :data-state="phpStatus?.runtimeCapability.status ?? 'unavailable'">{{ t(`states.${phpStatus?.runtimeCapability.status ?? 'unavailable'}`) }}</span>
          </div>
          <p v-if="!phpStatus">{{ t('php.statusUnavailable') }}</p>
          <p v-else-if="phpStatus.runtimeCapability.status !== 'available'">{{ t('php.runtimeUnavailable') }}</p>
          <template v-else>
            <p>{{ availablePHPVersions.length > 0 ? t('php.availableVersions', { versions: availablePHPVersions.join(', ') }) : t('php.notIncluded') }}</p>
            <div class="php-pool-list">
              <article v-for="pool in phpStatus.pools" :key="pool.version" class="php-pool-card">
                <header><strong>{{ t('php.version', { version: pool.version }) }}</strong><span class="state-badge" :data-state="pool.state">{{ t(`states.${pool.state}`) }}</span></header>
                <dl class="detail-list compact">
                  <div><dt>{{ t('php.domains') }}</dt><dd>{{ formatNumber(pool.configuredDomains, activeLocale) }}</dd></div>
                  <div><dt>{{ t('php.memory') }}</dt><dd>{{ pool.memoryBytes === undefined ? t('common.notAvailable') : formatBytes(pool.memoryBytes, activeLocale) }}</dd></div>
                  <div><dt>{{ t('php.processes') }}</dt><dd>{{ pool.processes === undefined ? t('common.notAvailable') : formatNumber(pool.processes, activeLocale) }}</dd></div>
                  <div><dt>{{ t('php.cpuTime') }}</dt><dd>{{ displayCPUTime(pool.cpuTimeNanoseconds) }}</dd></div>
                </dl>
              </article>
            </div>
          </template>
        </section>
        <div class="management-grid">
          <form class="panel management-form" @submit.prevent="submitDomain">
            <div class="panel-heading"><h2>{{ t('domains.create') }}</h2></div>
            <label><span>{{ t('domains.name') }}</span><input v-model="domainForm.name" required inputmode="url" spellcheck="false" :disabled="!canManageDomains"></label>
            <label><span>{{ t('domains.canonical') }}</span><select v-model="domainForm.canonicalMode" :disabled="!canManageDomains"><option value="serve_both">{{ t('domains.serveBoth') }}</option><option value="prefer_apex">{{ t('domains.preferApex') }}</option><option value="prefer_www">{{ t('domains.preferWWW') }}</option></select></label>
            <label><span>{{ t('domains.targetType') }}</span><select v-model="domainForm.targetType" :disabled="!canManageDomains"><option value="static">{{ t('domains.staticTarget') }}</option><option value="php" :disabled="availablePHPVersions.length === 0">{{ t('domains.phpTarget') }}</option></select><small v-if="availablePHPVersions.length === 0">{{ t('php.notIncluded') }}</small></label>
            <label v-if="domainForm.targetType === 'php'"><span>{{ t('php.versionLabel') }}</span><select v-model="domainForm.phpVersion" required :disabled="!canManageDomains"><option v-for="version in availablePHPVersions" :key="version" :value="version">{{ t('php.version', { version }) }}</option></select></label>
            <label><span>{{ t('domains.rootMode') }}</span><select v-model="domainForm.rootMode" :disabled="!canManageDomains"><option value="default">{{ t('domains.defaultRoot') }}</option><option value="custom">{{ t('domains.customRoot') }}</option></select></label>
            <label v-if="domainForm.rootMode === 'custom'"><span>{{ t('domains.documentRoot') }}</span><input v-model="domainForm.documentRoot" required spellcheck="false" :disabled="!canManageDomains"></label>
            <label class="check-field"><input v-model="domainForm.tls" type="checkbox" :disabled="!canManageDomains"><span>{{ t('domains.enableTLS') }}</span></label>
            <label><span>{{ t('domains.wafMode') }}</span><select v-model="domainForm.wafMode" :disabled="!canManageDomains"><option value="off">{{ t('waf.off') }}</option><option value="detection_only">{{ t('waf.detection_only') }}</option><option value="blocking_pl1">{{ t('waf.blocking_pl1') }}</option></select><small>{{ t('waf.hint') }}</small></label>
            <label><span>{{ t('cache.preset') }}</span><select v-model="domainForm.cachePreset" :disabled="!canManageDomains || domainForm.targetType !== 'php'"><option value="disabled">{{ t('cache.disabled') }}</option><option value="respect_origin">{{ t('cache.respect_origin') }}</option><option value="wordpress">{{ t('cache.wordpress') }}</option></select><small>{{ t('cache.hint') }}</small></label>
            <button class="primary-action" type="submit" :disabled="actionBusy || !canCreateDomain">{{ t('domains.createAction') }}</button>
          </form>
          <div class="resource-list">
            <article v-for="domain in domains" :key="domain.id" class="panel resource-card domain-card">
              <header><div><h2>{{ domain.name.display }}</h2><code>{{ domain.target.type === 'php' ? t('php.domainSummary', { version: domain.target.phpVersion, root: domain.target.documentRoot?.relativePath ?? domain.target.type }) : domain.target.documentRoot?.relativePath ?? domain.target.type }}</code></div><span class="state-badge" :data-state="domain.status">{{ t(`states.${domain.status}`) }}</span></header>
              <dl class="detail-list compact domain-details">
                <div><dt>{{ t('domains.targetType') }}</dt><dd>{{ domain.target.type === 'php' ? t('php.version', { version: domain.target.phpVersion }) : t('domains.staticTarget') }}</dd></div>
                <div><dt>{{ t('domains.canonical') }}</dt><dd>{{ t(`canonical.${domain.canonicalMode}`) }}</dd></div>
                <div><dt>{{ t('domains.tls') }}</dt><dd><span class="state-badge" :data-state="domain.tls.issuanceStatus">{{ t(`states.${domain.tls.issuanceStatus}`) }}</span></dd></div>
                <div><dt>{{ t('domains.wafMode') }}</dt><dd>{{ t(`waf.${domainWAFMode(domain)}`) }}</dd></div>
                <div><dt>{{ t('cache.preset') }}</dt><dd>{{ t(`cache.${domainCachePreset(domain)}`) }}</dd></div>
                <div><dt>{{ t('domains.expires') }}</dt><dd>{{ displayDate(domain.tls.expiresAt) }}</dd></div>
                <div><dt>{{ t('common.updated') }}</dt><dd>{{ displayDate(domain.updatedAt) }}</dd></div>
              </dl>
              <form v-if="editingDomainId === domain.id" class="inline-edit" @submit.prevent="submitDomainEdit(domain)">
                <label><span>{{ t('domains.canonical') }}</span><select v-model="editForm.canonicalMode"><option value="serve_both">{{ t('domains.serveBoth') }}</option><option value="prefer_apex">{{ t('domains.preferApex') }}</option><option value="prefer_www">{{ t('domains.preferWWW') }}</option></select></label>
                <label><span>{{ t('domains.targetType') }}</span><select v-model="editForm.targetType"><option value="static">{{ t('domains.staticTarget') }}</option><option value="php" :disabled="availablePHPVersions.length === 0">{{ t('domains.phpTarget') }}</option></select></label>
                <label v-if="editForm.targetType === 'php'"><span>{{ t('php.versionLabel') }}</span><select v-model="editForm.phpVersion" required><option v-for="version in availablePHPVersions" :key="version" :value="version">{{ t('php.version', { version }) }}</option></select><small v-if="!availablePHPVersions.includes(editForm.phpVersion)" class="field-error" role="alert">{{ t('php.versionNoLongerAvailable') }}</small></label>
                <label><span>{{ t('domains.rootMode') }}</span><select v-model="editForm.rootMode"><option value="default">{{ t('domains.defaultRoot') }}</option><option value="custom">{{ t('domains.customRoot') }}</option></select></label>
                <label v-if="editForm.rootMode === 'custom'"><span>{{ t('domains.documentRoot') }}</span><input v-model="editForm.documentRoot" required spellcheck="false"></label>
                <label><span>{{ t('domains.wafMode') }}</span><select v-model="editForm.wafMode"><option value="off">{{ t('waf.off') }}</option><option value="detection_only">{{ t('waf.detection_only') }}</option><option value="blocking_pl1">{{ t('waf.blocking_pl1') }}</option></select><small>{{ t('waf.hint') }}</small></label>
                <label><span>{{ t('cache.preset') }}</span><select v-model="editForm.cachePreset" :disabled="editForm.targetType !== 'php'"><option value="disabled">{{ t('cache.disabled') }}</option><option value="respect_origin">{{ t('cache.respect_origin') }}</option><option value="wordpress">{{ t('cache.wordpress') }}</option></select><small>{{ t('cache.hint') }}</small></label>
                <div class="card-actions"><button class="text-action" type="button" @click="editingDomainId = ''">{{ t('common.cancel') }}</button><button class="primary-action" type="submit" :disabled="actionBusy || !canSubmitEdit">{{ t('common.save') }}</button></div>
              </form>
              <div v-else class="card-actions">
                <button v-if="domainCachePreset(domain) !== 'disabled'" class="secondary-action" type="button" :disabled="Boolean(cacheBusyDomainId)" :aria-expanded="Boolean(cacheStatusByDomain[domain.id])" @click="loadCacheStatus(domain)">{{ t('cache.metrics') }}</button>
                <button v-if="canManageDomains && (domain.target.type === 'static' || domain.target.type === 'php')" class="secondary-action" type="button" :disabled="actionBusy" @click="startEditing(domain)">{{ t('common.edit') }}</button>
                <button v-if="canManageDomains && domain.tls.enabled && domain.tls.issuanceStatus !== 'active'" class="secondary-action" type="button" :disabled="actionBusy" @click="issueCertificate(domain)">{{ t('domains.issueTLS') }}</button>
                <button v-if="canManageDomains && domain.status === 'suspended'" class="secondary-action" type="button" :disabled="actionBusy" @click="runDomainAction(domain, 'resume')">{{ t('common.resume') }}</button>
                <button v-else-if="canManageDomains" class="secondary-action" type="button" :disabled="actionBusy" @click="runDomainAction(domain, 'suspend')">{{ t('common.suspend') }}</button>
                <button v-if="canRemoveDomains" class="danger-action" type="button" :disabled="actionBusy" @click="runDomainAction(domain, 'remove')">{{ t('common.remove') }}</button>
                <button v-if="domain.tls.enabled" class="text-action" type="button" :aria-expanded="certificateHistoryDomainId === domain.id" :aria-controls="`certificate-history-${domain.id}`" @click="toggleCertificateHistory(domain)">{{ certificateHistoryDomainId === domain.id ? t('certificates.hideHistory') : t('certificates.showHistory') }}</button>
              </div>
              <section v-if="cacheStatusByDomain[domain.id]" class="certificate-history" :aria-label="t('cache.managementFor', { domain: domain.name.display })">
                <dl class="detail-list compact">
                  <div><dt>{{ t('cache.hits') }}</dt><dd>{{ formatNumber(cacheStatusByDomain[domain.id].metrics.hits, activeLocale) }}</dd></div>
                  <div><dt>{{ t('cache.misses') }}</dt><dd>{{ formatNumber(cacheStatusByDomain[domain.id].metrics.misses, activeLocale) }}</dd></div>
                  <div><dt>{{ t('cache.bypasses') }}</dt><dd>{{ formatNumber(cacheStatusByDomain[domain.id].metrics.bypasses, activeLocale) }}</dd></div>
                  <div><dt>{{ t('cache.hitRatio') }}</dt><dd>{{ formatPercent(cacheHitRatio(cacheStatusByDomain[domain.id]), activeLocale) }}</dd></div>
                </dl>
                <form v-if="canManageDomains" class="inline-edit" @submit.prevent="purgeDomainCache(domain)">
                  <label><span>{{ t('cache.pathPrefix') }}</span><input v-model="cachePathPrefix[domain.id]" required maxlength="512" pattern="/.*" placeholder="/" spellcheck="false"><small>{{ t('cache.purgeHint') }}</small></label>
                  <button class="secondary-action" type="submit" :disabled="Boolean(cacheBusyDomainId)">{{ t('cache.purge') }}</button>
                </form>
                <p v-if="cacheFeedback[domain.id]" class="inline-feedback" :class="{ error: cacheFeedback[domain.id] === 'failed' }" role="status">{{ t(`cache.${cacheFeedback[domain.id]}`) }}</p>
              </section>
              <section v-if="certificateHistoryDomainId === domain.id" :id="`certificate-history-${domain.id}`" class="certificate-history" :aria-label="t('certificates.historyFor', { domain: domain.name.display })">
                <p v-if="certificateHistoryLoadingDomainId === domain.id" class="certificate-history-state" role="status">{{ t('certificates.loading') }}</p>
                <p v-else-if="(certificateHistory[domain.id]?.length ?? 0) === 0" class="certificate-history-state">{{ t('certificates.empty') }}</p>
                <div v-else class="certificate-records">
                  <div v-for="certificate in certificateHistory[domain.id]" :key="certificate.id" class="certificate-record">
                    <header><strong>{{ t('certificates.record') }}</strong><span class="state-badge" :data-state="certificate.status">{{ t(`states.${certificate.status}`) }}</span></header>
                    <dl class="detail-list compact">
                      <div><dt>{{ t('certificates.names') }}</dt><dd class="wrapped-value">{{ certificate.names.join(', ') }}</dd></div>
                      <div><dt>{{ t('certificates.issuer') }}</dt><dd>{{ certificate.issuer || t('common.notAvailable') }}</dd></div>
                      <div><dt>{{ t('certificates.validFrom') }}</dt><dd>{{ displayDate(certificate.notBefore) }}</dd></div>
                      <div><dt>{{ t('domains.expires') }}</dt><dd>{{ displayDate(certificate.expiresAt) }}</dd></div>
                      <div><dt>{{ t('certificates.nextRenewal') }}</dt><dd>{{ displayDate(certificate.nextRenewalAt) }}</dd></div>
                      <div><dt>{{ t('certificates.activated') }}</dt><dd>{{ displayDate(certificate.activatedAt) }}</dd></div>
                      <div v-if="certificate.retiredAt"><dt>{{ t('certificates.retired') }}</dt><dd>{{ displayDate(certificate.retiredAt) }}</dd></div>
                      <div v-if="certificate.fingerprintSha256"><dt>{{ t('certificates.fingerprint') }}</dt><dd><code class="wrapped-value">{{ certificate.fingerprintSha256 }}</code></dd></div>
                    </dl>
                  </div>
                </div>
              </section>
            </article>
            <div v-if="domains.length === 0" class="panel empty-state"><strong>{{ t('domains.empty') }}</strong><p>{{ t('domains.emptyBody') }}</p></div>
          </div>
        </div>
      </section>

      <section v-else-if="page === 'files'" class="file-workspace">
        <div v-if="!selectedAccount.hostReady" class="inline-feedback" role="status">{{ t('account.provisioningReadOnly') }}</div>
        <div v-else-if="selectedAccount.membershipRole === 'auditor'" class="inline-feedback" role="status">{{ t('files.auditorUnavailable') }}</div>
        <article v-else class="panel file-browser" aria-labelledby="file-browser-title">
          <div class="panel-heading file-browser-heading">
            <div><p class="eyebrow">{{ t('files.eyebrow') }}</p><h2 id="file-browser-title">{{ t('files.title') }}</h2></div>
            <code class="file-current-path">/{{ fileListing.path || '' }}</code>
          </div>
          <div class="file-toolbar">
            <button class="secondary-action" type="button" :disabled="!fileListing.path || loading" @click="openParentDirectory">{{ t('files.parent') }}</button>
            <button class="secondary-action" type="button" :disabled="trashLoading" :aria-expanded="trashOpen" @click="toggleFileTrash">{{ trashOpen ? t('files.hideTrash') : t('files.showTrash') }}</button>
            <p>{{ t('files.securityHint') }}</p>
          </div>
          <section class="file-mutation-panel" :aria-label="t('files.actions')">
            <div class="file-upload-control">
              <label for="file-upload-input"><span>{{ t('files.upload') }}</span><input id="file-upload-input" type="file" :disabled="!canMutateFiles || fileMutationBusy" @change="selectUploadFile"></label>
              <div class="file-action-buttons">
                <button class="primary-action" type="button" :disabled="!selectedUploadFile || !canMutateFiles || fileMutationBusy" @click="uploadSelectedFile">{{ t('files.startUpload') }}</button>
                <button v-if="fileMutationBusy && activeUploadId" class="secondary-action" type="button" @click="cancelActiveUpload">{{ t('common.cancel') }}</button>
              </div>
            </div>
            <div class="file-node-control">
              <label for="file-node-name"><span>{{ t('files.newName') }}</span><input id="file-node-name" v-model="fileNodeName" type="text" maxlength="255" autocomplete="off" :disabled="!canMutateFiles || fileMutationBusy"></label>
              <div class="file-action-buttons">
                <button class="secondary-action" type="button" :disabled="!fileNodeName || !canMutateFiles || fileMutationBusy" @click="createFileNode('file')">{{ t('files.createFile') }}</button>
                <button class="secondary-action" type="button" :disabled="!fileNodeName || !canMutateFiles || fileMutationBusy" @click="createFileNode('directory')">{{ t('files.createDirectory') }}</button>
              </div>
            </div>
            <div v-if="fileMutationBusy" class="file-upload-progress" role="status" aria-live="polite"><progress max="100" :value="uploadProgress">{{ uploadProgress }}%</progress><span>{{ uploadProgress }}%</span></div>
            <p v-if="fileMutationFeedback" class="inline-feedback" :class="{ error: fileMutationFailed, success: !fileMutationFailed }" role="status" aria-live="polite">{{ fileMutationFeedback }}</p>
            <p class="measurement-note file-upload-note">{{ t('files.uploadHint') }}</p>
          </section>
          <div class="file-table-wrap">
            <table class="file-table">
              <thead><tr><th scope="col">{{ t('files.name') }}</th><th scope="col">{{ t('files.type') }}</th><th scope="col">{{ t('files.size') }}</th><th scope="col">{{ t('files.permissions') }}</th><th scope="col">{{ t('common.updated') }}</th><th scope="col">{{ t('files.actions') }}</th></tr></thead>
              <tbody>
                <tr v-for="entry in fileListing.entries" :key="entry.name">
                  <td><button v-if="entry.type === 'directory'" class="file-name-action" type="button" @click="openFileDirectory(entry)"><span aria-hidden="true">▰</span>{{ entry.name }}</button><a v-else-if="entry.type === 'file'" class="file-name-action" :href="fileDownloadPath(entry)" :download="entry.name" :aria-label="t('files.download', { name: entry.name })"><span aria-hidden="true">⇩</span>{{ entry.name }}</a><span v-else class="file-name"><span aria-hidden="true">{{ entry.type === 'symlink' ? '↗' : '·' }}</span>{{ entry.name }}</span></td>
                  <td>{{ t(`files.types.${entry.type}`) }}</td>
                  <td>{{ entry.type === 'file' ? formatBytes(entry.sizeBytes, activeLocale) : '—' }}</td>
                  <td><code>{{ displayFileMode(entry.mode) }}</code></td>
                  <td>{{ displayDate(entry.modifiedAt) }}</td>
                  <td><div v-if="entry.type === 'file' || entry.type === 'directory'" class="file-row-actions"><button type="button" class="text-action" :disabled="!canMutateFiles || fileMutationBusy" @click="mutateFileEntry(entry, 'rename')">{{ t('files.rename') }}</button><button type="button" class="text-action" :disabled="!canMutateFiles || fileMutationBusy" @click="mutateFileEntry(entry, 'copy')">{{ t('files.copy') }}</button><button type="button" class="text-action" :disabled="!canMutateFiles || fileMutationBusy" @click="mutateFileEntry(entry, 'move')">{{ t('files.move') }}</button><button type="button" class="text-action" :disabled="!canMutateFiles || fileMutationBusy" @click="createFileArchive(entry)">{{ t('files.archive') }}</button><button v-if="entry.type === 'file' && archiveFormatForName(entry.name)" type="button" class="text-action" :disabled="!canMutateFiles || fileMutationBusy" @click="extractFileArchive(entry)">{{ t('files.extract') }}</button><button type="button" class="danger-link" :disabled="!canMutateFiles || fileMutationBusy" @click="trashFileEntry(entry)">{{ t('files.trash') }}</button></div><span v-else>—</span></td>
                </tr>
              </tbody>
            </table>
          </div>
          <div v-if="fileListing.entries.length === 0" class="empty-state file-empty"><strong>{{ t('files.empty') }}</strong><p>{{ t('files.emptyBody') }}</p></div>
          <p v-if="fileListing.omittedEntries > 0" class="measurement-note">{{ t('files.omitted', { count: fileListing.omittedEntries }) }}</p>
          <div v-if="fileListing.next" class="card-actions"><button class="secondary-action" type="button" :disabled="loading" @click="loadNextFilePage">{{ t('files.loadMore') }}</button></div>
          <section v-if="trashOpen" class="file-trash" aria-labelledby="file-trash-title">
            <div class="panel-heading"><div><p class="eyebrow">{{ t('files.recovery') }}</p><h3 id="file-trash-title">{{ t('files.trashTitle') }}</h3></div></div>
            <p class="measurement-note">{{ t('files.trashHint') }}</p>
            <div v-if="trashEntries.length" class="file-table-wrap"><table class="file-table"><thead><tr><th scope="col">{{ t('files.originalPath') }}</th><th scope="col">{{ t('files.type') }}</th><th scope="col">{{ t('files.trashedAt') }}</th><th scope="col">{{ t('files.actions') }}</th></tr></thead><tbody><tr v-for="entry in trashEntries" :key="entry.trashId"><td><code class="wrapped-value">/{{ entry.directory ? `${entry.directory}/` : '' }}{{ entry.name }}</code></td><td>{{ t(`files.types.${entry.type}`) }}</td><td>{{ displayDate(entry.trashedAt) }}</td><td><div class="file-row-actions"><button class="text-action" type="button" :disabled="fileMutationBusy" @click="restoreTrashEntry(entry)">{{ t('files.restore') }}</button><button class="danger-link" type="button" :disabled="fileMutationBusy" @click="purgeTrashEntry(entry)">{{ t('files.purge') }}</button></div></td></tr></tbody></table></div>
            <div v-else-if="!trashLoading" class="empty-state file-empty"><strong>{{ t('files.trashEmpty') }}</strong><p>{{ t('files.trashEmptyBody') }}</p></div>
            <div v-if="trashNext" class="card-actions"><button class="secondary-action" type="button" :disabled="trashLoading" @click="loadFileTrash(false)">{{ t('files.loadMore') }}</button></div>
          </section>
        </article>
      </section>

      <section v-else-if="page === 'backups'" class="database-workspace backup-workspace">
        <div v-if="!selectedAccount.hostReady" class="inline-feedback" role="status">{{ t('account.provisioningReadOnly') }}</div>
        <div v-else-if="selectedAccount.membershipRole === 'auditor'" class="inline-feedback" role="status">{{ t('backups.auditorUnavailable') }}</div>
        <template v-else>
          <div class="inline-feedback" role="note">
            <strong>{{ t('backups.filesOnlyTitle') }}</strong> {{ t('backups.filesOnlyBody') }}
          </div>
          <p v-if="backupFeedback" class="inline-feedback" :class="{ error: backupFailed, success: !backupFailed }" role="status" aria-live="polite">{{ backupFeedback }}</p>
          <div class="management-grid">
            <form class="panel management-form" @submit.prevent="createBackup">
              <div class="panel-heading"><div><p class="eyebrow">{{ t('backups.eyebrow') }}</p><h2>{{ t('backups.createTitle') }}</h2></div></div>
              <label><span>{{ t('backups.scope') }}</span><select v-model="backupScope" :disabled="!canCreateBackups || backupBusy"><option value="account_files">{{ t('backups.scopes.account_files') }}</option><option value="document_root">{{ t('backups.scopes.document_root') }}</option></select></label>
              <label v-if="backupScope === 'document_root'"><span>{{ t('backups.sourcePath') }}</span><input v-model="backupSourcePath" required maxlength="255" pattern="[A-Za-z0-9._-]+(?:/[A-Za-z0-9._-]+)*" spellcheck="false" autocomplete="off" :disabled="!canCreateBackups || backupBusy"><small>{{ t('backups.sourcePathHint') }}</small></label>
              <p class="form-hint">{{ backupScope === 'account_files' ? t('backups.accountScopeHint') : t('backups.documentRootHint') }}</p>
              <button class="primary-action" type="submit" :disabled="!canCreateBackups || backupBusy || (backupScope === 'document_root' && !backupSourcePath)">{{ backupBusy ? t('backups.working') : t('backups.create') }}</button>
            </form>

            <form class="panel management-form" @submit.prevent="importBackup">
              <div class="panel-heading"><div><p class="eyebrow">{{ t('backups.portableArchive') }}</p><h2>{{ t('backups.importTitle') }}</h2></div></div>
              <label><span>{{ t('backups.archiveFile') }}</span><input type="file" accept=".tar.gz,application/gzip" required :disabled="!canCreateBackups || backupBusy" @change="selectBackupImport"></label>
              <p class="form-hint">{{ t('backups.importHint') }}</p>
              <progress v-if="backupBusy && backupUploadProgress > 0" max="100" :value="backupUploadProgress">{{ backupUploadProgress }}%</progress>
              <div class="card-actions"><button class="primary-action" type="submit" :disabled="!canCreateBackups || !selectedBackupFile || backupBusy">{{ t('backups.import') }}</button><button v-if="activeBackupUploadId" class="danger-action" type="button" @click="cancelBackupImport">{{ t('common.cancel') }}</button></div>
            </form>

            <article class="panel table-panel">
              <div class="panel-heading"><div><p class="eyebrow">{{ t('backups.localRepository') }}</p><h2>{{ t('backups.title') }}</h2></div><button class="secondary-action" type="button" :disabled="backupsLoading || backupBusy" @click="loadBackups(true)">{{ t('common.refresh') }}</button></div>
              <p class="measurement-note">{{ t('backups.repositoryHint') }}</p>
              <div v-if="backupRepository" class="large-progress"><div><strong>{{ formatBytes(backupRepository.usedBytes, activeLocale) }}</strong><span>{{ t('backups.repositoryUsage', { limit: formatBytes(backupRepository.limitBytes, activeLocale) }) }}</span></div><progress :max="backupRepository.limitBytes" :value="backupRepository.usedBytes">{{ backupRepository.usedBytes }}</progress><small>{{ t('backups.repositoryCounts', { backups: backupRepository.backupCount, maximum: backupRepository.maximumBackups, uploads: backupRepository.activeUploads }) }}</small></div>
              <div v-if="backups.length" class="file-table-wrap">
                <table class="file-table">
                  <thead><tr><th scope="col">{{ t('backups.createdAt') }}</th><th scope="col">{{ t('backups.scope') }}</th><th scope="col">{{ t('backups.content') }}</th><th scope="col">{{ t('backups.integrity') }}</th><th scope="col">{{ t('files.actions') }}</th></tr></thead>
                  <tbody><tr v-for="item in backups" :key="item.backupId">
                    <td><span>{{ displayDate(item.createdAt) }}</span><br><code class="wrapped-value">{{ item.backupId }}</code></td>
                    <td>{{ t(`backups.scopes.${item.scope}`) }}<br><code v-if="item.sourcePath">/{{ item.sourcePath }}</code></td>
                    <td>{{ formatBytes(item.contentBytes, activeLocale) }} · {{ t('backups.entries', { count: formatNumber(item.entryCount, activeLocale) }) }}<br><small>{{ t('backups.archiveSize', { size: formatBytes(item.payloadBytes, activeLocale) }) }}</small></td>
                    <td><span class="state-badge" :data-state="item.manifestAuthenticated ? 'active' : 'failed'">{{ item.manifestAuthenticated ? t('backups.authenticated') : t('backups.notAuthenticated') }}</span><br><small>{{ item.payloadVerified ? t('backups.payloadVerified') : t('backups.payloadNotVerified') }}</small></td>
                    <td><div class="file-row-actions"><a class="text-action" :href="backupDownloadPath(item)">{{ t('backups.download') }}</a><button class="text-action" type="button" :disabled="backupBusy" @click="verifyBackup(item)">{{ t('backups.verify') }}</button><button v-if="canRestoreBackups" class="danger-link" type="button" :disabled="backupBusy" @click="restoreBackup(item)">{{ t('backups.restore') }}</button><button v-if="canDeleteBackups" class="danger-link" type="button" :disabled="backupBusy" @click="deleteBackup(item)">{{ t('backups.delete') }}</button></div></td>
                  </tr></tbody>
                </table>
              </div>
              <div v-else-if="!backupsLoading" class="empty-state"><strong>{{ t('backups.empty') }}</strong><p>{{ t('backups.emptyBody') }}</p></div>
              <div v-if="backupsNext" class="card-actions"><button class="secondary-action" type="button" :disabled="backupsLoading || backupBusy" @click="loadBackups(false)">{{ t('files.loadMore') }}</button></div>
              <p class="form-hint">{{ t('backups.restoreWarning') }}</p>
            </article>
          </div>
        </template>
      </section>

      <section v-else-if="page === 'logs'" class="database-workspace log-workspace">
        <div v-if="!selectedAccount.hostReady" class="inline-feedback" role="status">{{ t('account.provisioningReadOnly') }}</div>
        <article v-else class="panel table-panel" aria-labelledby="domain-log-title">
          <div class="panel-heading">
            <div><p class="eyebrow">{{ t('logs.eyebrow') }}</p><h2 id="domain-log-title">{{ t('logs.title') }}</h2></div>
            <button class="secondary-action" type="button" :disabled="logsLoading || !logDomainId" @click="loadLogs(true)">{{ t('common.refresh') }}</button>
          </div>
          <div class="management-form log-controls">
            <label><span>{{ t('logs.domain') }}</span><select v-model="logDomainId" :disabled="logsLoading || domains.length === 0" @change="changeLogSelection"><option v-for="domain in domains" :key="domain.id" :value="domain.id">{{ domain.name.display }}</option></select></label>
            <label><span>{{ t('logs.kind') }}</span><select v-model="logKind" :disabled="logsLoading" @change="changeLogSelection"><option value="access">{{ t('logs.access') }}</option><option value="error">{{ t('logs.error') }}</option></select></label>
          </div>
          <div class="inline-feedback" role="note"><strong>{{ t('logs.privacyTitle') }}</strong> {{ t('logs.privacyBody') }}</div>
          <p v-if="logPage" class="measurement-note">{{ t('logs.retention', { days: logPage.retentionDays, size: formatBytes(logPage.maximumActiveBytes, activeLocale) }) }}</p>
          <p v-if="logsFailed" class="inline-feedback error" role="alert">{{ t('logs.loadFailed') }}</p>
          <div v-if="logPage?.records.length" class="file-table-wrap" aria-live="polite">
            <table class="file-table">
              <thead v-if="logKind === 'access'"><tr><th scope="col">{{ t('logs.time') }}</th><th scope="col">{{ t('logs.client') }}</th><th scope="col">{{ t('logs.request') }}</th><th scope="col">{{ t('logs.status') }}</th><th scope="col">{{ t('logs.transfer') }}</th></tr></thead>
              <thead v-else><tr><th scope="col">{{ t('logs.time') }}</th><th scope="col">{{ t('logs.level') }}</th><th scope="col">{{ t('logs.message') }}</th></tr></thead>
              <tbody v-if="logKind === 'access'"><tr v-for="(record, index) in logPage.records" :key="`${record.timestamp}-${index}`"><td>{{ displayDate(record.timestamp) }}</td><td><code>{{ record.clientAddress }}</code></td><td><code class="wrapped-value">{{ record.method }} {{ record.path }}</code><br><small>{{ record.host }}</small></td><td><span class="state-badge" :data-state="(record.status ?? 500) < 400 ? 'active' : 'failed'">{{ record.status }}</span></td><td>{{ formatBytes(record.bytes ?? 0, activeLocale) }}<br><small>{{ t('logs.duration', { value: formatNumber(record.durationMs ?? 0, activeLocale) }) }}</small></td></tr></tbody>
              <tbody v-else><tr v-for="(record, index) in logPage.records" :key="`${record.timestamp}-${index}`"><td>{{ displayDate(record.timestamp) }}</td><td><span class="state-badge" :data-state="record.level === 'error' || record.level === 'crit' || record.level === 'alert' || record.level === 'emerg' ? 'failed' : 'pending'">{{ record.level }}</span></td><td><code class="wrapped-value">{{ record.message }}</code></td></tr></tbody>
            </table>
          </div>
          <div v-else-if="!logsLoading && !logsFailed" class="empty-state"><strong>{{ domains.length ? t('logs.empty') : t('logs.noDomains') }}</strong><p>{{ domains.length ? t('logs.emptyBody') : t('logs.noDomainsBody') }}</p></div>
          <div v-if="logPage?.next" class="card-actions"><button class="secondary-action" type="button" :disabled="logsLoading" @click="loadLogs(false)">{{ t('logs.loadOlder') }}</button></div>
        </article>
        <article v-if="selectedAccount.hostReady" class="panel table-panel" aria-labelledby="waf-events-title">
          <div class="panel-heading">
            <div><p class="eyebrow">{{ t('wafEvents.eyebrow') }}</p><h2 id="waf-events-title">{{ t('wafEvents.title') }}</h2></div>
            <button class="secondary-action" type="button" :disabled="wafEventsLoading || !logDomainId" @click="loadWAFEvents(true)">{{ t('common.refresh') }}</button>
          </div>
          <div class="inline-feedback" role="note"><strong>{{ t('wafEvents.privacyTitle') }}</strong> {{ t('wafEvents.privacyBody') }}</div>
          <p v-if="wafEventPage" class="measurement-note">{{ t('logs.retention', { days: wafEventPage.retentionDays, size: formatBytes(wafEventPage.maximumActiveBytes, activeLocale) }) }}</p>
          <p v-if="wafEventsFailed" class="inline-feedback error" role="alert">{{ t('wafEvents.loadFailed') }}</p>
          <div v-if="wafEventPage?.events.length" class="file-table-wrap" aria-live="polite">
            <table class="file-table">
              <thead><tr><th scope="col">{{ t('logs.time') }}</th><th scope="col">{{ t('wafEvents.rule') }}</th><th scope="col">{{ t('wafEvents.category') }}</th><th scope="col">{{ t('wafEvents.request') }}</th><th scope="col">{{ t('wafEvents.outcome') }}</th></tr></thead>
              <tbody><tr v-for="event in wafEventPage.events" :key="event.id"><td>{{ displayDate(event.timestamp) }}</td><td><code>{{ event.ruleId }}</code><br><small>{{ t(`wafEvents.severity.${event.severity}`) }}</small></td><td>{{ t(`wafEvents.categories.${event.category}`) }}</td><td><code v-if="event.method && event.path" class="wrapped-value">{{ event.method }} {{ event.path }}</code><span v-else>{{ t('common.notAvailable') }}</span><br><small v-if="event.correlationId">{{ t('wafEvents.correlation') }} <code>{{ event.correlationId }}</code></small></td><td><span class="state-badge" :data-state="event.outcome === 'blocked' ? 'failed' : 'pending'">{{ t(`wafEvents.${event.outcome}`) }}</span></td></tr></tbody>
            </table>
          </div>
          <div v-else-if="!wafEventsLoading && !wafEventsFailed" class="empty-state"><strong>{{ t('wafEvents.empty') }}</strong><p>{{ t('wafEvents.emptyBody') }}</p></div>
          <div v-if="wafEventPage?.next" class="card-actions"><button class="secondary-action" type="button" :disabled="wafEventsLoading" @click="loadWAFEvents(false)">{{ t('wafEvents.loadOlder') }}</button></div>
        </article>
      </section>

      <section v-else-if="page === 'jobs'" class="database-workspace">
        <div v-if="selectedAccount.status === 'suspended'" class="inline-feedback error" role="status">{{ t('account.suspendedReadOnly') }}</div>
        <div v-else-if="!selectedAccount.hostReady" class="inline-feedback" role="status">{{ t('account.provisioningReadOnly') }}</div>
        <div v-else-if="selectedAccount.effectiveLimits.maxScheduledJobs === 0" class="inline-feedback" role="status">{{ t('jobs.notIncluded') }}</div>
        <p v-if="scheduledJobFeedback" class="inline-feedback" :class="{ error: scheduledJobFailed, success: !scheduledJobFailed }" role="status" aria-live="polite">{{ scheduledJobFeedback }}</p>
        <div class="management-grid">
          <form class="panel management-form" @submit.prevent="submitScheduledJob">
            <div class="panel-heading"><div><p class="eyebrow">{{ t('jobs.eyebrow') }}</p><h2>{{ editingScheduledJobId ? t('jobs.editTitle') : t('jobs.createTitle') }}</h2></div></div>
            <p class="form-hint">{{ t('jobs.securityHint') }}</p>
            <label><span>{{ t('jobs.name') }}</span><input v-model="scheduledJobForm.name" required maxlength="80" autocomplete="off" :disabled="!canManageScheduledJobs || scheduledJobBusy"></label>
            <label><span>{{ t('jobs.runtime') }}</span><select v-model="scheduledJobForm.runtime" :disabled="!canManageScheduledJobs || scheduledJobBusy"><option value="shell">{{ t('jobs.shell') }}</option><option value="php" :disabled="availablePHPVersions.length === 0">{{ t('jobs.php') }}</option></select></label>
            <label><span>{{ t('jobs.scriptPath') }}</span><input v-model="scheduledJobForm.scriptPath" required maxlength="255" pattern="[A-Za-z0-9._-]+(?:/[A-Za-z0-9._-]+)*" spellcheck="false" autocomplete="off" :disabled="!canManageScheduledJobs || scheduledJobBusy"><small>{{ t('jobs.scriptPathHint') }}</small></label>
            <label v-if="scheduledJobForm.runtime === 'php'"><span>{{ t('php.versionLabel') }}</span><select v-model="scheduledJobForm.phpVersion" required :disabled="!canManageScheduledJobs || scheduledJobBusy"><option value="" disabled>{{ t('jobs.selectVersion') }}</option><option v-for="version in availablePHPVersions" :key="version" :value="version">{{ t('php.version', { version }) }}</option></select></label>
            <label><span>{{ t('jobs.schedule') }}</span><select v-model="scheduledJobForm.scheduleKind" :disabled="!canManageScheduledJobs || scheduledJobBusy"><option value="interval">{{ t('jobs.interval') }}</option><option value="hourly">{{ t('jobs.hourly') }}</option><option value="daily">{{ t('jobs.daily') }}</option><option value="weekly">{{ t('jobs.weekly') }}</option></select></label>
            <label v-if="scheduledJobForm.scheduleKind === 'interval'"><span>{{ t('jobs.intervalMinutes') }}</span><select v-model.number="scheduledJobForm.intervalMinutes"><option :value="5">5</option><option :value="15">15</option><option :value="30">30</option></select></label>
            <label v-if="scheduledJobForm.scheduleKind === 'weekly'"><span>{{ t('jobs.weekday') }}</span><select v-model="scheduledJobForm.weekday"><option v-for="day in ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun']" :key="day" :value="day">{{ t(`jobs.weekdays.${day}`) }}</option></select></label>
            <div v-if="scheduledJobForm.scheduleKind === 'daily' || scheduledJobForm.scheduleKind === 'weekly'" class="form-row"><label><span>{{ t('jobs.hourUtc') }}</span><input v-model.number="scheduledJobForm.hourUtc" type="number" min="0" max="23" required></label><label><span>{{ t('jobs.minuteUtc') }}</span><input v-model.number="scheduledJobForm.minuteUtc" type="number" min="0" max="59" required></label></div>
            <label v-else-if="scheduledJobForm.scheduleKind === 'hourly'"><span>{{ t('jobs.minuteUtc') }}</span><input v-model.number="scheduledJobForm.minuteUtc" type="number" min="0" max="59" required></label>
            <label class="check-field"><input v-model="scheduledJobForm.enabled" type="checkbox" :disabled="!canManageScheduledJobs || scheduledJobBusy"><span>{{ t('jobs.enabled') }}</span></label>
            <div class="card-actions"><button class="primary-action" type="submit" :disabled="!canManageScheduledJobs || scheduledJobBusy || (scheduledJobForm.runtime === 'php' && !availablePHPVersions.includes(scheduledJobForm.phpVersion))">{{ editingScheduledJobId ? t('jobs.save') : t('jobs.create') }}</button><button v-if="editingScheduledJobId" class="secondary-action" type="button" @click="editingScheduledJobId = ''; resetScheduledJobForm()">{{ t('common.cancel') }}</button></div>
          </form>

          <article class="panel table-panel">
            <div class="panel-heading"><div><p class="eyebrow">{{ t('jobs.usage', { current: scheduledJobList.length, limit: selectedAccount.effectiveLimits.maxScheduledJobs }) }}</p><h2>{{ t('jobs.title') }}</h2></div><button class="secondary-action" type="button" :disabled="scheduledJobsLoading" @click="loadScheduledJobs">{{ t('common.refresh') }}</button></div>
            <div v-if="scheduledJobList.length" class="resource-list">
              <article v-for="job in scheduledJobList" :key="job.id" class="resource-card">
                <header><div><h3>{{ job.name }}</h3><code class="wrapped-value">{{ job.scriptPath }}</code></div><span class="state-badge" :data-state="job.status">{{ t(`states.${job.status}`) }}</span></header>
                <dl class="detail-list compact"><div><dt>{{ t('jobs.runtime') }}</dt><dd>{{ job.runtime === 'php' ? t('php.version', { version: job.phpVersion }) : t('jobs.shell') }}</dd></div><div><dt>{{ t('jobs.schedule') }}</dt><dd>{{ formatScheduledJobSchedule(job.schedule) }}</dd></div><div><dt>{{ t('common.updated') }}</dt><dd>{{ displayDate(job.updatedAt) }}</dd></div></dl>
                <div v-if="job.status === 'active' || job.status === 'disabled'" class="card-actions"><button class="secondary-action" type="button" :disabled="!canManageScheduledJobs || scheduledJobBusy" @click="editScheduledJob(job)">{{ t('common.edit') }}</button><button class="secondary-action" type="button" :disabled="!canManageScheduledJobs || scheduledJobBusy" @click="toggleScheduledJob(job)">{{ job.enabled ? t('jobs.disable') : t('jobs.enable') }}</button><button class="danger-action" type="button" :disabled="!canManageScheduledJobs || scheduledJobBusy" @click="deleteScheduledJob(job)">{{ t('jobs.delete') }}</button></div>
              </article>
            </div>
            <div v-else-if="!scheduledJobsLoading" class="empty-state"><strong>{{ t('jobs.empty') }}</strong><p>{{ t('jobs.emptyBody') }}</p></div>
          </article>
        </div>
      </section>

      <section v-else-if="page === 'databases'" class="database-workspace">
        <div v-if="selectedAccount.status === 'suspended'" class="inline-feedback error" role="status">{{ t('account.suspendedReadOnly') }}</div>
        <div v-else-if="!selectedAccount.hostReady" class="inline-feedback" role="status">{{ t('account.provisioningReadOnly') }}</div>
        <div v-if="trackedOperation?.kind === 'database.lifecycle'" class="panel account-operation" role="status" aria-live="polite">
          <div class="operation-summary"><div><span>{{ operationLabel(trackedOperation.kind) }}</span><strong>{{ t(`states.${trackedOperation.status}`) }}</strong></div><span class="state-badge" :data-state="trackedOperation.status">{{ trackedOperation.progressPercent }}%</span></div>
          <progress max="100" :value="trackedOperation.progressPercent" :aria-label="t('operations.progress')">{{ trackedOperation.progressPercent }}%</progress>
          <small>{{ t('operations.attempts', { current: trackedOperation.attemptCount, maximum: trackedOperation.maxAttempts }) }}</small>
        </div>

        <article v-if="databaseCredential" class="panel credential-reveal" role="alert" aria-labelledby="database-credential-title">
          <div class="panel-heading"><div><p class="eyebrow">{{ t('databases.singleUse') }}</p><h2 id="database-credential-title">{{ t('databases.credentialTitle') }}</h2></div><button class="text-action" type="button" @click="emit('dismissDatabaseCredential')">{{ t('common.close') }}</button></div>
          <p>{{ t('databases.credentialWarning') }}</p>
          <dl class="detail-list">
            <div><dt>{{ t('databases.username') }}</dt><dd><code class="wrapped-value">{{ databaseCredential.username }}</code></dd></div>
            <div><dt>{{ t('databases.host') }}</dt><dd><code>{{ databaseCredential.host }}</code></dd></div>
            <div><dt>{{ t('databases.password') }}</dt><dd><code class="wrapped-value">{{ databaseCredential.password }}</code></dd></div>
          </dl>
        </article>

        <div class="management-grid">
          <form class="panel management-form database-wizard" @submit.prevent="submitDatabaseWizard">
            <div class="panel-heading"><div><p class="eyebrow">{{ t('databases.step', { current: databaseWizard.step, total: 4 }) }}</p><h2>{{ t('databases.wizardTitle') }}</h2></div></div>
            <ol class="wizard-steps" :aria-label="t('databases.wizardSteps')">
              <li v-for="step in 4" :key="step" :aria-current="databaseWizard.step === step ? 'step' : undefined" :data-state="databaseWizard.step >= step ? 'active' : 'pending'">{{ step }}</li>
            </ol>
            <template v-if="databaseWizard.step === 1">
              <h3>{{ t('databases.createDatabase') }}</h3>
              <label><span>{{ t('databases.databaseAlias') }}</span><input v-model="databaseWizard.databaseAlias" required maxlength="28" pattern="[a-z][a-z0-9_]{0,27}" spellcheck="false" autocomplete="off" :disabled="!canManageDatabases"><small>{{ t('databases.aliasHint') }}</small></label>
            </template>
            <fieldset v-else-if="databaseWizard.step === 2">
              <legend>{{ t('databases.chooseUser') }}</legend>
              <label class="check-field"><input v-model="databaseWizard.userMode" type="radio" value="new" :disabled="!canManageDatabases"><span>{{ t('databases.newUser') }}</span></label>
              <label class="check-field"><input v-model="databaseWizard.userMode" type="radio" value="existing" :disabled="!canManageDatabases || activeDatabaseUsers.length === 0"><span>{{ t('databases.existingUser') }}</span></label>
              <label v-if="databaseWizard.userMode === 'new'"><span>{{ t('databases.userAlias') }}</span><input v-model="databaseWizard.newUserAlias" required maxlength="28" pattern="[a-z][a-z0-9_]{0,27}" spellcheck="false" autocomplete="off"></label>
              <label v-else><span>{{ t('databases.databaseUser') }}</span><select v-model="databaseWizard.existingUserId" required><option value="" disabled>{{ t('databases.selectUser') }}</option><option v-for="user in activeDatabaseUsers" :key="user.id" :value="user.id">{{ user.alias }}@{{ user.host }}</option></select></label>
            </fieldset>
            <fieldset v-else-if="databaseWizard.step === 3">
              <legend>{{ t('databases.accessPreset') }}</legend>
              <label class="check-field"><input v-model="databaseWizard.preset" type="radio" value="read_write"><span>{{ t('databases.readWrite') }}</span></label>
              <p class="form-hint">{{ t('databases.readWriteHint') }}</p>
              <label class="check-field"><input v-model="databaseWizard.preset" type="radio" value="read_only"><span>{{ t('databases.readOnly') }}</span></label>
              <p class="form-hint">{{ t('databases.readOnlyHint') }}</p>
            </fieldset>
            <template v-else>
              <h3>{{ t('databases.review') }}</h3>
              <dl class="detail-list">
                <div><dt>{{ t('databases.databaseAlias') }}</dt><dd><code>{{ databaseWizard.databaseAlias }}</code></dd></div>
                <div><dt>{{ t('databases.databaseUser') }}</dt><dd><code>{{ databaseWizard.userMode === 'new' ? databaseWizard.newUserAlias : selectedDatabaseUser?.alias }}</code></dd></div>
                <div><dt>{{ t('databases.accessPreset') }}</dt><dd>{{ t(`databases.${databaseWizard.preset === 'read_write' ? 'readWrite' : 'readOnly'}`) }}</dd></div>
              </dl>
              <p class="form-hint">{{ t('databases.reviewHint') }}</p>
            </template>
            <div class="card-actions">
              <button v-if="databaseWizard.step > 1" class="text-action" type="button" @click="previousDatabaseWizardStep">{{ t('common.back') }}</button>
              <button v-if="databaseWizard.step < 4" class="primary-action" type="button" :disabled="actionBusy || !canManageDatabases || (databaseWizard.step === 1 ? !databaseAliasValid : databaseWizard.step === 2 ? !databaseUserStepValid : false)" @click="nextDatabaseWizardStep">{{ t('common.next') }}</button>
              <button v-else class="primary-action" type="submit" :disabled="actionBusy || !canManageDatabases">{{ t('databases.apply') }}</button>
            </div>
          </form>

          <div class="resource-list database-lists">
            <article v-for="database in databaseWorkspace.databases" :key="database.id" class="panel resource-card">
              <header><div><h2>{{ database.alias }}</h2><code>{{ t('databases.accountPrefixed') }}</code></div><span class="state-badge" :data-state="database.status">{{ t(`states.${database.status}`) }}</span></header>
              <dl class="detail-list compact"><div><dt>{{ t('databases.users') }}</dt><dd>{{ databaseWorkspace.grants.filter((grant) => grant.databaseId === database.id && grant.status === 'active').length }}</dd></div><div><dt>{{ t('common.updated') }}</dt><dd>{{ displayDate(database.updatedAt) }}</dd></div></dl>
              <p v-if="database.status === 'active' && canDeleteDatabases" class="form-hint">{{ t('databases.backupUnavailable') }}</p>
              <div v-if="database.status === 'active' && canDeleteDatabases" class="card-actions"><button class="danger-action" type="button" :disabled="actionBusy" @click="deleteDatabaseTarget('database', database)">{{ t('databases.deleteDatabase') }}</button></div>
            </article>
            <article v-for="user in databaseWorkspace.users" :key="user.id" class="panel resource-card">
              <header><div><h2>{{ user.alias }}</h2><code>@{{ user.host }}</code></div><span class="state-badge" :data-state="user.status">{{ t(`states.${user.status}`) }}</span></header>
              <dl class="detail-list compact"><div><dt>{{ t('databases.grants') }}</dt><dd>{{ databaseWorkspace.grants.filter((grant) => grant.databaseUserId === user.id && grant.status === 'active').length }}</dd></div><div><dt>{{ t('databases.credential') }}</dt><dd>{{ user.revealed ? t('databases.alreadyRevealed') : t('databases.notRevealed') }}</dd></div></dl>
              <p v-if="user.status === 'active' && databaseWorkspace.grants.some((grant) => grant.databaseUserId === user.id && grant.status === 'active')" class="form-hint">{{ t('databases.phpMyAdminHint') }}</p>
              <div v-if="user.status === 'active'" class="card-actions"><button v-if="databaseWorkspace.grants.some((grant) => grant.databaseUserId === user.id && grant.status === 'active')" class="primary-action" type="button" :disabled="actionBusy || !canManageDatabases" @click="launchPHPMyAdmin(user)">{{ t('databases.openPHPMyAdmin') }}</button><button v-if="!user.revealed" class="secondary-action" type="button" :disabled="actionBusy" @click="revealDatabaseCredential(user)">{{ t('databases.revealCredential') }}</button><button class="secondary-action" type="button" :disabled="actionBusy || !canManageDatabases" @click="rotateDatabaseCredential(user)">{{ t('databases.rotateCredential') }}</button><button v-if="canDeleteDatabases" class="danger-action" type="button" :disabled="actionBusy || databaseWorkspace.grants.some((grant) => grant.databaseUserId === user.id && grant.status !== 'revoked')" @click="deleteDatabaseTarget('user', user)">{{ t('databases.deleteUser') }}</button></div>
            </article>
            <div v-if="databaseWorkspace.databases.length === 0 && databaseWorkspace.users.length === 0" class="panel empty-state"><strong>{{ t('databases.empty') }}</strong><p>{{ t('databases.emptyBody') }}</p></div>
          </div>
        </div>
      </section>

      <section v-else-if="page === 'usage'" class="usage-grid">
        <article class="panel usage-plan-card"><div class="panel-heading"><h2>{{ selectedAccount.packageName }}</h2><span>{{ t('account.packageRevision', { revision: selectedAccount.packageRevision }) }}</span></div><p>{{ t('usage.limitExplanation') }}</p><div class="large-progress"><div><strong>{{ formatNumber(selectedAccount.usage.domains, activeLocale) }}</strong><span>{{ t('usage.ofDomains', { limit: formatNumber(selectedAccount.effectiveLimits.maxDomains, activeLocale) }) }}</span></div><progress max="1" :value="domainPercent" :aria-label="t('usage.domainUsage')">{{ formatPercent(domainPercent, activeLocale) }}</progress></div></article>
        <article class="panel table-panel"><div class="panel-heading"><h2>{{ t('usage.resourceLimits') }}</h2></div><dl class="detail-list quota-list"><div><dt>{{ t('packages.cpuShort') }}</dt><dd>{{ displayLimit(selectedAccount.effectiveLimits.cpuQuotaPercent, 'percent') }}</dd></div><div><dt>{{ t('packages.memoryShort') }}</dt><dd>{{ displayLimit(selectedAccount.effectiveLimits.memoryBytes, 'bytes') }}</dd></div><div><dt>{{ t('packages.storageShort') }}</dt><dd>{{ displayLimit(selectedAccount.effectiveLimits.storageBytes, 'bytes') }}</dd></div><div><dt>{{ t('packages.backupStorageShort') }}</dt><dd>{{ displayLimit(selectedAccount.effectiveLimits.backupStorageBytes ?? 20 * (1024 ** 3), 'bytes') }}</dd></div><div><dt>{{ t('packages.bandwidthShort') }}</dt><dd>{{ displayLimit(selectedAccount.effectiveLimits.monthlyCombinedBytes, 'bytes') }}</dd></div><div><dt>{{ t('usage.processes') }}</dt><dd>{{ displayLimit(selectedAccount.effectiveLimits.processLimit) }}</dd></div><div><dt>{{ t('usage.storageInodes') }}</dt><dd>{{ displayLimit(selectedAccount.effectiveLimits.storageInodes) }}</dd></div></dl><p class="measurement-note">{{ t('usage.telemetryDeferred') }}</p></article>
      </section>

      <section v-else-if="page === 'profile'" class="settings-grid">
        <form class="panel management-form profile-form" @submit.prevent="submitProfile"><div class="panel-heading"><h2>{{ t('profile.edit') }}</h2></div><label><span>{{ t('auth.displayName') }}</span><input v-model="profileForm.displayName" required maxlength="120" autocomplete="name"></label><label><span>{{ t('auth.email') }}</span><input v-model="profileForm.email" required type="email" maxlength="254" autocomplete="email"></label><label><span>{{ t('settings.locale') }}</span><select v-model="profileForm.locale"><option value="en">{{ t('localeNames.en') }}</option><option value="de">{{ t('localeNames.de') }}</option></select></label><p class="form-hint">{{ t('profile.freshAuthHint') }}</p><button class="primary-action" type="submit" :disabled="actionBusy">{{ t('profile.save') }}</button></form>
        <article class="panel profile-summary"><p class="eyebrow">{{ t('settings.identity') }}</p><h2>{{ session.identity.displayName }}</h2><p>{{ session.identity.email }}</p><dl class="detail-list"><div><dt>{{ t('settings.authenticationLevel') }}</dt><dd>{{ session.authenticationLevel }}</dd></div><div><dt>{{ t('settings.sessionExpires') }}</dt><dd>{{ displayDate(session.expiresAt) }}</dd></div></dl><button class="danger-action" type="button" :disabled="actionBusy" @click="emit('logout')">{{ t('auth.signOut') }}</button></article>
      </section>

      <section v-else class="sessions-layout">
        <div class="session-toolbar"><p>{{ t('sessions.description') }}</p><button class="danger-action" type="button" :disabled="actionBusy || otherSessions === 0" @click="emit('revokeOtherSessions')">{{ t('sessions.revokeOthers') }}</button></div>
        <div class="resource-list session-list"><article v-for="item in sessions" :key="item.id" class="panel session-card"><header><div><h2>{{ item.current ? t('sessions.thisDevice') : t('sessions.otherDevice') }}</h2><code>{{ item.sourceAddress || t('common.notAvailable') }}</code></div><span v-if="item.current" class="state-badge" data-state="active">{{ t('sessions.current') }}</span></header><dl class="detail-list compact"><div><dt>{{ t('sessions.userAgent') }}</dt><dd class="wrapped-value">{{ item.userAgent || t('common.notAvailable') }}</dd></div><div><dt>{{ t('sessions.lastSeen') }}</dt><dd>{{ displayDate(item.lastSeenAt) }}</dd></div><div><dt>{{ t('settings.authenticationLevel') }}</dt><dd>{{ item.authenticationLevel }}</dd></div><div><dt>{{ t('settings.sessionExpires') }}</dt><dd>{{ displayDate(item.expiresAt) }}</dd></div></dl><div class="card-actions"><button class="danger-action" type="button" :disabled="actionBusy" @click="revokeManagedSession(item)">{{ item.current ? t('auth.signOut') : t('sessions.revoke') }}</button></div></article><div v-if="sessions.length === 0" class="panel empty-state"><strong>{{ t('sessions.empty') }}</strong><p>{{ t('sessions.emptyBody') }}</p></div></div>
      </section>
    </template>
  </div>
</template>
