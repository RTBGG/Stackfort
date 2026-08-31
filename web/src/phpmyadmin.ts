// SPDX-License-Identifier: AGPL-3.0-or-later

import type { PHPMyAdminHandoff } from './api'

const launchPath = '/phpmyadmin/stackfort-launch.php'
const handoffPattern = /^[A-Za-z0-9_-]{43}$/

// Submit the bearer in a same-origin POST body. It is never placed in a URL,
// browser storage, Vue state, or a referrer-bearing navigation.
export function submitPHPMyAdminHandoff(handoff: PHPMyAdminHandoff): void {
  if (handoff.launchPath !== launchPath || !handoffPattern.test(handoff.handoffToken)) {
    throw new Error('Invalid phpMyAdmin handoff response')
  }
  const form = document.createElement('form')
  form.method = 'POST'
  form.action = launchPath
  form.hidden = true
  const token = document.createElement('input')
  token.type = 'hidden'
  token.name = 'handoff_token'
  token.value = handoff.handoffToken
  form.append(token)
  document.body.append(form)
  try {
    form.submit()
  } finally {
    form.remove()
  }
}
