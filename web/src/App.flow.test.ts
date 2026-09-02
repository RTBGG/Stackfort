// SPDX-License-Identifier: AGPL-3.0-or-later
// @vitest-environment jsdom

import axe from 'axe-core'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import App from './App.vue'
import { messages } from './i18n'

let wrapper: VueWrapper | null = null

function response(status: number, body: unknown) {
  return Promise.resolve({
    ok: status >= 200 && status < 300,
    status,
    json: () => Promise.resolve(body),
  } as Response)
}

function sessionResponse() {
  return {
    identity: { id: 'admin-id', email: 'admin@example.test', displayName: 'Test Admin', locale: 'en' },
    sessionId: 'session-id', authenticatedAt: '2026-08-24T10:00:00Z',
    lastSeenAt: '2026-08-24T10:00:00Z', expiresAt: '2026-08-25T10:00:00Z',
    authenticationLevel: 'mfa',
  }
}

function phpStatusResponse() {
  return response(200, {
    runtimeCapability: { status: 'available' }, hostApprovedVersions: ['8.4'],
    packageAllowedVersions: [], availableVersions: [],
    pools: [{ version: '8.4', state: 'missing', configuredDomains: 0 }],
  })
}

function authenticatedResource(input: string) {
  if (input.endsWith('/me')) return response(200, { platformAdministrator: true, accounts: [] })
  if (input.endsWith('/admin/packages')) return response(200, { packages: [] })
  if (input.endsWith('/admin/accounts')) return response(200, { accounts: [] })
  if (input.includes('/admin/operations')) return response(200, { operations: [] })
  if (input.includes('/admin/audit-events')) return response(200, { events: [] })
  if (input.endsWith('/admin/acme/accounts')) return response(200, { accounts: [] })
  if (input.endsWith('/admin/host/capabilities')) return response(503, { code: 'host_agent_unavailable' })
  if (input.endsWith('/admin/updates')) return response(200, {
    currentVersion: '1.0.0', currentVersionValid: true, channel: 'stable', automaticChecks: true,
    automaticFunctionalUpdates: false, checkIntervalSeconds: 21600, updateAvailable: false,
  })
  if (input.endsWith('/health')) return response(200, { status: 'ok' })
  if (input.endsWith('/build')) return response(200, { version: 'dev', commit: 'test', buildDate: 'unknown' })
  return null
}

function mountApplication() {
  const i18n = createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages })
  wrapper = mount(App, { attachTo: document.body, global: { plugins: [i18n] } })
  return wrapper
}

beforeEach(() => {
  window.localStorage.clear()
  vi.stubGlobal('matchMedia', vi.fn().mockReturnValue({
    matches: false, media: '', onchange: null,
    addEventListener: vi.fn(), removeEventListener: vi.fn(),
    addListener: vi.fn(), removeListener: vi.fn(), dispatchEvent: vi.fn(),
  } satisfies MediaQueryList))
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  document.body.innerHTML = ''
  document.body.className = ''
  vi.unstubAllGlobals()
})

describe('administrator entry flows', () => {
  it('validates and completes one-time administrator bootstrap', async () => {
    const fetchMock = vi.fn().mockImplementation((input: string | URL | Request, init?: RequestInit) => {
      const url = String(input)
      const resource = authenticatedResource(url)
      if (resource) return resource
      if (url.endsWith('/bootstrap') && init?.method === 'POST') return response(201, {
        id: 'admin-id', email: 'admin@example.test', displayName: 'Test Admin', locale: 'en',
      })
      if (url.endsWith('/bootstrap')) return response(200, {
        required: true, capabilityActive: true, expiresAt: '2026-08-24T12:00:00Z',
      })
      return response(404, { code: 'resource_not_found' })
    })
    vi.stubGlobal('fetch', fetchMock)
    const application = mountApplication()
    await flushPromises()

    expect(application.find('h1').text()).toBe('Create the first administrator')
    const results = await axe.run(application.element, { rules: { 'color-contrast': { enabled: false } } })
    expect(results.violations.map((violation) => violation.id)).toEqual([])

    const inputs = application.findAll<HTMLInputElement>('.auth-form input')
    await inputs[0]?.setValue('sfb_test-capability')
    await inputs[1]?.setValue('Test Admin')
    await inputs[2]?.setValue('admin@example.test')
    await inputs[3]?.setValue('long-enough-password')
    await inputs[4]?.setValue('different-password')
    await application.find('form').trigger('submit')
    expect(application.text()).toContain('The passwords do not match.')
    expect(fetchMock.mock.calls.filter((call) => call[1]?.method === 'POST')).toHaveLength(0)

    await inputs[4]?.setValue('long-enough-password')
    await application.find('form').trigger('submit')
    await flushPromises()
    expect(application.find('h1').text()).toBe('Sign in to Stackfort')
    expect(application.text()).toContain('Administrator created.')
  })

  it('continues a password login through MFA into the administrator shell', async () => {
    const fetchMock = vi.fn().mockImplementation((input: string | URL | Request, init?: RequestInit) => {
      const url = String(input)
      const resource = authenticatedResource(url)
      if (resource) return resource
      if (url.endsWith('/bootstrap')) return response(200, { required: false, capabilityActive: false })
      if (url.endsWith('/session')) return response(401, { code: 'authentication_required' })
      if (url.endsWith('/login/mfa')) return response(200, sessionResponse())
      if (url.endsWith('/login') && init?.method === 'POST') return response(202, {
        mfaRequired: true, expiresAt: '2026-08-24T10:05:00Z',
      })
      return response(404, { code: 'resource_not_found' })
    })
    vi.stubGlobal('fetch', fetchMock)
    const application = mountApplication()
    await flushPromises()

    expect(application.find('h1').text()).toBe('Sign in to Stackfort')
    await application.find<HTMLInputElement>('input[type="email"]').setValue('admin@example.test')
    await application.find<HTMLInputElement>('input[type="password"]').setValue('long-enough-password')
    await application.find('form').trigger('submit')
    await flushPromises()
    expect(application.find('h1').text()).toBe('Verify your identity')

    await application.find<HTMLInputElement>('input[autocomplete="one-time-code"]').setValue('123456')
    await application.find('form').trigger('submit')
    await flushPromises()
    expect(application.find('h1').text()).toBe('Overview')
    expect(application.find('.identity-summary').text()).toContain('Test Admin')
    expect(application.findAll('[aria-current="page"]')).toHaveLength(1)
  })

  it('routes an account owner to bounded account flows after login', async () => {
    const account = {
      id: 'account-id', name: 'Example account', slug: 'example-account', status: 'active',
      membershipRole: 'owner', packageId: 'package-id', packageName: 'Starter', packageRevision: 1, hostReady: true,
      effectiveLimits: {
        maxDomains: 5, maxDatabases: 0, maxDatabaseUsers: 0, maxScheduledJobs: 0,
        maxOciApplications: 0, memoryBytes: 2147483648, storageBytes: 21474836480,
        allowedPhpVersions: [], features: {
          ociApplications: false, customRedirects: true, wafExceptions: false, scheduledBackups: false,
        },
      },
      usage: { domains: 1 }, createdAt: '2026-08-24T10:00:00Z', updatedAt: '2026-08-24T10:00:00Z',
    }
    const fetchMock = vi.fn().mockImplementation((input: string | URL | Request) => {
      const url = String(input)
      if (url.endsWith('/bootstrap')) return response(200, { required: false, capabilityActive: false })
      if (url.endsWith('/session')) return response(200, {
        identity: { id: 'owner-id', email: 'owner@example.test', displayName: 'Test Owner', locale: 'en' },
        sessionId: 'session-id', authenticatedAt: '2026-08-24T10:00:00Z',
        lastSeenAt: '2026-08-24T10:00:00Z', expiresAt: '2026-08-25T10:00:00Z', authenticationLevel: 'password',
      })
      if (url.endsWith('/me')) return response(200, { platformAdministrator: false, accounts: [account] })
      if (url.endsWith('/sessions')) return response(200, { sessions: [] })
      if (url.endsWith('/accounts/account-id/php')) return phpStatusResponse()
      if (url.endsWith('/accounts/account-id/domains')) return response(200, { domains: [] })
      if (url.endsWith('/health')) return response(200, { status: 'ok' })
      if (url.endsWith('/build')) return response(200, { version: 'dev', commit: 'test', buildDate: 'unknown' })
      return response(404, { code: 'resource_not_found' })
    })
    vi.stubGlobal('fetch', fetchMock)
    const application = mountApplication()
    await flushPromises()

    expect(application.find('h1').text()).toBe('Overview')
    expect(application.find('.context-label').text()).toBe('Account workspace')
    expect(application.text()).toContain('Example account')
    expect(application.text()).toContain('Starter')
    expect(application.findAll('.nav-item').map((item) => item.text().trim())).toEqual([
      'Overview', 'Domains', 'Files', 'Backups', 'Logs', 'Databases', 'Scheduled jobs', 'Package usage', 'Profile', 'Sessions',
    ])
  })

  it('tracks an owner domain operation through completion and refreshes the domain list', async () => {
    vi.spyOn(document, 'cookie', 'get').mockReturnValue('__Host-sf-csrf=csrf-bound')
    const account = {
      id: 'account-id', name: 'Example account', slug: 'example-account', status: 'active',
      membershipRole: 'owner', packageId: 'package-id', packageName: 'Starter', packageRevision: 1, hostReady: true,
      effectiveLimits: {
        maxDomains: 5, maxDatabases: 0, maxDatabaseUsers: 0, maxScheduledJobs: 0, maxOciApplications: 0,
        allowedPhpVersions: [], features: {
          ociApplications: false, customRedirects: true, wafExceptions: false, scheduledBackups: false,
        },
      },
      usage: { domains: 0 }, createdAt: '2026-08-24T10:00:00Z', updatedAt: '2026-08-24T10:00:00Z',
    }
    const createdDomain = {
      id: 'domain-id', name: { display: 'example.test', ascii: 'example.test' }, status: 'active',
      canonicalMode: 'serve_both',
      target: { id: 'target-id', type: 'static', documentRoot: { id: 'root-id', relativePath: 'public_html', referenceCount: 1 } },
      tls: { enabled: true, issuanceStatus: 'issuing' },
      createdAt: '2026-08-24T10:00:00Z', updatedAt: '2026-08-24T10:00:01Z',
    }
    let operationCompleted = false
    const fetchMock = vi.fn().mockImplementation((input: string | URL | Request, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/bootstrap')) return response(200, { required: false, capabilityActive: false })
      if (url.endsWith('/session')) return response(200, {
        identity: { id: 'owner-id', email: 'owner@example.test', displayName: 'Test Owner', locale: 'en' },
        sessionId: 'session-id', authenticatedAt: '2026-08-24T10:00:00Z',
        lastSeenAt: '2026-08-24T10:00:00Z', expiresAt: '2026-08-25T10:00:00Z', authenticationLevel: 'password',
      })
      if (url.endsWith('/me')) return response(200, {
        platformAdministrator: false,
        accounts: [{ ...account, usage: { domains: operationCompleted ? 1 : 0 } }],
      })
      if (url.endsWith('/sessions')) return response(200, { sessions: [] })
      if (url.endsWith('/accounts/account-id/php')) return phpStatusResponse()
      if (url.endsWith('/accounts/account-id/domains') && init?.method === 'POST') {
        return response(202, { operationId: 'operation-id', domainId: 'operation-id', status: 'pending' })
      }
      if (url.endsWith('/accounts/account-id/operations/operation-id')) {
        operationCompleted = true
        return response(200, {
          id: 'operation-id', accountId: 'account-id', kind: 'domain.lifecycle.apply', status: 'succeeded',
          stage: 'completed', progressPercent: 100, attemptCount: 1, maxAttempts: 3,
          createdAt: '2026-08-24T10:00:00Z', updatedAt: '2026-08-24T10:00:01Z',
        })
      }
      if (url.endsWith('/accounts/account-id/domains')) {
        return response(200, { domains: operationCompleted ? [createdDomain] : [] })
      }
      if (url.endsWith('/health')) return response(200, { status: 'ok' })
      if (url.endsWith('/build')) return response(200, { version: 'dev', commit: 'test', buildDate: 'unknown' })
      return response(404, { code: 'resource_not_found' })
    })
    vi.stubGlobal('fetch', fetchMock)
    const application = mountApplication()
    await flushPromises()

    const domainsNavigation = application.findAll('.nav-item').find((item) => item.text().trim() === 'Domains')
    await domainsNavigation?.trigger('click')
    await flushPromises()
    await application.get<HTMLInputElement>('form.management-form input[inputmode="url"]').setValue('example.test')
    await application.get('form.management-form').trigger('submit')
    await flushPromises()

    expect(application.get('.account-operation').text()).toContain('Domain configuration')
    expect(application.get('.account-operation').text()).toContain('Succeeded')
    expect(application.get('.account-operation progress').attributes('value')).toBe('100')
    expect(application.text()).toContain('The operation completed successfully.')
    expect(application.text()).toContain('example.test')
    const operationRequest = fetchMock.mock.calls.find((call) => String(call[0]).includes('/operations/operation-id'))
    expect(operationRequest?.[1]?.method).toBe('GET')
  })

  it('keeps a failed operation bounded and does not render its internal error code', async () => {
    vi.spyOn(document, 'cookie', 'get').mockReturnValue('__Host-sf-csrf=csrf-bound')
    const account = {
      id: 'account-id', name: 'Example account', slug: 'example-account', status: 'active',
      membershipRole: 'owner', packageId: 'package-id', packageName: 'Starter', packageRevision: 1, hostReady: true,
      effectiveLimits: {
        maxDomains: 5, maxDatabases: 0, maxDatabaseUsers: 0, maxScheduledJobs: 0, maxOciApplications: 0,
        allowedPhpVersions: [], features: {
          ociApplications: false, customRedirects: true, wafExceptions: false, scheduledBackups: false,
        },
      },
      usage: { domains: 0 }, createdAt: '2026-08-24T10:00:00Z', updatedAt: '2026-08-24T10:00:00Z',
    }
    const fetchMock = vi.fn().mockImplementation((input: string | URL | Request, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/bootstrap')) return response(200, { required: false, capabilityActive: false })
      if (url.endsWith('/session')) return response(200, {
        identity: { id: 'owner-id', email: 'owner@example.test', displayName: 'Test Owner', locale: 'en' },
        sessionId: 'session-id', authenticatedAt: '2026-08-24T10:00:00Z',
        lastSeenAt: '2026-08-24T10:00:00Z', expiresAt: '2026-08-25T10:00:00Z', authenticationLevel: 'password',
      })
      if (url.endsWith('/me')) return response(200, { platformAdministrator: false, accounts: [account] })
      if (url.endsWith('/sessions')) return response(200, { sessions: [] })
      if (url.endsWith('/accounts/account-id/php')) return phpStatusResponse()
      if (url.endsWith('/accounts/account-id/domains') && init?.method === 'POST') {
        return response(202, { operationId: 'operation-id', domainId: 'operation-id', status: 'pending' })
      }
      if (url.endsWith('/accounts/account-id/operations/operation-id')) return response(200, {
        id: 'operation-id', accountId: 'account-id', kind: 'domain.lifecycle.apply', status: 'failed',
        stage: 'failed', progressPercent: 15, errorCode: 'internal_marker_must_not_render',
        attemptCount: 3, maxAttempts: 3,
        createdAt: '2026-08-24T10:00:00Z', updatedAt: '2026-08-24T10:00:01Z',
      })
      if (url.endsWith('/accounts/account-id/domains')) return response(200, { domains: [] })
      if (url.endsWith('/health')) return response(200, { status: 'ok' })
      if (url.endsWith('/build')) return response(200, { version: 'dev', commit: 'test', buildDate: 'unknown' })
      return response(404, { code: 'resource_not_found' })
    })
    vi.stubGlobal('fetch', fetchMock)
    const application = mountApplication()
    await flushPromises()

    const domainsNavigation = application.findAll('.nav-item').find((item) => item.text().trim() === 'Domains')
    await domainsNavigation?.trigger('click')
    await flushPromises()
    await application.get<HTMLInputElement>('form.management-form input[inputmode="url"]').setValue('failed.example.test')
    await application.get('form.management-form').trigger('submit')
    await flushPromises()

    expect(application.get('.account-operation').text()).toContain('Failed')
    expect(application.text()).toContain('The background operation failed.')
    expect(application.text()).not.toContain('internal_marker_must_not_render')
    expect(application.text()).toContain('No domains for this account')
  })

  it('refreshes an open certificate history after successful issuance', async () => {
    vi.spyOn(document, 'cookie', 'get').mockReturnValue('__Host-sf-csrf=csrf-bound')
    const account = {
      id: 'account-id', name: 'Example account', slug: 'example-account', status: 'active',
      membershipRole: 'owner', packageId: 'package-id', packageName: 'Starter', packageRevision: 1, hostReady: true,
      effectiveLimits: {
        maxDomains: 5, maxDatabases: 0, maxDatabaseUsers: 0, maxScheduledJobs: 0, maxOciApplications: 0,
        allowedPhpVersions: [], features: {
          ociApplications: false, customRedirects: true, wafExceptions: false, scheduledBackups: false,
        },
      },
      usage: { domains: 1 }, createdAt: '2026-08-24T10:00:00Z', updatedAt: '2026-08-24T10:00:00Z',
    }
    const domain = {
      id: 'domain-id', name: { display: 'example.test', ascii: 'example.test' }, status: 'active',
      canonicalMode: 'serve_both',
      target: { id: 'target-id', type: 'static', documentRoot: { id: 'root-id', relativePath: 'public_html', referenceCount: 1 } },
      tls: { enabled: true, issuanceStatus: 'failed' },
      createdAt: '2026-08-24T10:00:00Z', updatedAt: '2026-08-24T10:00:00Z',
    }
    const retiredCertificate = {
      id: 'certificate-retired', status: 'retired', names: ['example.test', 'www.example.test'],
      issuer: 'Stackfort Test CA', createdAt: '2026-05-24T10:00:00Z', retiredAt: '2026-08-24T10:00:00Z',
    }
    const activeCertificate = {
      id: 'certificate-active', status: 'active', names: ['example.test', 'www.example.test'],
      issuer: 'Stackfort Test CA', fingerprintSha256: 'ab'.repeat(32),
      notBefore: '2026-08-24T10:00:00Z', expiresAt: '2026-11-22T10:00:00Z',
      nextRenewalAt: '2026-10-22T10:00:00Z', createdAt: '2026-08-24T09:59:00Z',
      activatedAt: '2026-08-24T10:00:00Z',
    }
    let issuanceComplete = false
    let certificateRequests = 0
    const fetchMock = vi.fn().mockImplementation((input: string | URL | Request, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/bootstrap')) return response(200, { required: false, capabilityActive: false })
      if (url.endsWith('/session')) return response(200, {
        identity: { id: 'owner-id', email: 'owner@example.test', displayName: 'Test Owner', locale: 'en' },
        sessionId: 'session-id', authenticatedAt: '2026-08-24T10:00:00Z',
        lastSeenAt: '2026-08-24T10:00:00Z', expiresAt: '2026-08-25T10:00:00Z', authenticationLevel: 'password',
      })
      if (url.endsWith('/me')) return response(200, { platformAdministrator: false, accounts: [account] })
      if (url.endsWith('/sessions')) return response(200, { sessions: [] })
      if (url.endsWith('/accounts/account-id/php')) return phpStatusResponse()
      if (url.endsWith('/accounts/account-id/domains/domain-id/tls/issue') && init?.method === 'POST') {
        return response(202, { operationId: 'operation-id', domainId: 'domain-id', status: 'pending' })
      }
      if (url.endsWith('/accounts/account-id/operations/operation-id')) {
        issuanceComplete = true
        return response(200, {
          id: 'operation-id', accountId: 'account-id', kind: 'tls.certificate.lifecycle', status: 'succeeded',
          stage: 'completed', progressPercent: 100, attemptCount: 1, maxAttempts: 4,
          createdAt: '2026-08-24T10:00:00Z', updatedAt: '2026-08-24T10:00:01Z',
        })
      }
      if (url.endsWith('/accounts/account-id/domains/domain-id/tls/certificates')) {
        certificateRequests++
        return response(200, { certificates: issuanceComplete ? [activeCertificate, retiredCertificate] : [retiredCertificate] })
      }
      if (url.endsWith('/accounts/account-id/domains')) {
        return response(200, { domains: [{
          ...domain,
          tls: issuanceComplete
            ? { enabled: true, issuanceStatus: 'active', expiresAt: activeCertificate.expiresAt }
            : domain.tls,
        }] })
      }
      if (url.endsWith('/health')) return response(200, { status: 'ok' })
      if (url.endsWith('/build')) return response(200, { version: 'dev', commit: 'test', buildDate: 'unknown' })
      return response(404, { code: 'resource_not_found' })
    })
    vi.stubGlobal('fetch', fetchMock)
    const application = mountApplication()
    await flushPromises()

    const domainsNavigation = application.findAll('.nav-item').find((item) => item.text().trim() === 'Domains')
    await domainsNavigation?.trigger('click')
    await flushPromises()
    const historyButton = application.findAll('button').find((button) => button.text().trim() === 'Show certificate history')
    await historyButton?.trigger('click')
    await flushPromises()
    expect(application.get('.certificate-history').text()).toContain('Retired')
    expect(application.get('.certificate-history').text()).not.toContain('Active')

    const issueButton = application.findAll('button').find((button) => button.text().trim() === 'Retry certificate issuance')
    await issueButton?.trigger('click')
    await flushPromises()

    expect(application.get('.account-operation').text()).toContain('Certificate issuance')
    expect(application.get('.account-operation').text()).toContain('Succeeded')
    expect(application.get('.certificate-history').text()).toContain('Active')
    expect(application.get('.certificate-history').text()).toContain('Retired')
    expect(application.text()).toContain('Nov 22, 2026')
    expect(certificateRequests).toBe(2)
  })
})
