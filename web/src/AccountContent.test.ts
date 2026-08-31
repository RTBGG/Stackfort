// SPDX-License-Identifier: AGPL-3.0-or-later
// @vitest-environment jsdom

import axe from 'axe-core'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import AccountContent from './AccountContent.vue'
import { api } from './api'
import type { AccountPHPStatus, AccountWorkspace, Domain, ManagedSession, Operation, ScheduledJob, Session } from './api'
import { messages } from './i18n'

const session: Session = {
  identity: { id: 'owner-id', email: 'owner@example.test', displayName: 'Account Owner', locale: 'en' },
  sessionId: 'session-id', authenticatedAt: '2026-08-24T10:00:00Z',
  lastSeenAt: '2026-08-24T10:00:00Z', expiresAt: '2026-08-24T22:00:00Z',
  authenticationLevel: 'password',
}

const account: AccountWorkspace = {
  id: 'account-id', name: 'Example account', slug: 'example-account', status: 'active', membershipRole: 'owner',
  packageId: 'package-id', packageName: 'Starter', packageRevision: 1, hostReady: true,
  effectiveLimits: {
    maxDomains: 5, maxDatabases: 0, maxDatabaseUsers: 0, maxScheduledJobs: 0, maxOciApplications: 0,
    allowedPhpVersions: ['8.4'], features: {
      ociApplications: false, customRedirects: true, wafExceptions: false, scheduledBackups: false,
    },
  },
  usage: { domains: 1 }, createdAt: '2026-08-24T10:00:00Z', updatedAt: '2026-08-24T10:00:00Z',
}

const domain: Domain = {
  id: 'domain-id', name: { display: 'example.test', ascii: 'example.test' }, status: 'active',
  canonicalMode: 'prefer_apex',
  target: { id: 'target-id', type: 'static', documentRoot: { id: 'root-id', relativePath: 'public_html', referenceCount: 1 } },
  tls: { enabled: true, issuanceStatus: 'active', expiresAt: '2026-11-22T10:00:00Z' },
  createdAt: '2026-08-24T10:00:00Z', updatedAt: '2026-08-24T10:00:00Z',
}

const phpStatus: AccountPHPStatus = {
  runtimeCapability: { status: 'available' },
  hostApprovedVersions: ['8.4'], packageAllowedVersions: ['8.4'], availableVersions: ['8.4'],
  pools: [{
    version: '8.4', state: 'active', configuredDomains: 1,
    memoryBytes: 67_108_864, cpuTimeNanoseconds: 2_500_000_000, processes: 3,
  }],
}

const sessions: ManagedSession[] = [{
  id: 'session-id', current: true, createdAt: '2026-08-24T10:00:00Z',
  authenticatedAt: '2026-08-24T10:00:00Z', lastSeenAt: '2026-08-24T10:00:00Z',
  expiresAt: '2026-08-24T22:00:00Z', sourceAddress: '192.0.2.1', userAgent: 'Test browser',
  authenticationLevel: 'password',
}, {
  id: 'other-session-id', current: false, createdAt: '2026-08-24T09:00:00Z',
  authenticatedAt: '2026-08-24T09:00:00Z', lastSeenAt: '2026-08-24T09:30:00Z',
  expiresAt: '2026-08-24T21:00:00Z', sourceAddress: '192.0.2.2', userAgent: 'Other browser',
  authenticationLevel: 'totp',
}]

function mountContent(page: 'domains' | 'files' | 'backups' | 'logs' | 'jobs' | 'databases' | 'profile' | 'sessions', operation: Operation | null = null) {
  return mount(AccountContent, {
    attachTo: document.body,
    props: {
      page, session, accounts: [account], selectedAccountId: account.id, domains: [domain], phpStatus, operation,
      databaseWorkspace: { databases: [], users: [], grants: [] }, databaseCredential: null,
      fileListing: { path: '', entries: [], omittedEntries: 0 },
      certificateHistory: {}, certificateHistoryLoadingDomainId: '', sessions,
      health: 'healthy', loading: false, actionBusy: false, errorCode: '', noticeCode: '',
    },
    global: { plugins: [createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages })] },
  })
}

afterEach(() => {
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})

describe('account-owner content', () => {
  it('creates a closed UTC scheduled-job definition without a command field', async () => {
    const job: ScheduledJob = {
      id: '019d413d-98f0-7abc-8def-0123456789ab', accountId: account.id, name: 'Refresh cache',
      runtime: 'shell', scriptPath: 'jobs/refresh.sh',
      schedule: { kind: 'hourly', intervalMinutes: 0, hourUtc: 0, minuteUtc: 12 },
      enabled: true, status: 'active', revision: 1, appliedRevision: 1,
      createdAt: '2026-08-30T10:00:00Z', updatedAt: '2026-08-30T10:00:00Z',
    }
    vi.spyOn(api, 'scheduledJobs').mockResolvedValue([job])
    const create = vi.spyOn(api, 'createScheduledJob').mockResolvedValue({
      operationId: '019d413d-98f0-7abc-8def-0123456789ac', status: 'pending',
      job: { ...job, id: '019d413d-98f0-7abc-8def-0123456789ad', status: 'pending', appliedRevision: undefined },
    })
    const wrapper = mountContent('jobs')
    await wrapper.setProps({ accounts: [{
      ...account, effectiveLimits: { ...account.effectiveLimits, maxScheduledJobs: 3 },
    }] })
    await flushPromises()
    expect(wrapper.text()).toContain('Choose an account-relative .sh or .php file')
    expect(wrapper.text()).toContain('Refresh cache')
    expect(wrapper.find('input[name="command"]').exists()).toBe(false)
    const form = wrapper.get('form.management-form')
    const inputs = form.findAll('input')
    await inputs[0]?.setValue('Warm pages')
    await inputs[1]?.setValue('jobs/warm.sh')
    await form.findAll('input[type="number"]')[0]?.setValue('17')
    await form.trigger('submit')
    await flushPromises()
    expect(create).toHaveBeenCalledWith(account.id, {
      expectedRevision: undefined, name: 'Warm pages', runtime: 'shell', scriptPath: 'jobs/warm.sh',
      phpVersion: undefined, schedule: { kind: 'hourly', intervalMinutes: 0, hourUtc: 0, minuteUtc: 17 },
      enabled: true,
    })
    const results = await axe.run(wrapper.element, { rules: { 'color-contrast': { enabled: false } } })
    expect(results.violations.map((violation) => violation.id)).toEqual([])
    wrapper.unmount()
  })

  it('shows only the bounded redacted domain-log response', async () => {
    const logs = vi.spyOn(api, 'hostingLogs').mockResolvedValue({
      domain: domain.name, kind: 'access', next: '42:128', retentionDays: 7,
      maximumActiveBytes: 8 * 1024 * 1024, sensitiveRedaction: true, queryStringsStored: false,
      records: [{
        timestamp: '2026-08-29T10:00:00Z', level: 'info', clientAddress: '192.0.2.10',
        host: 'example.test', method: 'GET', path: '/index.html', status: 200, bytes: 42, durationMs: 5,
      }],
    })
    const wafEvents = vi.spyOn(api, 'wafEvents').mockResolvedValue({
      domain: domain.name, next: '43:256', retentionDays: 7, maximumActiveBytes: 8 * 1024 * 1024,
      nativeDataWithheld: true, queryStringsStored: false,
      events: [{
        id: '0123456789abcdef0123456789abcdef', timestamp: '2026-08-31T10:00:00Z', ruleId: 942100,
        category: 'sql_injection', severity: 'critical', outcome: 'blocked', method: 'GET', path: '/search',
        correlationId: 'abcdef0123456789',
      }],
    })
    const wrapper = mountContent('logs')
    await flushPromises()
    expect(logs).toHaveBeenCalledWith(account.id, domain.id, 'access', '')
    expect(wafEvents).toHaveBeenCalledWith(account.id, domain.id, '')
    expect(wrapper.text()).toContain('Privacy by design')
    expect(wrapper.text()).toContain('GET /index.html')
    expect(wrapper.text()).not.toContain('token=')
    expect(wrapper.text()).toContain('Load older records')
    expect(wrapper.text()).toContain('Native diagnostics withheld')
    expect(wrapper.text()).toContain('942100')
    expect(wrapper.text()).toContain('GET /search')
    expect(wrapper.text()).toContain('SQL injection')
    const results = await axe.run(wrapper.element, { rules: { 'color-contrast': { enabled: false } } })
    expect(results.violations.map((violation) => violation.id)).toEqual([])
    wrapper.unmount()
  })

  it('creates and restores an authenticated local file backup with exact confirmation', async () => {
    const backup = {
      schemaVersion: 1, backupId: '019d413d-98f0-7abc-8def-0123456789ab', accountId: account.id,
      scope: 'account_files' as const, createdAt: '2026-08-29T10:00:00Z', payloadBytes: 512,
      contentBytes: 1024, entryCount: 2, payloadSha256: 'a'.repeat(64), manifestSha256: 'b'.repeat(64),
      manifestAuthenticated: true, payloadVerified: false,
    }
    vi.spyOn(api, 'backups').mockResolvedValue({ backups: [backup] })
    const create = vi.spyOn(api, 'createBackup').mockResolvedValue({ backup: { ...backup, payloadVerified: true }, completed: true })
    const restore = vi.spyOn(api, 'restoreBackup').mockResolvedValue({ backup: { ...backup, payloadVerified: true }, completed: true })
    vi.spyOn(window, 'prompt').mockReturnValue(backup.backupId)
    const wrapper = mountContent('backups')
    await flushPromises()
    expect(wrapper.text()).toContain('These backups exclude databases')
    expect(wrapper.text()).toContain(backup.backupId)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(create).toHaveBeenCalledWith(account.id, { scope: 'account_files', sourcePath: undefined })
    const restoreButton = wrapper.findAll('button').find((item) => item.text() === 'Restore')
    await restoreButton?.trigger('click')
    await flushPromises()
    expect(restore).toHaveBeenCalledWith(account.id, backup.backupId, backup.backupId)
    const results = await axe.run(wrapper.element, { rules: { 'color-contrast': { enabled: false } } })
    expect(results.violations.map((violation) => violation.id)).toEqual([])
    wrapper.unmount()
  })

  it('browses only directory entries from the bounded file listing', async () => {
    const wrapper = mountContent('files')
    await wrapper.setProps({
      fileListing: {
        path: 'public_html', next: '42', omittedEntries: 1,
        entries: [
          { name: 'assets', type: 'directory', sizeBytes: 4096, mode: 0o750, modifiedAt: '2026-08-26T10:00:00Z', hidden: false },
          { name: 'index.html', type: 'file', sizeBytes: 1024, mode: 0o640, modifiedAt: '2026-08-26T10:00:00Z', hidden: false },
        ],
      },
    })
    expect(wrapper.get('.file-current-path').text()).toBe('/public_html')
    expect(wrapper.get('.file-table').text()).toContain('1\u202fKiB')
    const fileActions = wrapper.findAll('.file-name-action')
    expect(fileActions[1]?.attributes('href')).toBe(
      '/api/v1/accounts/account-id/files/download?path=public_html%2Findex.html',
    )
    expect(fileActions[1]?.attributes('download')).toBe('index.html')
    await fileActions[0]?.trigger('click')
    expect(wrapper.emitted('loadFiles')?.[0]).toEqual([{ accountId: 'account-id', path: 'public_html/assets' }])
    const more = wrapper.findAll('button').find((button) => button.text() === 'Load more')
    await more?.trigger('click')
    expect(wrapper.emitted('loadFiles')?.[1]).toEqual([{
      accountId: 'account-id', path: 'public_html', cursor: '42',
    }])
    const results = await axe.run(wrapper.element, { rules: { 'color-contrast': { enabled: false } } })
    expect(results.violations.map((violation) => violation.id)).toEqual([])
    wrapper.unmount()
  })

	it('creates a file in the current descriptor-scoped directory', async () => {
		const create = vi.spyOn(api, 'createFileNode').mockResolvedValue({
			uploadId: '', directory: 'public_html', name: 'empty.txt', sizeBytes: 0,
			receivedBytes: 0, createdAt: '', completed: true,
		})
		const wrapper = mountContent('files')
		await wrapper.setProps({ fileListing: { path: 'public_html', entries: [], omittedEntries: 0 } })
		await wrapper.get('#file-node-name').setValue('empty.txt')
		const button = wrapper.findAll('button').find((item) => item.text() === 'Create file')
		await button?.trigger('click')
		await flushPromises()
		expect(create).toHaveBeenCalledWith('account-id', {
			directory: 'public_html', name: 'empty.txt', type: 'file',
		})
		expect(wrapper.emitted('loadFiles')?.[0]).toEqual([{ accountId: 'account-id', path: 'public_html' }])
		wrapper.unmount()
	})

	it('copies a regular file through the bounded no-replace operation', async () => {
		const copy = vi.spyOn(api, 'mutateFileNode').mockResolvedValue({ operationId: 'operation-id', completed: true })
		vi.spyOn(window, 'prompt').mockReturnValueOnce('public_html/assets').mockReturnValueOnce('index-copy.html')
		const wrapper = mountContent('files')
		await wrapper.setProps({ fileListing: { path: 'public_html', omittedEntries: 0, entries: [{
			name: 'index.html', type: 'file', sizeBytes: 12, mode: 0o640,
			modifiedAt: '2026-08-29T10:00:00Z', hidden: false,
		}] } })
		const button = wrapper.findAll('button').find((item) => item.text() === 'Copy')
		await button?.trigger('click')
		await flushPromises()
		expect(copy).toHaveBeenCalledWith('account-id', {
			action: 'copy', sourceDirectory: 'public_html', sourceName: 'index.html',
			destinationDirectory: 'public_html/assets', destinationName: 'index-copy.html',
		})
		expect(wrapper.emitted('loadFiles')?.[0]).toEqual([{ accountId: 'account-id', path: 'public_html' }])
		wrapper.unmount()
	})

	it('creates and extracts supported archives through hidden bounded operations', async () => {
		const archive = vi.spyOn(api, 'mutateFileArchive').mockResolvedValue({
			operationId: 'operation-id', archiveFormat: 'zip', entryCount: 2, sizeBytes: 128, completed: true,
		})
		vi.spyOn(window, 'prompt').mockReturnValueOnce('zip').mockReturnValueOnce('assets.zip')
			.mockReturnValueOnce('restored-assets')
		const wrapper = mountContent('files')
		await wrapper.setProps({ fileListing: { path: 'public_html', omittedEntries: 0, entries: [
			{ name: 'assets', type: 'directory', sizeBytes: 0, mode: 0o750, modifiedAt: '2026-08-29T10:00:00Z', hidden: false },
			{ name: 'assets.zip', type: 'file', sizeBytes: 128, mode: 0o640, modifiedAt: '2026-08-29T10:01:00Z', hidden: false },
		] } })
		const pack = wrapper.findAll('button').find((item) => item.text() === 'Archive')
		await pack?.trigger('click')
		await flushPromises()
		expect(archive).toHaveBeenNthCalledWith(1, 'account-id', {
			action: 'create', format: 'zip', sourceDirectory: 'public_html', sourceName: 'assets',
			destinationDirectory: 'public_html', destinationName: 'assets.zip',
		})
		const extract = wrapper.findAll('button').find((item) => item.text() === 'Extract')
		await extract?.trigger('click')
		await flushPromises()
		expect(archive).toHaveBeenNthCalledWith(2, 'account-id', {
			action: 'extract', format: 'zip', sourceDirectory: 'public_html', sourceName: 'assets.zip',
			destinationDirectory: 'public_html', destinationName: 'restored-assets',
		})
		wrapper.unmount()
	})

	it('loads recoverable trash and restores an entry without replacement controls in the browser', async () => {
		vi.spyOn(api, 'fileTrash').mockResolvedValue({ trashEntries: [{
			trashId: 'trash-id', directory: 'public_html', name: 'old.txt', type: 'file', sizeBytes: 4,
			trashedAt: '2026-08-29T10:00:00Z',
		}] })
		const restore = vi.spyOn(api, 'restoreTrash').mockResolvedValue({ trashId: 'trash-id', completed: true })
		const wrapper = mountContent('files')
		const show = wrapper.findAll('button').find((item) => item.text() === 'Show trash')
		await show?.trigger('click')
		await flushPromises()
		expect(wrapper.get('.file-trash').text()).toContain('/public_html/old.txt')
		const restoreButton = wrapper.findAll('button').find((item) => item.text() === 'Restore')
		await restoreButton?.trigger('click')
		await flushPromises()
		expect(restore).toHaveBeenCalledWith('account-id', 'trash-id')
		wrapper.unmount()
	})

  it('renders an accessible domain workspace and emits a bounded edit payload', async () => {
    const wrapper = mountContent('domains')
    const results = await axe.run(wrapper.element, { rules: { 'color-contrast': { enabled: false } } })
    expect(results.violations.map((violation) => violation.id)).toEqual([])

    const editButton = wrapper.findAll('button').find((button) => button.text().trim() === 'Edit')
    await editButton?.trigger('click')
    const edit = wrapper.get('form.inline-edit')
    const selects = edit.findAll('select')
    await selects[0]?.setValue('serve_both')
    await selects[2]?.setValue('custom')
    await edit.get('input').setValue('sites/example')
    await edit.trigger('submit')

    expect(wrapper.emitted('updateDomain')?.[0]).toEqual([{
      accountId: 'account-id', domainId: 'domain-id', canonicalMode: 'serve_both',
      target: { type: 'static', rootMode: 'custom', documentRoot: 'sites/example' },
      wafMode: 'off', cachePreset: 'disabled',
    }])
    wrapper.unmount()
  })

  it('shows bounded PHP pool health and emits an allowed PHP target', async () => {
    const wrapper = mountContent('domains')
    const panel = wrapper.get('.php-runtime-panel')
    expect(panel.text()).toContain('PHP-FPM pools')
    expect(panel.text()).toContain('64\u202fMiB')
    expect(panel.text()).toContain('3')
    expect(panel.text()).toContain('3 sec')

    const form = wrapper.get('form.management-form')
    await form.get('input[inputmode="url"]').setValue('php.example.test')
    await form.findAll('select')[1]?.setValue('php')
    await form.findAll('select')[2]?.setValue('8.4')
    await form.trigger('submit')

    expect(wrapper.emitted('createDomain')?.[0]).toEqual([{
      accountId: 'account-id', name: 'php.example.test', canonicalMode: 'serve_both',
      target: { type: 'php', rootMode: 'default', phpVersion: '8.4' },
		disableTls: false, tlsMode: 'acme', wafMode: 'off', cachePreset: 'disabled',
    }])
    const results = await axe.run(wrapper.element, { rules: { 'color-contrast': { enabled: false } } })
    expect(results.violations.map((violation) => violation.id)).toEqual([])
    wrapper.unmount()
  })

  it('keeps domain mutations disabled while the host boundary is provisioning', async () => {
    const wrapper = mountContent('domains')
    await wrapper.setProps({ accounts: [{ ...account, hostReady: false }] })

    expect(wrapper.text()).toContain('The host is still provisioning this account.')
    expect(wrapper.get<HTMLButtonElement>('form.management-form button[type="submit"]').element.disabled).toBe(true)
    expect(wrapper.findAll('button').some((button) => button.text().trim() === 'Edit')).toBe(false)
    await wrapper.get('form.management-form').trigger('submit')
    expect(wrapper.emitted('createDomain')).toBeUndefined()
    wrapper.unmount()
  })

  it('announces the bounded progress of the active account operation', async () => {
    const wrapper = mountContent('domains', {
      id: 'operation-id', accountId: account.id, kind: 'tls.certificate.lifecycle', status: 'running',
      stage: 'authorizing', progressPercent: 35, attemptCount: 1, maxAttempts: 3,
      createdAt: '2026-08-24T10:00:00Z', updatedAt: '2026-08-24T10:00:01Z',
    })

    const status = wrapper.get('.account-operation')
    expect(status.attributes('role')).toBe('status')
    expect(status.text()).toContain('Certificate issuance')
    expect(status.text()).toContain('Running')
    expect(status.text()).toContain('35%')
    expect(status.get<HTMLProgressElement>('progress').element.value).toBe(35)
    const results = await axe.run(wrapper.element, { rules: { 'color-contrast': { enabled: false } } })
    expect(results.violations.map((violation) => violation.id)).toEqual([])
    wrapper.unmount()
  })

  it('loads and presents non-secret active and retired certificate history', async () => {
    const wrapper = mountContent('domains')
    const historyButton = wrapper.findAll('button').find((button) => button.text().trim() === 'Show certificate history')
    await historyButton?.trigger('click')
    expect(wrapper.emitted('loadCertificateHistory')?.[0]).toEqual([{
      accountId: account.id, domainId: domain.id,
    }])

    await wrapper.setProps({
      certificateHistory: {
        [domain.id]: [{
          id: 'certificate-active', status: 'active', names: ['example.test', 'www.example.test'],
          issuer: 'Stackfort Test CA', fingerprintSha256: 'ab'.repeat(32),
          notBefore: '2026-08-24T10:00:00Z', expiresAt: '2026-11-22T10:00:00Z',
          nextRenewalAt: '2026-10-22T10:00:00Z', createdAt: '2026-08-24T09:59:00Z',
          activatedAt: '2026-08-24T10:00:00Z',
        }, {
          id: 'certificate-retired', status: 'retired', names: ['example.test', 'www.example.test'],
          issuer: 'Stackfort Test CA', createdAt: '2026-05-24T10:00:00Z',
          retiredAt: '2026-08-24T10:00:00Z',
        }],
      },
    })

    const history = wrapper.get('.certificate-history')
    expect(history.attributes('aria-label')).toBe('Certificate history for example.test')
    expect(history.text()).toContain('Stackfort Test CA')
    expect(history.text()).toContain('www.example.test')
    expect(history.text()).toContain('Active')
    expect(history.text()).toContain('Retired')
    expect(history.text()).toContain('abababab')
    const results = await axe.run(wrapper.element, { rules: { 'color-contrast': { enabled: false } } })
    expect(results.violations.map((violation) => violation.id)).toEqual([])
    wrapper.unmount()
  })

  it('emits self-profile and identity-scoped session actions', async () => {
    const wrapper = mountContent('profile')
    const inputs = wrapper.findAll<HTMLInputElement>('.profile-form input')
    await inputs[0]?.setValue('Updated Owner')
    await inputs[1]?.setValue('updated@example.test')
    await wrapper.get<HTMLSelectElement>('.profile-form select').setValue('de')
    await wrapper.get('form.profile-form').trigger('submit')
    expect(wrapper.emitted('updateProfile')?.[0]).toEqual([{
      displayName: 'Updated Owner', email: 'updated@example.test', locale: 'de',
    }])

    await wrapper.setProps({ page: 'sessions' })
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const revoke = wrapper.findAll('button').find((button) => button.text().trim() === 'Revoke session')
    await revoke?.trigger('click')
    expect(wrapper.emitted('revokeSession')?.[0]).toEqual(['other-session-id'])
    wrapper.unmount()
  })

  it('offers phpMyAdmin only for an active granted database user', async () => {
    const wrapper = mountContent('databases')
    await wrapper.setProps({
      accounts: [{
        ...account,
        effectiveLimits: { ...account.effectiveLimits, maxDatabases: 2, maxDatabaseUsers: 2 },
      }],
      databaseWorkspace: {
        databases: [],
        users: [{
          id: 'database-user-id', alias: 'application', host: 'localhost', status: 'active', revealed: true,
          createdAt: '2026-08-26T10:00:00Z', updatedAt: '2026-08-26T10:00:00Z',
        }],
        grants: [{
          id: 'grant-id', databaseId: 'database-id', databaseUserId: 'database-user-id',
          preset: 'read_write', status: 'active',
        }],
      },
    })

    const launch = wrapper.findAll('button').find((button) => button.text().trim() === 'Open phpMyAdmin')
    expect(launch?.exists()).toBe(true)
    await launch?.trigger('click')
    expect(wrapper.emitted('launchPhpMyAdmin')?.[0]).toEqual([{
      accountId: 'account-id', userId: 'database-user-id',
    }])
    expect(wrapper.text()).toContain('30-second, single-use handoff')

	vi.spyOn(window, 'confirm').mockReturnValue(true)
	const rotate = wrapper.findAll('button').find((button) => button.text().trim() === 'Rotate password')
	expect(rotate?.exists()).toBe(true)
	await rotate?.trigger('click')
	expect(wrapper.emitted('rotateDatabaseCredential')?.[0]).toEqual([{
	  accountId: 'account-id', userId: 'database-user-id',
	}])
    wrapper.unmount()
  })
})
