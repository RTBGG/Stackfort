// SPDX-License-Identifier: AGPL-3.0-or-later

import http from 'node:http'

const accountID = '0198b935-b600-7000-8000-000000000501'
const operationID = '0198b935-b600-7000-8000-000000000508'
let operationPolls = 0
let operationDone = false

const account = {
  id: accountID,
  name: 'Browser account',
  slug: 'browser-account',
  status: 'active',
  membershipRole: 'owner',
  packageId: '0198b935-b600-7000-8000-000000000502',
  packageName: 'Starter',
  packageRevision: 1,
  hostReady: true,
  effectiveLimits: {
    maxDomains: 5,
    maxDatabases: 0,
    maxDatabaseUsers: 0,
    maxScheduledJobs: 0,
    maxOciApplications: 0,
    allowedPhpVersions: [],
    features: {
      ociApplications: false,
      customRedirects: true,
      wafExceptions: false,
      scheduledBackups: false,
    },
  },
  usage: { domains: 0 },
  createdAt: '2026-08-25T10:00:00Z',
  updatedAt: '2026-08-25T10:00:00Z',
}

const domain = {
  id: '0198b935-b600-7000-8000-000000000503',
  name: { display: 'browser.example.test', ascii: 'browser.example.test' },
  status: 'active',
  canonicalMode: 'serve_both',
  target: {
    id: '0198b935-b600-7000-8000-000000000504',
    type: 'static',
    documentRoot: {
      id: '0198b935-b600-7000-8000-000000000505',
      relativePath: 'public_html',
      referenceCount: 1,
    },
  },
  tls: { enabled: true, issuanceStatus: 'active', expiresAt: '2026-11-22T10:00:00Z' },
  createdAt: '2026-08-25T10:00:00Z',
  updatedAt: '2026-08-25T10:00:01Z',
}

function respond(response, status, value, headers = {}) {
  response.writeHead(status, { 'Content-Type': 'application/json', ...headers })
  response.end(JSON.stringify(value))
}

const server = http.createServer((request, response) => {
  request.resume()
  const path = new URL(request.url ?? '/', 'http://127.0.0.1').pathname
  if (path === '/api/v1/health') return respond(response, 200, { status: 'ok' })
  if (path === '/api/v1/build') {
    return respond(response, 200, { version: 'dev', commit: 'browser-test', buildDate: '2026-08-25T10:00:00Z' })
  }
  if (path === '/api/v1/bootstrap') return respond(response, 200, { required: false, capabilityActive: false })
  if (path === '/api/v1/session') {
    return respond(response, 200, {
      identity: {
        id: '0198b935-b600-7000-8000-000000000506',
        email: 'owner@example.test',
        displayName: 'Browser Owner',
        locale: 'en',
      },
      sessionId: '0198b935-b600-7000-8000-000000000507',
      authenticatedAt: '2026-08-25T10:00:00Z',
      lastSeenAt: '2026-08-25T10:00:00Z',
      expiresAt: '2026-08-25T22:00:00Z',
      authenticationLevel: 'password',
    }, { 'Set-Cookie': '__Host-sf-csrf=csrf-bound; Path=/; Secure; SameSite=Strict' })
  }
  if (path === '/api/v1/me') {
    return respond(response, 200, {
      platformAdministrator: false,
      accounts: [{ ...account, usage: { domains: operationDone ? 1 : 0 } }],
    })
  }
  if (path === '/api/v1/sessions') return respond(response, 200, { sessions: [] })
  if (path === `/api/v1/accounts/${accountID}/domains` && request.method === 'POST') {
    if (request.headers['x-csrf-token'] !== 'csrf-bound' || !request.headers['idempotency-key']) {
      return respond(response, 403, { code: 'csrf_failed' })
    }
    return respond(response, 202, { operationId: operationID, domainId: operationID, status: 'pending' })
  }
  if (path === `/api/v1/accounts/${accountID}/operations/${operationID}`) {
    operationPolls++
    const state = operationPolls === 1
      ? ['pending', 'queued', 0]
      : operationPolls === 2
        ? ['running', 'rendering', 55]
        : ['succeeded', 'completed', 100]
    if (state[0] === 'succeeded') operationDone = true
    return respond(response, 200, {
      id: operationID,
      accountId: accountID,
      kind: 'domain.lifecycle.apply',
      status: state[0],
      stage: state[1],
      progressPercent: state[2],
      attemptCount: state[0] === 'pending' ? 0 : 1,
      maxAttempts: 3,
      createdAt: '2026-08-25T10:00:00Z',
      updatedAt: '2026-08-25T10:00:01Z',
    })
  }
  if (path === `/api/v1/accounts/${accountID}/domains/${domain.id}/tls/certificates`) {
    return respond(response, 200, { certificates: [{
      id: '0198b935-b600-7000-8000-000000000509',
      status: 'active',
      names: ['browser.example.test', 'www.browser.example.test'],
      issuer: 'Stackfort Browser Test CA',
      serialHex: '1001',
      fingerprintSha256: 'ab'.repeat(32),
      notBefore: '2026-08-25T10:00:00Z',
      expiresAt: '2026-11-22T10:00:00Z',
      nextRenewalAt: '2026-10-22T10:00:00Z',
      createdAt: '2026-08-25T09:59:00Z',
      activatedAt: '2026-08-25T10:00:00Z',
    }, {
      id: '0198b935-b600-7000-8000-000000000510',
      status: 'retired',
      names: ['browser.example.test', 'www.browser.example.test'],
      issuer: 'Stackfort Browser Test CA',
      createdAt: '2026-05-25T10:00:00Z',
      retiredAt: '2026-08-25T10:00:00Z',
    }] })
  }
  if (path === `/api/v1/accounts/${accountID}/domains`) {
    return respond(response, 200, { domains: operationDone ? [domain] : [] })
  }
  return respond(response, 404, { code: 'resource_not_found' })
})

server.listen(8080, '127.0.0.1', () => {
  process.stdout.write('Stackfort browser fixture listening on 127.0.0.1:8080\n')
})
