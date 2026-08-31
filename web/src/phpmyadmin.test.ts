// SPDX-License-Identifier: AGPL-3.0-or-later
// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from 'vitest'
import { submitPHPMyAdminHandoff } from './phpmyadmin'

afterEach(() => {
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})

describe('phpMyAdmin browser handoff', () => {
  it('submits the bearer only in a transient same-origin POST form', () => {
    const handoffToken = 'a'.repeat(43)
    let submittedMethod = ''
    let submittedAction = ''
    let submittedToken = ''
    vi.spyOn(HTMLFormElement.prototype, 'submit').mockImplementation(function (this: HTMLFormElement) {
      submittedMethod = this.method
      submittedAction = this.getAttribute('action') ?? ''
      submittedToken = this.querySelector<HTMLInputElement>('input[name="handoff_token"]')?.value ?? ''
    })

    submitPHPMyAdminHandoff({
      handoffToken, expiresAt: '2026-08-26T12:00:30Z',
      launchPath: '/phpmyadmin/stackfort-launch.php',
    })

    expect(submittedMethod).toBe('post')
    expect(submittedAction).toBe('/phpmyadmin/stackfort-launch.php')
    expect(submittedAction).not.toContain(handoffToken)
    expect(submittedToken).toBe(handoffToken)
    expect(document.querySelector('form')).toBeNull()
  })

  it('rejects substituted paths and malformed bearers', () => {
    expect(() => submitPHPMyAdminHandoff({
      handoffToken: 'short', expiresAt: '2026-08-26T12:00:30Z',
      launchPath: '/phpmyadmin/stackfort-launch.php',
    })).toThrow()
  })
})
