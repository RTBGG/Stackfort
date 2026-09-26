// SPDX-License-Identifier: AGPL-3.0-or-later
// @vitest-environment jsdom
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import PanelHostnameSettings from './PanelHostnameSettings.vue'
import { messages } from './i18n'

let wrapper: VueWrapper | undefined
afterEach(() => { wrapper?.unmount(); vi.useRealTimers(); vi.restoreAllMocks(); vi.unstubAllGlobals() })

it.each(['en', 'de'] as const)('queues one explicit panel request and observes completion (%s)', async (locale) => {
  vi.useFakeTimers()
  vi.spyOn(document, 'cookie', 'get').mockReturnValue('__Host-sf-csrf=fixture-csrf')
  let queued = false
  let completed = false
  const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
    let body: unknown
    if (url.endsWith('/panel/issue')) {
      queued = true; body = { operationId: 'panel-op', status: 'pending' }
    } else if (url.includes('/operations')) {
      body = { operations: queued ? [{ id: 'panel-op', kind: 'panel.hostname.issue', status: completed ? 'succeeded' : 'running' }] : [] }
    } else {
      body = completed ? { enabled: true, autoRenew: true, recoveryRequired: false, hostname: 'panel.example.com', url: 'https://panel.example.com/' }
        : { enabled: false, autoRenew: false, recoveryRequired: false }
    }
    return { ok: true, status: init?.method === 'POST' ? 202 : 200, json: async () => body } as Response
  })
  vi.stubGlobal('fetch', fetchMock)
  wrapper = mount(PanelHostnameSettings, { global: { plugins: [createI18n({ legacy: false, locale, messages })] } })
  await flushPromises()
  await wrapper.get('input[type="text"]').setValue('panel.example.com')
  await wrapper.get('input[type="email"]').setValue('admin@example.com')
  const checks = wrapper.findAll('input[type="checkbox"]')
  expect(wrapper.get<HTMLButtonElement>('button[type="submit"]').element.disabled).toBe(true)
  await checks[0]!.setValue(true)
  expect(wrapper.get<HTMLButtonElement>('button[type="submit"]').element.disabled).toBe(true)
  await checks[1]!.setValue(true)
  await wrapper.get('form').trigger('submit'); await flushPromises()
  await wrapper.get('form').trigger('submit'); await flushPromises()
  const posts = fetchMock.mock.calls.filter(([, init]) => init?.method === 'POST')
  expect(posts).toHaveLength(1)
  expect(JSON.parse(posts[0]![1]!.body as string)).toEqual({
    hostname: 'panel.example.com', email: 'admin@example.com', acceptTerms: true, confirmOriginChange: true,
  })
  expect(new Headers(posts[0]![1]!.headers).get('X-CSRF-Token')).toBe('fixture-csrf')
  expect(new Headers(posts[0]![1]!.headers).get('Idempotency-Key')).toBeTruthy()
  expect(wrapper.text()).toContain(messages[locale].panelHostname.pending)
  await vi.advanceTimersByTimeAsync(2000); await flushPromises()
  expect(wrapper.find('a[href="https://panel.example.com/"]').exists()).toBe(false)
  completed = true
  await vi.advanceTimersByTimeAsync(2000); await flushPromises()
  expect(wrapper.get('a[href="https://panel.example.com/"]').attributes('rel')).toBe('noopener noreferrer')
  expect(wrapper.text()).toContain(messages[locale].panelHostname.success)
  expect(wrapper.text()).toContain('8443')
})

it('does not navigate to untrusted URLs and blocks recovery-required changes', async () => {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => ({
    ok: true, status: 200, json: async () => url.includes('/operations') ? { operations: [] }
      : { enabled: true, hostname: 'panel.example.com', url: 'https://attacker.example/', autoRenew: false, recoveryRequired: true },
  })))
  wrapper = mount(PanelHostnameSettings, { global: { plugins: [createI18n({ legacy: false, locale: 'en', messages })] } })
  await flushPromises()
  expect(wrapper.get<HTMLFieldSetElement>('fieldset').element.disabled).toBe(true)
  expect(wrapper.find('a[href="https://attacker.example/"]').exists()).toBe(false)
  expect(wrapper.text()).toContain(messages.en.panelHostname.recovery)
})
