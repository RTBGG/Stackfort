// SPDX-License-Identifier: AGPL-3.0-or-later

export type BootstrapStatus = {
  required: boolean
  capabilityActive: boolean
  expiresAt?: string
}

export type Identity = {
  id: string
  email: string
  displayName: string
  locale: 'en' | 'de'
}

export type Session = {
  identity: Identity
  sessionId: string
  authenticatedAt: string
  lastSeenAt: string
  expiresAt: string
  authenticationLevel: string
  mfaAuthenticatedAt?: string
}

export type PackageLimits = {
  maxDomains: number
  maxDatabases: number
  maxDatabaseUsers: number
  maxScheduledJobs: number
  maxOciApplications: number
  cpuQuotaPercent?: number
  cpuWeight?: number
  memoryBytes?: number
  swapBytes?: number
  processLimit?: number
  storageBytes?: number
  backupStorageBytes?: number
  storageInodes?: number
  readBytesPerSecond?: number
  writeBytesPerSecond?: number
  readIops?: number
  writeIops?: number
  monthlyIngressBytes?: number
  monthlyEgressBytes?: number
  monthlyCombinedBytes?: number
  allowedPhpVersions: string[]
  features: {
    ociApplications: boolean
    customRedirects: boolean
    wafExceptions: boolean
    scheduledBackups: boolean
  }
}

export type HostingPackage = {
  id: string
  name: string
  slug: string
  status: 'active' | 'archived'
  currentRevision: number
  limits: PackageLimits
  createdAt: string
  updatedAt: string
}

export type HostingAccount = {
  id: string
  name: string
  slug: string
  status: 'active' | 'suspended' | 'archived'
  currentPackageAssignmentId: string
  packageId?: string
  packageName?: string
  packageRevision?: number
  hostReady: boolean
  provisioningOperationId?: string
  provisioningStatus?: 'pending' | 'running' | 'succeeded' | 'failed' | 'cancelling' | 'cancelled'
  createdAt: string
  updatedAt: string
}

export type AccountWorkspace = {
  id: string
  name: string
  slug: string
  status: 'active' | 'suspended'
  membershipRole: 'owner' | 'member' | 'auditor'
  packageId: string
  packageName: string
  packageRevision: number
  hostReady: boolean
  effectiveLimits: PackageLimits
  usage: { domains: number }
  createdAt: string
  updatedAt: string
}

export type SelfServiceContext = {
  platformAdministrator: boolean
  accounts: AccountWorkspace[]
}

export type Domain = {
  id: string
  name: { display: string; ascii: string }
  status: 'pending' | 'active' | 'suspended' | 'removed'
  canonicalMode: 'prefer_apex' | 'prefer_www' | 'serve_both'
  waf: { mode: 'off' | 'detection_only' | 'blocking_pl1' }
  cache?: { preset: 'disabled' | 'respect_origin' | 'wordpress' }
  target: {
    id: string
    type: 'static' | 'php' | 'oci_application' | 'redirect'
    documentRoot?: { id: string; relativePath: string; referenceCount: number }
    phpVersion?: string
    redirect?: { statusCode: 301 | 302; targetUrl: string }
  }
  tls: {
    enabled: boolean
    issuanceStatus: string
    expiresAt?: string
  }
  createdAt: string
  updatedAt: string
}

export type DomainWAFException = {
	id: string
	ruleId: number
	requestPath?: string
	parameter?: string
	expiresAt: string
	createdAt: string
}

export type CacheStatus = {
  preset: NonNullable<Domain['cache']>['preset']
  metrics: {
    domainAscii: string
    hits: number
    misses: number
    bypasses: number
    windowRecords: number
  }
}

export type DomainTargetInput = {
  type: 'static' | 'php'
  rootMode: 'default' | 'custom'
  documentRoot?: string
  phpVersion?: string
}

export type AccountPHPStatus = {
  runtimeCapability: { status: 'available' | 'unavailable' | 'unsupported' | 'unknown'; reasonCode?: string }
  hostApprovedVersions: string[]
  packageAllowedVersions: string[]
  availableVersions: string[]
  pools: Array<{
    version: string
    state: 'missing' | 'inactive' | 'active' | 'failed'
    configuredDomains: number
    memoryBytes?: number
    cpuTimeNanoseconds?: number
    processes?: number
  }>
}

export type ManagedDatabase = {
  id: string
  alias: string
  status: 'pending' | 'active' | 'deleting' | 'error' | 'deleted'
  createdAt: string
  updatedAt: string
}

export type ManagedDatabaseUser = {
  id: string
  alias: string
  host: 'localhost'
  status: 'pending' | 'active' | 'deleting' | 'error' | 'deleted'
  revealed: boolean
  createdAt: string
  updatedAt: string
}

export type ManagedDatabaseGrant = {
  id: string
  databaseId: string
  databaseUserId: string
  preset: 'read_only' | 'read_write'
  status: 'pending' | 'active' | 'revoking' | 'error' | 'revoked'
}

export type DatabaseWorkspace = {
  databases: ManagedDatabase[]
  users: ManagedDatabaseUser[]
  grants: ManagedDatabaseGrant[]
}

export type FileEntry = {
	name: string
	type: 'directory' | 'file' | 'symlink' | 'other'
	sizeBytes: number
	mode: number
	modifiedAt: string
	hidden: boolean
}

export type FileListing = {
	path: string
	entries: FileEntry[]
	next?: string
	omittedEntries: number
}

export type FileUpload = {
  uploadId: string
  directory: string
  name: string
  sizeBytes: number
  receivedBytes: number
  sha256?: string
  createdAt: string
  completed?: boolean
}

export type FileMutationResult = {
  operationId?: string
  trashId?: string
  sourceDirectory?: string
  sourceName?: string
  directory?: string
  name?: string
	archiveFormat?: 'zip' | 'tar_gzip'
	entryCount?: number
	sizeBytes?: number
  completed: boolean
}

export type FileTrashEntry = {
  trashId: string
  directory: string
  name: string
  type: 'directory' | 'file'
  sizeBytes: number
  trashedAt: string
}

export type FileTrashListing = {
  trashEntries: FileTrashEntry[]
  next?: string
}

export type BackupRecord = {
  schemaVersion: number
  backupId: string
  accountId: string
  scope: 'account_files' | 'document_root'
  sourcePath?: string
  createdAt: string
  payloadBytes: number
  contentBytes: number
  entryCount: number
  payloadSha256: string
  manifestSha256: string
  manifestAuthenticated: boolean
  payloadVerified: boolean
}

export type BackupResult = {
	uploadId?: string
	sizeBytes?: number
	receivedBytes?: number
	createdAt?: string
  operationId?: string
  completed?: boolean
  backup?: BackupRecord
  backups?: BackupRecord[]
  next?: string
	backupRepository?: BackupRepositoryStatus
}

export type BackupRepositoryStatus = {
	usedBytes: number
	limitBytes: number
	backupCount: number
	maximumBackups: number
	activeUploads: number
}

export type HostingLogRecord = {
  timestamp: string
  level: 'info' | 'notice' | 'warn' | 'error' | 'crit' | 'alert' | 'emerg'
  clientAddress?: string
  host?: string
  method?: string
  path?: string
  status?: number
  bytes?: number
  durationMs?: number
  message?: string
}

export type HostingLogPage = {
  domain: { display: string; ascii: string }
  kind: 'access' | 'error'
  records: HostingLogRecord[]
  next?: string
  retentionDays: number
  maximumActiveBytes: number
  sensitiveRedaction: boolean
  queryStringsStored: boolean
}

export type WAFEvent = {
  id: string
  timestamp: string
  ruleId: number
  category: 'protocol' | 'protocol_attack' | 'local_file_inclusion' | 'remote_file_inclusion' |
    'remote_code_execution' | 'php_attack' | 'cross_site_scripting' | 'sql_injection' |
    'session_attack' | 'java_attack' | 'anomaly_threshold' | 'request_validation' | 'other'
  severity: 'emergency' | 'alert' | 'critical' | 'error' | 'warning' | 'notice' | 'info'
  outcome: 'detected' | 'blocked'
  method?: string
  path?: string
  correlationId?: string
}

export type WAFEventPage = {
  domain: { display: string; ascii: string }
  events: WAFEvent[]
  next?: string
  retentionDays: number
  maximumActiveBytes: number
  nativeDataWithheld: boolean
  queryStringsStored: boolean
}

export type DatabaseWizardOperation = {
  operationId: string
  databaseId: string
  databaseUserId: string
  grantId: string
  status: Operation['status']
}

export type DatabaseCredential = { username: string; host: string; password: string }

export type PHPMyAdminHandoff = {
  handoffToken: string
  expiresAt: string
  launchPath: '/phpmyadmin/stackfort-launch.php'
}

export type Operation = {
  id: string
  accountId?: string
  kind: string
  status: 'pending' | 'running' | 'succeeded' | 'failed' | 'cancelling' | 'cancelled'
  stage: string
  progressPercent: number
  errorCode?: string
  attemptCount: number
  maxAttempts: number
  createdAt: string
  updatedAt: string
}

export type ScheduledJobSchedule = {
  kind: 'interval' | 'hourly' | 'daily' | 'weekly'
  intervalMinutes: number
  hourUtc: number
  minuteUtc: number
  weekday?: 'mon' | 'tue' | 'wed' | 'thu' | 'fri' | 'sat' | 'sun'
}

export type ScheduledJob = {
  id: string
  accountId: string
  name: string
  runtime: 'shell' | 'php'
  scriptPath: string
  phpVersion?: string
  schedule: ScheduledJobSchedule
  enabled: boolean
  status: 'pending' | 'active' | 'disabled' | 'deleting' | 'error' | 'deleted'
  revision: number
  appliedRevision?: number
  createdAt: string
  updatedAt: string
}

export type ScheduledJobInput = {
  expectedRevision?: number
  name: string
  runtime: ScheduledJob['runtime']
  scriptPath: string
  phpVersion?: string
  schedule: ScheduledJobSchedule
  enabled: boolean
}

export type ScheduledJobMutation = {
  operationId: string
  status: Operation['status']
  job: ScheduledJob
}

export type TLSCertificate = {
  id: string
  status: 'ordering' | 'staged' | 'active' | 'retired'
  names: string[]
  issuer?: string
  serialHex?: string
  fingerprintSha256?: string
  notBefore?: string
  expiresAt?: string
  nextRenewalAt?: string
  createdAt: string
  activatedAt?: string
  retiredAt?: string
}

export type AuditEvent = {
  sequence: number
  id: string
  occurredAt: string
  actorId?: string
  sourceAddress?: string
  action: string
  targetType: string
  targetId?: string
  accountId?: string
  operationId?: string
  result: 'success' | 'failure' | 'denied'
  details: Record<string, unknown>
}

export type HostCapabilities = {
  inspectedAt: string
  managedPhpVersions: string[]
  platform: {
    distributionId: string
    versionId: string
    architecture: string
    kernelRelease: string
    support: { status: string; reasonCode?: string }
  }
  services: Array<{
    key: string
    unit: string
    activeState: string
    subState: string
    availability: { status: string; reasonCode?: string }
  }>
}

export type BuildInfo = { version: string; commit: string; buildDate: string }
export type DomainOperation = { operationId: string; domainId: string; status: string }

export type ACMEAccount = {
  id: string
  environment: 'letsencrypt-staging' | 'letsencrypt-production'
  directoryUrl: string
  contactEmail: string
  status: 'pending' | 'valid' | 'deactivated' | 'revoked'
  termsAgreedAt: string
  createdAt: string
  updatedAt: string
  registeredAt?: string
}

export type ManagedSession = {
  id: string
  current: boolean
  createdAt: string
  authenticatedAt: string
  lastSeenAt: string
  expiresAt: string
  sourceAddress: string
  userAgent: string
  authenticationLevel: string
  mfaAuthenticatedAt?: string
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

type RequestOptions = {
	method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
  body?: unknown
  csrf?: boolean
  idempotent?: boolean
}

async function uploadFileChunk(
	path: string, offset: number, chunk: Blob, signal?: AbortSignal,
): Promise<FileUpload> {
	const token = cookieValue('__Host-sf-csrf')
	if (!token) throw new ApiError(0, 'csrf_token_missing', 'The browser CSRF token is unavailable.')
	const response = await fetch(path, {
		method: 'PUT', credentials: 'same-origin', signal, body: chunk,
		headers: {
			Accept: 'application/json', 'Content-Type': 'application/octet-stream',
			'Upload-Offset': String(offset), 'X-Request-ID': requestIdentifier(), 'X-CSRF-Token': token,
		},
	})
	if (!response.ok) {
		const error = await response.json().catch(() => ({})) as { code?: string; message?: string }
		throw new ApiError(response.status, error.code ?? 'request_failed', error.message ?? 'The request failed.')
	}
	return response.json() as Promise<FileUpload>
}

async function uploadBackupChunk(path: string, offset: number, chunk: Blob, signal?: AbortSignal): Promise<BackupResult> {
	const token = cookieValue('__Host-sf-csrf')
	if (!token) throw new ApiError(0, 'csrf_token_missing', 'The browser CSRF token is unavailable.')
	const response = await fetch(path, { method: 'PUT', credentials: 'same-origin', signal, body: chunk, headers: {
		Accept: 'application/json', 'Content-Type': 'application/octet-stream', 'Upload-Offset': String(offset),
		'X-Request-ID': requestIdentifier(), 'X-CSRF-Token': token,
	} })
	if (!response.ok) {
		const error = await response.json().catch(() => ({})) as { code?: string; message?: string }
		throw new ApiError(response.status, error.code ?? 'request_failed', error.message ?? 'The request failed.')
	}
	return response.json() as Promise<BackupResult>
}

function cookieValue(name: string): string {
  const prefix = `${encodeURIComponent(name)}=`
  const match = document.cookie.split(';').map((part) => part.trim())
    .find((part) => part.startsWith(prefix))
  return match ? decodeURIComponent(match.slice(prefix.length)) : ''
}

function requestIdentifier(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID()
  return `browser-${Date.now()}-${Math.random().toString(16).slice(2)}`
}

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const method = options.method ?? 'GET'
  const headers = new Headers({ Accept: 'application/json' })
  if (options.body !== undefined) headers.set('Content-Type', 'application/json')
  if (method !== 'GET') headers.set('X-Request-ID', requestIdentifier())
  if (options.idempotent) headers.set('Idempotency-Key', requestIdentifier())
  if (options.csrf) {
    const token = cookieValue('__Host-sf-csrf')
    if (!token) throw new ApiError(0, 'csrf_token_missing', 'The browser CSRF token is unavailable.')
    headers.set('X-CSRF-Token', token)
  }
  const response = await fetch(path, {
    method,
    headers,
    credentials: 'same-origin',
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
  })
  if (!response.ok) {
    const error = await response.json().catch(() => ({})) as { code?: string; message?: string }
    throw new ApiError(response.status, error.code ?? 'request_failed', error.message ?? 'The request failed.')
  }
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}

export const api = {
  bootstrapStatus: () => request<BootstrapStatus>('/api/v1/bootstrap'),
  bootstrapAdministrator: (input: {
    token: string; email: string; displayName: string; password: string; locale: 'en' | 'de'
  }) => request<Identity>('/api/v1/bootstrap', { method: 'POST', body: input }),
  session: () => request<Session>('/api/v1/session'),

  selfContext: () => request<SelfServiceContext>('/api/v1/me'),
  updateProfile: (input: { email: string; displayName: string; locale: 'en' | 'de' }) =>
    request<Identity>('/api/v1/me/profile', { method: 'PATCH', body: input, csrf: true }),
  login: async (email: string, password: string): Promise<
    { kind: 'session'; session: Session } | { kind: 'mfa'; expiresAt: string }
  > => {
    const response = await request<Session | { mfaRequired: true; expiresAt: string }>('/api/v1/login', {
      method: 'POST', body: { email, password },
    })
    return 'mfaRequired' in response
      ? { kind: 'mfa', expiresAt: response.expiresAt }
      : { kind: 'session', session: response }
  },
  completeMFA: (code: string) => request<Session>('/api/v1/login/mfa', {
    method: 'POST', body: { code },
  }),
  logout: () => request<void>('/api/v1/logout', { method: 'POST', csrf: true }),
  health: () => request<{ status: string }>('/api/v1/health'),
  build: () => request<BuildInfo>('/api/v1/build'),
  packages: async () => (await request<{ packages: HostingPackage[] }>('/api/v1/admin/packages')).packages,
  createPackage: (input: { name: string; slug: string; limits: PackageLimits }) =>
    request<HostingPackage>('/api/v1/admin/packages', { method: 'POST', body: input, csrf: true }),
  accounts: async () => (await request<{ accounts: HostingAccount[] }>('/api/v1/admin/accounts')).accounts,
  createAccount: (input: { name: string; slug: string; packageId: string; ownerIdentityId?: string }) =>
    request<HostingAccount>('/api/v1/admin/accounts', { method: 'POST', body: input, csrf: true }),
  domains: async (accountId: string) => (
    await request<{ domains: Domain[] }>(`/api/v1/accounts/${encodeURIComponent(accountId)}/domains`)
  ).domains,
  accountOperation: (accountId: string, operationId: string) => request<Operation>(
    `/api/v1/accounts/${encodeURIComponent(accountId)}/operations/${encodeURIComponent(operationId)}`,
  ),
  certificates: async (accountId: string, domainId: string) => (
    await request<{ certificates: TLSCertificate[] }>(
      `/api/v1/accounts/${encodeURIComponent(accountId)}/domains/${encodeURIComponent(domainId)}/tls/certificates`,
    )
  ).certificates,
  createDomain: (accountId: string, input: {
    name: string
    canonicalMode: Domain['canonicalMode']
    target: DomainTargetInput
    disableTls: boolean
    tlsMode?: 'acme'
    wafMode: Domain['waf']['mode']
    cachePreset: NonNullable<Domain['cache']>['preset']
  }) => request<DomainOperation>(`/api/v1/accounts/${encodeURIComponent(accountId)}/domains`, {
    method: 'POST', body: input, csrf: true, idempotent: true,
  }),
  updateDomain: (accountId: string, domainId: string, input: {
    canonicalMode: Domain['canonicalMode']
    target: DomainTargetInput
    wafMode: Domain['waf']['mode']
    cachePreset: NonNullable<Domain['cache']>['preset']
  }) => request<DomainOperation>(
    `/api/v1/accounts/${encodeURIComponent(accountId)}/domains/${encodeURIComponent(domainId)}`,
    { method: 'PATCH', body: input, csrf: true, idempotent: true },
  ),
  cacheStatus: (accountId: string, domainId: string) => request<CacheStatus>(
    `/api/v1/accounts/${encodeURIComponent(accountId)}/domains/${encodeURIComponent(domainId)}/cache`,
  ),
  purgeCache: (accountId: string, domainId: string, pathPrefix: string) => request<DomainOperation>(
    `/api/v1/accounts/${encodeURIComponent(accountId)}/domains/${encodeURIComponent(domainId)}/cache/purge`,
    { method: 'POST', body: { pathPrefix }, csrf: true, idempotent: true },
  ),
  domainAction: (accountId: string, domainId: string, action: 'suspend' | 'resume' | 'remove') => {
    const suffix = action === 'remove' ? '' : `/${action}`
    return request<DomainOperation>(
      `/api/v1/accounts/${encodeURIComponent(accountId)}/domains/${encodeURIComponent(domainId)}${suffix}`,
      { method: action === 'remove' ? 'DELETE' : 'POST', csrf: true, idempotent: true },
    )
  },
  wafExceptions: async (accountId: string, domainId: string) => (
    await request<{ exceptions: DomainWAFException[] }>(
      `/api/v1/admin/accounts/${encodeURIComponent(accountId)}/domains/${encodeURIComponent(domainId)}/waf-exceptions`,
    )
  ).exceptions,
  createWAFException: (accountId: string, domainId: string, input: {
    ruleId: number; requestPath?: string; parameter?: string; expiresAt: string
  }) => request<DomainOperation>(
    `/api/v1/admin/accounts/${encodeURIComponent(accountId)}/domains/${encodeURIComponent(domainId)}/waf-exceptions`,
    { method: 'POST', body: input, csrf: true, idempotent: true },
  ),
  removeWAFException: (accountId: string, domainId: string, exceptionId: string) => request<DomainOperation>(
    `/api/v1/admin/accounts/${encodeURIComponent(accountId)}/domains/${encodeURIComponent(domainId)}/waf-exceptions/${encodeURIComponent(exceptionId)}`,
    { method: 'DELETE', csrf: true, idempotent: true },
  ),
  hostCapabilities: () => request<HostCapabilities>('/api/v1/admin/host/capabilities'),
  accountPHP: (accountId: string) => request<AccountPHPStatus>(
    `/api/v1/accounts/${encodeURIComponent(accountId)}/php`,
  ),
  scheduledJobs: async (accountId: string) => (
    await request<{ jobs: ScheduledJob[] }>(`/api/v1/accounts/${encodeURIComponent(accountId)}/jobs`)
  ).jobs,
  createScheduledJob: (accountId: string, input: ScheduledJobInput) => request<ScheduledJobMutation>(
    `/api/v1/accounts/${encodeURIComponent(accountId)}/jobs`,
    { method: 'POST', body: input, csrf: true, idempotent: true },
  ),
  updateScheduledJob: (accountId: string, jobId: string, input: ScheduledJobInput) => request<ScheduledJobMutation>(
    `/api/v1/accounts/${encodeURIComponent(accountId)}/jobs/${encodeURIComponent(jobId)}`,
    { method: 'PUT', body: input, csrf: true, idempotent: true },
  ),
  deleteScheduledJob: (accountId: string, jobId: string, expectedRevision: number) => request<ScheduledJobMutation>(
    `/api/v1/accounts/${encodeURIComponent(accountId)}/jobs/${encodeURIComponent(jobId)}`,
    { method: 'DELETE', body: { expectedRevision }, csrf: true, idempotent: true },
  ),
  scheduledJobOperation: (accountId: string, operationId: string) => request<Operation>(
    `/api/v1/accounts/${encodeURIComponent(accountId)}/job-operations/${encodeURIComponent(operationId)}`,
  ),
  databaseWorkspace: (accountId: string) => request<DatabaseWorkspace>(
    `/api/v1/accounts/${encodeURIComponent(accountId)}/databases`,
  ),
  files: (accountId: string, path = '', cursor = '') => {
    const query = new URLSearchParams()
    if (path) query.set('path', path)
    if (cursor) query.set('cursor', cursor)
    const suffix = query.size > 0 ? `?${query.toString()}` : ''
    return request<FileListing>(`/api/v1/accounts/${encodeURIComponent(accountId)}/files${suffix}`)
  },
  fileDownloadURL: (accountId: string, path: string) => {
    const query = new URLSearchParams({ path })
    return `/api/v1/accounts/${encodeURIComponent(accountId)}/files/download?${query.toString()}`
  },
	initiateFileUpload: (accountId: string, input: {
		directory: string; name: string; sizeBytes: number; expectedSha256?: string
	}) => request<FileUpload>(`/api/v1/accounts/${encodeURIComponent(accountId)}/file-uploads`, {
		method: 'POST', body: input, csrf: true, idempotent: true,
	}),
	fileUploadStatus: (accountId: string, uploadId: string) => request<FileUpload>(
		`/api/v1/accounts/${encodeURIComponent(accountId)}/file-uploads/${encodeURIComponent(uploadId)}`,
	),
	uploadFileChunk: (accountId: string, uploadId: string, offset: number, chunk: Blob, signal?: AbortSignal) =>
		uploadFileChunk(
			`/api/v1/accounts/${encodeURIComponent(accountId)}/file-uploads/${encodeURIComponent(uploadId)}`,
			offset, chunk, signal,
		),
	completeFileUpload: (accountId: string, uploadId: string, input: {
		directory: string; name: string; sizeBytes: number; expectedSha256?: string
	}) => request<FileUpload>(
		`/api/v1/accounts/${encodeURIComponent(accountId)}/file-uploads/${encodeURIComponent(uploadId)}/complete`,
		{ method: 'POST', body: input, csrf: true, idempotent: true },
	),
	cancelFileUpload: (accountId: string, uploadId: string) => request<void>(
		`/api/v1/accounts/${encodeURIComponent(accountId)}/file-uploads/${encodeURIComponent(uploadId)}`,
		{ method: 'DELETE', csrf: true, idempotent: true },
	),
	createFileNode: (accountId: string, input: { directory: string; name: string; type: 'file' | 'directory' }) =>
		request<FileUpload>(`/api/v1/accounts/${encodeURIComponent(accountId)}/file-nodes`, {
			method: 'POST', body: input, csrf: true, idempotent: true,
		}),
	mutateFileNode: (accountId: string, input: {
		action: 'rename' | 'move' | 'copy'
		sourceDirectory: string
		sourceName: string
		destinationDirectory: string
		destinationName: string
	}) => request<FileMutationResult>(`/api/v1/accounts/${encodeURIComponent(accountId)}/file-operations`, {
		method: 'POST', body: input, csrf: true, idempotent: true,
	}),
	mutateFileArchive: (accountId: string, input: {
		action: 'create' | 'extract'
		format: 'zip' | 'tar_gzip'
		sourceDirectory: string
		sourceName: string
		destinationDirectory: string
		destinationName: string
	}) => request<FileMutationResult>(`/api/v1/accounts/${encodeURIComponent(accountId)}/file-archives`, {
		method: 'POST', body: input, csrf: true, idempotent: true,
	}),
	trashFileNode: (accountId: string, input: { directory: string; name: string }) =>
		request<FileMutationResult>(`/api/v1/accounts/${encodeURIComponent(accountId)}/file-trash`, {
			method: 'POST', body: input, csrf: true, idempotent: true,
		}),
	fileTrash: (accountId: string, cursor = '') => {
		const query = cursor ? `?${new URLSearchParams({ cursor }).toString()}` : ''
		return request<FileTrashListing>(`/api/v1/accounts/${encodeURIComponent(accountId)}/file-trash${query}`)
	},
	restoreTrash: (accountId: string, trashId: string) => request<FileMutationResult>(
		`/api/v1/accounts/${encodeURIComponent(accountId)}/file-trash/${encodeURIComponent(trashId)}/restore`,
		{ method: 'POST', csrf: true, idempotent: true },
	),
	purgeTrash: (accountId: string, trashId: string) => request<void>(
		`/api/v1/accounts/${encodeURIComponent(accountId)}/file-trash/${encodeURIComponent(trashId)}`,
		{ method: 'DELETE', csrf: true, idempotent: true },
	),
  backups: (accountId: string, cursor = '') => {
    const query = cursor ? `?${new URLSearchParams({ cursor }).toString()}` : ''
    return request<BackupResult>(`/api/v1/accounts/${encodeURIComponent(accountId)}/backups${query}`)
  },
  hostingLogs: (accountId: string, domainId: string, kind: 'access' | 'error', cursor = '') => {
    const query = new URLSearchParams({ domainId, kind })
    if (cursor) query.set('cursor', cursor)
    return request<HostingLogPage>(
      `/api/v1/accounts/${encodeURIComponent(accountId)}/logs?${query.toString()}`,
    )
  },
  wafEvents: (accountId: string, domainId: string, cursor = '') => {
    const query = new URLSearchParams({ domainId })
    if (cursor) query.set('cursor', cursor)
    return request<WAFEventPage>(
      `/api/v1/accounts/${encodeURIComponent(accountId)}/waf-events?${query.toString()}`,
    )
  },
  createBackup: (accountId: string, input: {
    scope: BackupRecord['scope']; sourcePath?: string
  }) => request<BackupResult>(`/api/v1/accounts/${encodeURIComponent(accountId)}/backups`, {
    method: 'POST', body: input, csrf: true, idempotent: true,
  }),
  inspectBackup: (accountId: string, backupId: string) => request<BackupResult>(
    `/api/v1/accounts/${encodeURIComponent(accountId)}/backups/${encodeURIComponent(backupId)}`,
  ),
  verifyBackup: (accountId: string, backupId: string) => request<BackupResult>(
    `/api/v1/accounts/${encodeURIComponent(accountId)}/backups/${encodeURIComponent(backupId)}/verify`,
    { method: 'POST', body: {}, csrf: true },
  ),
  restoreBackup: (accountId: string, backupId: string, confirmation: string) => request<BackupResult>(
    `/api/v1/accounts/${encodeURIComponent(accountId)}/backups/${encodeURIComponent(backupId)}/restore`,
    { method: 'POST', body: { confirmation }, csrf: true, idempotent: true },
  ),
	backupDownloadURL: (accountId: string, backupId: string) =>
		`/api/v1/accounts/${encodeURIComponent(accountId)}/backups/${encodeURIComponent(backupId)}/download`,
	initiateBackupUpload: (accountId: string, input: {
		scope: BackupRecord['scope']; sourcePath?: string; sizeBytes: number; expectedSha256?: string
	}) => request<BackupResult>(`/api/v1/accounts/${encodeURIComponent(accountId)}/backup-uploads`, {
		method: 'POST', body: input, csrf: true, idempotent: true,
	}),
	backupUploadStatus: (accountId: string, uploadId: string) => request<BackupResult>(
		`/api/v1/accounts/${encodeURIComponent(accountId)}/backup-uploads/${encodeURIComponent(uploadId)}`,
	),
	uploadBackupChunk: (accountId: string, uploadId: string, offset: number, chunk: Blob, signal?: AbortSignal) =>
		uploadBackupChunk(`/api/v1/accounts/${encodeURIComponent(accountId)}/backup-uploads/${encodeURIComponent(uploadId)}`,
			offset, chunk, signal),
	completeBackupUpload: (accountId: string, uploadId: string, input: {
		scope: BackupRecord['scope']; sourcePath?: string; sizeBytes: number; expectedSha256?: string
	}) => request<BackupResult>(
		`/api/v1/accounts/${encodeURIComponent(accountId)}/backup-uploads/${encodeURIComponent(uploadId)}/complete`,
		{ method: 'POST', body: input, csrf: true, idempotent: true },
	),
	cancelBackupUpload: (accountId: string, uploadId: string) => request<BackupResult>(
		`/api/v1/accounts/${encodeURIComponent(accountId)}/backup-uploads/${encodeURIComponent(uploadId)}`,
		{ method: 'DELETE', csrf: true, idempotent: true },
	),
	deleteBackup: (accountId: string, backupId: string, confirmation: string) => request<BackupResult>(
		`/api/v1/accounts/${encodeURIComponent(accountId)}/backups/${encodeURIComponent(backupId)}`,
		{ method: 'DELETE', body: { confirmation }, csrf: true, idempotent: true },
	),
  createDatabase: (accountId: string, input: {
    databaseAlias: string
    existingUserId?: string
    newUserAlias?: string
    preset: 'read_only' | 'read_write'
  }) => request<DatabaseWizardOperation>(
    `/api/v1/accounts/${encodeURIComponent(accountId)}/databases/wizard`,
    { method: 'POST', body: input, csrf: true, idempotent: true },
  ),
  revealDatabaseCredential: (accountId: string, userId: string) => request<DatabaseCredential>(
    `/api/v1/accounts/${encodeURIComponent(accountId)}/database-users/${encodeURIComponent(userId)}/credential/reveal`,
    { method: 'POST', csrf: true },
  ),
  phpMyAdminHandoff: (accountId: string, userId: string) => request<PHPMyAdminHandoff>(
    `/api/v1/accounts/${encodeURIComponent(accountId)}/database-users/${encodeURIComponent(userId)}/phpmyadmin-handoffs`,
    { method: 'POST', csrf: true },
  ),
  rotateDatabaseCredential: (accountId: string, userId: string) => request<{
    operationId: string; status: Operation['status']
  }>(
    `/api/v1/accounts/${encodeURIComponent(accountId)}/database-users/${encodeURIComponent(userId)}/credential/rotate`,
    { method: 'POST', csrf: true, idempotent: true },
  ),
  deleteDatabaseTarget: (
    accountId: string, targetKind: 'database' | 'user', targetId: string, confirmation: string,
  ) => request<{ operationId: string; status: string }>(
    `/api/v1/accounts/${encodeURIComponent(accountId)}/${targetKind === 'database' ? 'databases' : 'database-users'}/${encodeURIComponent(targetId)}`,
    { method: 'DELETE', body: { confirmation }, csrf: true, idempotent: true },
  ),
  operations: async () => (
    await request<{ operations: Operation[] }>('/api/v1/admin/operations?limit=50')
  ).operations,
  auditEvents: async () => (
    await request<{ events: AuditEvent[] }>('/api/v1/admin/audit-events?limit=50')
  ).events,
  acmeAccounts: async () => (
    await request<{ accounts: ACMEAccount[] }>('/api/v1/admin/acme/accounts')
  ).accounts,
  registerACMEAccount: (input: {
    environment: 'letsencrypt-production'
    contactEmail: string
    termsAccepted: boolean
  }) => request<{ operationId: string; status: string }>('/api/v1/admin/acme/accounts', {
    method: 'POST', body: input, csrf: true, idempotent: true,
  }),
  sessions: async () => (await request<{ sessions: ManagedSession[] }>('/api/v1/sessions')).sessions,
  revokeSession: (sessionId: string) => request<void>(
    `/api/v1/sessions/${encodeURIComponent(sessionId)}`,
    { method: 'DELETE', csrf: true },
  ),
  revokeOtherSessions: () => request<{ revoked: number; currentRevoked: boolean }>(
    '/api/v1/sessions/revoke-all',
    { method: 'POST', body: { keepCurrent: true }, csrf: true },
  ),
  issueCertificate: (accountId: string, domainId: string) => request<DomainOperation>(
    `/api/v1/accounts/${encodeURIComponent(accountId)}/domains/${encodeURIComponent(domainId)}/tls/issue`,
    { method: 'POST', csrf: true, idempotent: true },
  ),
}
