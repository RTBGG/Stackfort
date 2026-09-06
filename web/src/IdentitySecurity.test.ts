// SPDX-License-Identifier: AGPL-3.0-or-later
// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import axe from 'axe-core'
import IdentitySecurity from './IdentitySecurity.vue'
import MFARecoveryView from './MFARecoveryView.vue'
import { api, ApiError } from './api'
import { messages } from './i18n'

let wrapper: VueWrapper | undefined
const enrollment = () => ({ challengeId: 'challenge-id', secret: 'JBSWY3DPEHPK3PXP', provisioningUri: 'otpauth://never-send-externally', expiresAt: new Date(Date.now() + 600_000).toISOString() })
function setup(enabled = false, locale = 'en') {
  vi.spyOn(api, 'totpStatus').mockResolvedValue({ enabled, recoveryCodesRemaining: enabled ? 8 : 0 })
  wrapper = mount(IdentitySecurity, { attachTo: document.body, global: { plugins: [createI18n({ legacy: false, locale, messages })] } })
  return wrapper
}
afterEach(() => { wrapper?.unmount(); wrapper = undefined; document.body.innerHTML = ''; vi.restoreAllMocks(); vi.useRealTimers() })

describe('identity MFA settings', () => {
  it('confirms enrollment and hands off recovery codes without persisting secrets', async () => {
    const begin = vi.spyOn(api, 'beginTOTP').mockResolvedValue(enrollment())
    const confirm = vi.spyOn(api, 'confirmTOTP').mockResolvedValue({ factorId: 'factor-id', activatedAt: new Date().toISOString(), recoveryCodes: ['new-recovery-code'] })
    const view = setup(); await flushPromises()
    await view.get('form').trigger('submit'); await flushPromises()
    expect(begin).toHaveBeenCalledWith('')
    expect(view.get('input[readonly]').element).toHaveProperty('value', 'JBSWY3DPEHPK3PXP')
    expect(view.html()).not.toContain('otpauth:')
    expect(document.activeElement).toBe(view.get('input[inputmode="numeric"]').element)
    await view.get('input[inputmode="numeric"]').setValue('123456')
    await view.get('form').trigger('submit'); await flushPromises()
    expect(confirm).toHaveBeenCalledWith('challenge-id', '123456')
    expect(view.emitted('changed')).toEqual([[['new-recovery-code']]])
    expect(view.find('input[readonly]').exists()).toBe(false)
    expect(JSON.stringify(localStorage)).not.toContain('JBSWY3DPEHPK3PXP')
    expect(JSON.stringify(sessionStorage)).not.toContain('new-recovery-code')
  })

  it('requires current proof for replacement and clears it after use', async () => {
    const begin = vi.spyOn(api, 'beginTOTP').mockResolvedValue(enrollment())
    const view = setup(true); await flushPromises()
    await view.get('form').trigger('submit'); expect(begin).not.toHaveBeenCalled()
    await view.get('input[type="password"]').setValue('current-recovery-code')
    await view.get('form').trigger('submit'); await flushPromises()
    expect(begin).toHaveBeenCalledWith('current-recovery-code')
    await view.get('button.secondary-action').trigger('click'); await flushPromises()
    expect(view.get('input[type="password"]').element).toHaveProperty('value', '')
    expect(view.text()).not.toContain('current-recovery-code')
  })

  it('gates removal on proof and explicit confirmation', async () => {
    const remove = vi.spyOn(api, 'disableTOTP').mockResolvedValue(undefined)
    const view = setup(true, 'de'); await flushPromises()
    expect(view.text()).toContain('Zwei-Faktor-Authentifizierung')
    await view.get('input[type="password"]').setValue('123456')
    expect(view.get('button.danger-action').attributes('disabled')).toBeDefined()
    await view.get('input[type="checkbox"]').setValue(true)
    await view.get('button.danger-action').trigger('click'); await flushPromises()
    expect(remove).toHaveBeenCalledWith('123456')
    expect(view.emitted('changed')).toEqual([[[]]])
  })

  it('discards expired enrollment and never confirms it', async () => {
    vi.useFakeTimers()
    vi.spyOn(api, 'beginTOTP').mockResolvedValue(enrollment())
    const confirm = vi.spyOn(api, 'confirmTOTP')
    const view = setup(); await flushPromises()
    await view.get('form').trigger('submit'); await flushPromises()
    await vi.advanceTimersByTimeAsync(600_001)
    expect(view.find('input[readonly]').exists()).toBe(false)
    expect(view.get('[role="alert"]').text()).toContain('expired')
    expect(confirm).not.toHaveBeenCalled()
  })

  it('shows a localized reauthentication path, not internal error details', async () => {
    vi.spyOn(api, 'beginTOTP').mockRejectedValue(new ApiError(403, 'recent_authentication_required', 'internal error detail'))
    const view = setup(); await flushPromises()
    await view.get('form').trigger('submit'); await flushPromises()
    expect(view.text()).toContain('Sign out and sign in again')
    await view.get('button.secondary-action').trigger('click')
    expect(view.emitted('logout')).toHaveLength(1)
  })

  it('ignores late setup responses after unmount and coalesces submissions', async () => {
    let resolve!: (value: ReturnType<typeof enrollment>) => void
    const begin = vi.spyOn(api, 'beginTOTP').mockImplementation(() => new Promise((done) => { resolve = done }))
    const view = setup(); await flushPromises()
    await view.get('form').trigger('submit'); await view.get('form').trigger('submit')
    expect(begin).toHaveBeenCalledTimes(1)
    view.unmount(); wrapper = undefined
    resolve(enrollment()); await flushPromises()
    expect(view.emitted('changed')).toBeUndefined()
  })

  it('requires acknowledgement on the accessible recovery-only page', async () => {
    wrapper = mount(MFARecoveryView, { props: { codes: ['recovery-1', 'recovery-2'] }, attachTo: document.body, global: { plugins: [createI18n({ legacy: false, locale: 'en', messages })] } })
    expect(wrapper.get('button').attributes('disabled')).toBeDefined()
    expect(document.activeElement).toBe(wrapper.get('h1').element)
    expect((await axe.run(wrapper.element, { rules: { 'color-contrast': { enabled: false } } })).violations).toEqual([])
    await wrapper.get('input[type="checkbox"]').setValue(true)
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('done')).toHaveLength(1)
  })
})
