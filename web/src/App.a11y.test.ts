// SPDX-License-Identifier: AGPL-3.0-or-later
// @vitest-environment jsdom

import axe from 'axe-core'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { nextTick } from 'vue'
import App from './App.vue'
import { messages } from './i18n'

let wrapper: VueWrapper | null = null

function installMatchMedia(matches: boolean) {
  vi.stubGlobal('matchMedia', vi.fn().mockImplementation((query: string) => ({
    matches,
    media: query,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  } satisfies MediaQueryList)))
}

function installHealthyAPI() {
  vi.stubGlobal('fetch', vi.fn().mockImplementation((input: string | URL | Request) => {
    const url = String(input)
    let body: unknown = { status: 'ok' }
    if (url.endsWith('/bootstrap')) body = { required: false, capabilityActive: false }
    else if (url.endsWith('/session')) body = {
      identity: { id: 'admin-id', email: 'admin@example.test', displayName: 'Test Admin', locale: 'en' },
      sessionId: 'session-id', authenticatedAt: '2026-08-24T10:00:00Z',
      lastSeenAt: '2026-08-24T10:00:00Z', expiresAt: '2026-08-25T10:00:00Z',
      authenticationLevel: 'password',
    }
    else if (url.endsWith('/me')) body = { platformAdministrator: true, accounts: [] }
    else if (url.endsWith('/build')) body = { version: 'dev', commit: 'test-commit', buildDate: 'unknown' }
    else if (url.endsWith('/admin/packages')) body = { packages: [] }
    else if (url.endsWith('/admin/accounts')) body = { accounts: [] }
    else if (url.includes('/admin/operations')) body = { operations: [] }
    else if (url.includes('/admin/audit-events')) body = { events: [] }
    else if (url.endsWith('/admin/acme/accounts')) body = { accounts: [] }
    else if (url.endsWith('/admin/host/capabilities')) body = {
      inspectedAt: '2026-08-24T10:00:00Z',
      managedPhpVersions: ['8.4'],
      platform: { distributionId: 'debian', versionId: '13', architecture: 'amd64', kernelRelease: '6.12', support: { status: 'available' } },
      services: [],
    }
    else if (url.endsWith('/admin/updates')) body = {
      currentVersion: '1.0.0', currentVersionValid: true, channel: 'stable', automaticChecks: true,
      automaticFunctionalUpdates: false, checkIntervalSeconds: 21600, updateAvailable: false,
    }
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(body) } as Response)
  }))
}

function mountApplication() {
  const i18n = createI18n({
    legacy: false,
    locale: 'en',
    fallbackLocale: 'en',
    messages,
  })
  wrapper = mount(App, {
    attachTo: document.body,
    global: { plugins: [i18n] },
  })
  return wrapper
}

beforeEach(() => {
  window.localStorage.clear()
  installHealthyAPI()
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  document.body.innerHTML = ''
  document.body.className = ''
  document.documentElement.lang = 'en'
  vi.unstubAllGlobals()
})

describe('accessible application shell', () => {
  it('has no selected automated accessibility violations on desktop', async () => {
    installMatchMedia(false)
    const application = mountApplication()
    await flushPromises()
    await nextTick()

    const results = await axe.run(application.element, {
      rules: {
        'color-contrast': { enabled: false },
      },
    })
    expect(results.violations.map((violation) => violation.id)).toEqual([])
    expect(application.find('a.skip-link').attributes('href')).toBe('#main-content')
    expect(application.findAll('[aria-current="page"]')).toHaveLength(1)
    expect(application.find('main').attributes('id')).toBe('main-content')
    await application.find('a.skip-link').trigger('click')
    expect(application.find('main').element).toBe(document.activeElement)
  })

  it('traps mobile navigation focus, closes with Escape, and restores focus', async () => {
    installMatchMedia(true)
    const application = mountApplication()
    await flushPromises()
    await nextTick()
    const menu = application.find<HTMLButtonElement>('.mobile-menu')

    await menu.trigger('click')
    await nextTick()
    expect(application.find('.sidebar').attributes('aria-hidden')).toBeUndefined()
    expect(document.activeElement).toBe(application.find('.sidebar-close').element)

    const lastControl = application.find<HTMLButtonElement>('.identity-summary')
    lastControl.element.focus()
    await application.find('.sidebar').trigger('keydown', { key: 'Tab' })
    expect(document.activeElement).toBe(application.find('.sidebar-close').element)

    await application.find('.sidebar').trigger('keydown', { key: 'Escape' })
    await nextTick()
    expect(application.find('.sidebar').attributes('aria-hidden')).toBe('true')
    expect(document.activeElement).toBe(menu.element)
  })

  it('moves focus to changed page content and persists the selected language', async () => {
    installMatchMedia(false)
    const application = mountApplication()
    await flushPromises()
    await nextTick()
    const domains = application.findAll<HTMLButtonElement>('.nav-item')
      .find((item) => item.text().trim() === 'Domains')
    expect(domains).toBeDefined()

    await domains?.trigger('click')
    await nextTick()
    expect(application.find('h1').text()).toBe('Domains')
    expect(application.find('h1').element).toBe(document.activeElement)
    expect(domains?.attributes('aria-current')).toBe('page')

    await application.find<HTMLSelectElement>('#language').setValue('de')
    await nextTick()
    expect(document.documentElement.lang).toBe('de')
    expect(window.localStorage.getItem('stackfort.locale')).toBe('de')
    expect(application.find('h1').text()).toBe('Domains')
    expect(application.find('.context-label').text()).toBe('Plattformverwaltung')
  })
})
