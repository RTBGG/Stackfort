// SPDX-License-Identifier: AGPL-3.0-or-later
// @vitest-environment jsdom

import axe from 'axe-core'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import AdminContent from './AdminContent.vue'
import type { AccountPHPStatus, HostCapabilities, HostingAccount, HostingPackage, Session } from './api'
import { messages } from './i18n'

const session: Session = {
  identity: { id: 'admin-id', email: 'admin@example.test', displayName: 'Test Admin', locale: 'en' },
  sessionId: 'session-id', authenticatedAt: '2026-08-25T10:00:00Z',
  lastSeenAt: '2026-08-25T10:00:00Z', expiresAt: '2026-08-25T22:00:00Z',
  authenticationLevel: 'password',
}

const capabilities: HostCapabilities = {
  inspectedAt: '2026-08-25T10:00:00Z', managedPhpVersions: ['8.4'],
  platform: {
    distributionId: 'debian', versionId: '13', architecture: 'amd64', kernelRelease: '6.12',
    support: { status: 'available' },
  },
  services: [],
}

const hostingPackage: HostingPackage = {
  id: 'package-id', name: 'PHP Starter', slug: 'php-starter', status: 'active', currentRevision: 1,
  limits: {
    maxDomains: 5, maxDatabases: 0, maxDatabaseUsers: 0, maxScheduledJobs: 0, maxOciApplications: 0,
    allowedPhpVersions: ['8.4'], features: {
      ociApplications: false, customRedirects: true, wafExceptions: false, scheduledBackups: false,
    },
  },
  createdAt: '2026-08-25T10:00:00Z', updatedAt: '2026-08-25T10:00:00Z',
}

const hostingAccount: HostingAccount = {
  id: 'account-id', name: 'PHP Account', slug: 'php-account', status: 'active',
  currentPackageAssignmentId: 'assignment-id', packageId: hostingPackage.id,
  packageName: hostingPackage.name, packageRevision: 1, hostReady: true,
  createdAt: '2026-08-25T10:00:00Z', updatedAt: '2026-08-25T10:00:00Z',
}

const phpStatus: AccountPHPStatus = {
  runtimeCapability: { status: 'available' }, hostApprovedVersions: ['8.4'],
  packageAllowedVersions: ['8.4'], availableVersions: ['8.4'],
  pools: [{ version: '8.4', state: 'missing', configuredDomains: 0 }],
}

function mountContent(
  page: 'settings' | 'packages' | 'domains',
  packages: HostingPackage[] = [],
  accounts: HostingAccount[] = [],
) {
  return mount(AdminContent, {
    attachTo: document.body,
    props: {
      page, session, health: 'healthy', build: null, packages, accounts,
      domains: [], wafExceptions: [], operations: [], auditEvents: [], capabilities, phpStatus, acmeAccounts: [],
      loading: false, actionBusy: false, errorCode: '', noticeCode: '',
    },
    global: { plugins: [createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages })] },
  })
}

afterEach(() => { document.body.innerHTML = '' })

describe('administrator certificate authority settings', () => {
  it('is accessible and emits the fixed production registration intent', async () => {
    const wrapper = mountContent('settings')
    const results = await axe.run(wrapper.element, { rules: { 'color-contrast': { enabled: false } } })
    expect(results.violations.map((violation) => violation.id)).toEqual([])

    await wrapper.get<HTMLInputElement>('input[type="email"]').setValue('tls@example.test')
    await wrapper.get<HTMLInputElement>('input[type="checkbox"]').setValue(true)
    await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('registerACMEAccount')?.[0]).toEqual([{
      environment: 'letsencrypt-production', contactEmail: 'tls@example.test', termsAccepted: true,
    }])
    wrapper.unmount()
  })
})

describe('administrator PHP controls', () => {
  it('only exposes host-approved versions when creating a package', async () => {
    const wrapper = mountContent('packages')
    const form = wrapper.get('form.management-form')
    await form.findAll('input')[0]?.setValue('PHP Plan')
    await form.findAll('input')[1]?.setValue('php-plan')
    await form.get<HTMLInputElement>('input[type="checkbox"]').setValue(true)
    await form.trigger('submit')

    const limits = (wrapper.emitted('createPackage')?.[0]?.[0] as { limits: { allowedPhpVersions: string[] } }).limits
    expect(limits.allowedPhpVersions).toEqual(['8.4'])
    expect(wrapper.text()).not.toContain('PHP 8.3')
    const results = await axe.run(wrapper.element, { rules: { 'color-contrast': { enabled: false } } })
    expect(results.violations.map((violation) => violation.id)).toEqual([])
    wrapper.unmount()
  })

  it('emits a PHP domain only from the selected package and host intersection', async () => {
    const wrapper = mountContent('domains', [hostingPackage], [hostingAccount])
    const form = wrapper.get('form.management-form')
    await form.get('input[inputmode="url"]').setValue('app.example.test')
    await form.findAll('select')[1]?.setValue('php')
    await form.findAll('select')[2]?.setValue('8.4')
    await form.trigger('submit')

    expect(wrapper.emitted('createDomain')?.[0]).toEqual([{
      accountId: 'account-id', name: 'app.example.test', canonicalMode: 'serve_both',
      target: { type: 'php', rootMode: 'default', phpVersion: '8.4' },
		disableTls: false, tlsMode: 'acme', wafMode: 'off', cachePreset: 'disabled',
    }])
    wrapper.unmount()
  })
})
