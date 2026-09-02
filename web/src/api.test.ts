// SPDX-License-Identifier: AGPL-3.0-or-later
// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from 'vitest'
import { api, ApiError } from './api'

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('browser API client', () => {
  it('binds domain mutations to CSRF and idempotency without leaking route-only fields', async () => {
    vi.spyOn(document, 'cookie', 'get').mockReturnValue('__Host-sf-csrf=csrf-bound')
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 202,
      json: () => Promise.resolve({ operationId: 'operation-id', domainId: 'domain-id', status: 'pending' }),
    } as Response)
    vi.stubGlobal('fetch', fetchMock)

    await api.createDomain('account/id', {
      name: 'example.test', canonicalMode: 'serve_both',
		target: { type: 'static', rootMode: 'default' }, disableTls: false, tlsMode: 'acme',
		wafMode: 'detection_only',
    })

    const [url, request] = fetchMock.mock.calls[0] as [string, RequestInit]
    const headers = request.headers as Headers
    expect(url).toBe('/api/v1/accounts/account%2Fid/domains')
    expect(request.method).toBe('POST')
    expect(headers.get('X-CSRF-Token')).toBe('csrf-bound')
    expect(headers.get('Idempotency-Key')).not.toBe('')
    expect(headers.get('X-Request-ID')).not.toBe('')
    expect(JSON.parse(String(request.body))).toEqual({
      name: 'example.test', canonicalMode: 'serve_both',
		target: { type: 'static', rootMode: 'default' }, disableTls: false, tlsMode: 'acme',
		wafMode: 'detection_only',
    })
  })

  it('preserves stable server error codes for localized presentation', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false, status: 403,
      json: () => Promise.resolve({ code: 'permission_denied', message: 'internal English detail' }),
    } as Response))
    await expect(api.packages()).rejects.toEqual(expect.objectContaining<ApiError>({
      status: 403,
      code: 'permission_denied',
    }))
  })

  it('keeps account and domain identifiers in owner edit routes only', async () => {
    vi.spyOn(document, 'cookie', 'get').mockReturnValue('__Host-sf-csrf=csrf-bound')
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true, status: 202,
      json: () => Promise.resolve({ operationId: 'operation-id', domainId: 'domain-id', status: 'pending' }),
    } as Response)
    vi.stubGlobal('fetch', fetchMock)

    await api.updateDomain('account/id', 'domain/id', {
      canonicalMode: 'prefer_apex',
      target: { type: 'php', rootMode: 'custom', documentRoot: 'sites/example', phpVersion: '8.4' },
		wafMode: 'blocking_pl1',
    })

    const [url, request] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/accounts/account%2Fid/domains/domain%2Fid')
    expect(request.method).toBe('PATCH')
    expect(JSON.parse(String(request.body))).toEqual({
      canonicalMode: 'prefer_apex',
      target: { type: 'php', rootMode: 'custom', documentRoot: 'sites/example', phpVersion: '8.4' },
		wafMode: 'blocking_pl1',
    })
  })

  it('keeps PHP health inside the encoded account route', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true, status: 200,
      json: () => Promise.resolve({ availableVersions: [], pools: [] }),
    } as Response)
    vi.stubGlobal('fetch', fetchMock)

    await api.accountPHP('account/id')

    const [url, request] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/accounts/account%2Fid/php')
    expect(request.method).toBe('GET')
  })

  it('builds an encoded same-origin file download URL without fetching into memory', () => {
    expect(api.fileDownloadURL('account/id', 'public_html/site data.txt')).toBe(
      '/api/v1/accounts/account%2Fid/files/download?path=public_html%2Fsite+data.txt',
    )
  })

  it('binds destructive backup restore to CSRF, idempotency, and exact confirmation', async () => {
    vi.spyOn(document, 'cookie', 'get').mockReturnValue('__Host-sf-csrf=csrf-bound')
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true, status: 200,
      json: () => Promise.resolve({ completed: true }),
    } as Response)
    vi.stubGlobal('fetch', fetchMock)
    await api.restoreBackup('account/id', 'backup/id', 'backup/id')
    const [url, request] = fetchMock.mock.calls[0] as [string, RequestInit]
    const headers = request.headers as Headers
    expect(url).toBe('/api/v1/accounts/account%2Fid/backups/backup%2Fid/restore')
    expect(request.method).toBe('POST')
    expect(headers.get('X-CSRF-Token')).toBe('csrf-bound')
    expect(headers.get('Idempotency-Key')).toBeTruthy()
    expect(JSON.parse(String(request.body))).toEqual({ confirmation: 'backup/id' })
  })

	it('streams a resumable file chunk with CSRF and an exact upload offset', async () => {
		vi.spyOn(document, 'cookie', 'get').mockReturnValue('__Host-sf-csrf=csrf-bound')
		const fetchMock = vi.fn().mockResolvedValue({
			ok: true, status: 200,
			json: () => Promise.resolve({
				uploadId: 'upload-id', directory: 'public_html', name: 'site.bin',
				sizeBytes: 8, receivedBytes: 8, createdAt: '2026-08-28T12:00:00Z',
			}),
		} as Response)
		vi.stubGlobal('fetch', fetchMock)
		const chunk = new Blob(['fort'], { type: 'application/octet-stream' })

		await api.uploadFileChunk('account/id', 'upload/id', 4, chunk)

		const [url, request] = fetchMock.mock.calls[0] as [string, RequestInit]
		const headers = request.headers as Record<string, string>
		expect(url).toBe('/api/v1/accounts/account%2Fid/file-uploads/upload%2Fid')
		expect(request.method).toBe('PUT')
		expect(request.body).toBe(chunk)
		expect(headers['Upload-Offset']).toBe('4')
		expect(headers['X-CSRF-Token']).toBe('csrf-bound')
		expect(headers['Content-Type']).toBe('application/octet-stream')
	})

	it('sends a closed account-scoped file copy with browser protections', async () => {
		vi.spyOn(document, 'cookie', 'get').mockReturnValue('__Host-sf-csrf=csrf-bound')
		const fetchMock = vi.fn().mockResolvedValue({
			ok: true, status: 200,
			json: () => Promise.resolve({ operationId: 'operation-id', completed: true }),
		} as Response)
		vi.stubGlobal('fetch', fetchMock)
		await api.mutateFileNode('account/id', {
			action: 'copy', sourceDirectory: 'public_html', sourceName: 'index.html',
			destinationDirectory: 'public_html/assets', destinationName: 'index.html',
		})
		const [url, request] = fetchMock.mock.calls[0] as [string, RequestInit]
		const headers = request.headers as Headers
		expect(url).toBe('/api/v1/accounts/account%2Fid/file-operations')
		expect(request.method).toBe('POST')
		expect(headers.get('X-CSRF-Token')).toBe('csrf-bound')
		expect(headers.get('Idempotency-Key')).toBeTruthy()
		expect(JSON.parse(String(request.body))).toEqual({
			action: 'copy', sourceDirectory: 'public_html', sourceName: 'index.html',
			destinationDirectory: 'public_html/assets', destinationName: 'index.html',
		})
	})

	it('sends a closed account-scoped archive extraction with browser protections', async () => {
		vi.spyOn(document, 'cookie', 'get').mockReturnValue('__Host-sf-csrf=csrf-bound')
		const fetchMock = vi.fn().mockResolvedValue({
			ok: true, status: 200,
			json: () => Promise.resolve({ operationId: 'operation-id', completed: true }),
		} as Response)
		vi.stubGlobal('fetch', fetchMock)
		await api.mutateFileArchive('account/id', {
			action: 'extract', format: 'zip', sourceDirectory: 'public_html', sourceName: 'assets.zip',
			destinationDirectory: 'public_html', destinationName: 'assets',
		})
		const [url, request] = fetchMock.mock.calls[0] as [string, RequestInit]
		const headers = request.headers as Headers
		expect(url).toBe('/api/v1/accounts/account%2Fid/file-archives')
		expect(request.method).toBe('POST')
		expect(headers.get('X-CSRF-Token')).toBe('csrf-bound')
		expect(headers.get('Idempotency-Key')).toBeTruthy()
		expect(JSON.parse(String(request.body))).toEqual({
			action: 'extract', format: 'zip', sourceDirectory: 'public_html', sourceName: 'assets.zip',
			destinationDirectory: 'public_html', destinationName: 'assets',
		})
	})

  it('keeps account and operation identifiers inside the scoped status route', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true, status: 200,
      json: () => Promise.resolve({ id: 'operation-id', status: 'running', progressPercent: 50 }),
    } as Response)
    vi.stubGlobal('fetch', fetchMock)

    await api.accountOperation('account/id', 'operation/id')

    const [url, request] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/accounts/account%2Fid/operations/operation%2Fid')
    expect(request.method).toBe('GET')
  })

  it('requests a phpMyAdmin handoff with CSRF and keeps the bearer out of the URL', async () => {
    vi.spyOn(document, 'cookie', 'get').mockReturnValue('__Host-sf-csrf=csrf-bound')
    const handoffToken = 'a'.repeat(43)
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true, status: 201,
      json: () => Promise.resolve({
        handoffToken, expiresAt: '2026-08-26T12:00:30Z',
        launchPath: '/phpmyadmin/stackfort-launch.php',
      }),
    } as Response)
    vi.stubGlobal('fetch', fetchMock)

    await api.phpMyAdminHandoff('account/id', 'user/id')

    const [url, request] = fetchMock.mock.calls[0] as [string, RequestInit]
    const headers = request.headers as Headers
    expect(url).toBe('/api/v1/accounts/account%2Fid/database-users/user%2Fid/phpmyadmin-handoffs')
    expect(url).not.toContain(handoffToken)
    expect(request.method).toBe('POST')
    expect(request.body).toBeUndefined()
    expect(headers.get('X-CSRF-Token')).toBe('csrf-bound')
  })

  it('queues database password rotation with CSRF and idempotency', async () => {
    vi.spyOn(document, 'cookie', 'get').mockReturnValue('__Host-sf-csrf=csrf-bound')
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true, status: 202,
      json: () => Promise.resolve({ operationId: 'rotation-operation', status: 'pending' }),
    } as Response)
    vi.stubGlobal('fetch', fetchMock)

    await api.rotateDatabaseCredential('account/id', 'user/id')

    const [url, request] = fetchMock.mock.calls[0] as [string, RequestInit]
    const headers = request.headers as Headers
    expect(url).toBe('/api/v1/accounts/account%2Fid/database-users/user%2Fid/credential/rotate')
    expect(request.method).toBe('POST')
    expect(request.body).toBeUndefined()
    expect(headers.get('X-CSRF-Token')).toBe('csrf-bound')
    expect(headers.get('Idempotency-Key')).toBeTruthy()
  })

  it('keeps certificate history identifiers inside the account and domain route', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true, status: 200,
      json: () => Promise.resolve({ certificates: [] }),
    } as Response)
    vi.stubGlobal('fetch', fetchMock)

    await api.certificates('account/id', 'domain/id')

    const [url, request] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/accounts/account%2Fid/domains/domain%2Fid/tls/certificates')
    expect(request.method).toBe('GET')
  })

  it('registers only the fixed production ACME environment with browser protections', async () => {
    vi.spyOn(document, 'cookie', 'get').mockReturnValue('__Host-sf-csrf=csrf-bound')
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true, status: 202,
      json: () => Promise.resolve({ operationId: 'operation-id', status: 'pending' }),
    } as Response)
    vi.stubGlobal('fetch', fetchMock)

    await api.registerACMEAccount({
      environment: 'letsencrypt-production', contactEmail: 'admin@example.test', termsAccepted: true,
    })

    const [url, request] = fetchMock.mock.calls[0] as [string, RequestInit]
    const headers = request.headers as Headers
    expect(url).toBe('/api/v1/admin/acme/accounts')
    expect(request.method).toBe('POST')
    expect(headers.get('X-CSRF-Token')).toBe('csrf-bound')
    expect(headers.get('Idempotency-Key')).not.toBe('')
    expect(JSON.parse(String(request.body))).toEqual({
      environment: 'letsencrypt-production', contactEmail: 'admin@example.test', termsAccepted: true,
    })
  })

  it('binds update-policy changes and manual checks to same-origin CSRF', async () => {
    vi.spyOn(document, 'cookie', 'get').mockReturnValue('__Host-sf-csrf=csrf-bound')
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true, status: 200,
      json: () => Promise.resolve({ channel: 'beta', automaticChecks: false }),
    } as Response)
    vi.stubGlobal('fetch', fetchMock)

    await api.updatePolicy({ channel: 'beta', automaticChecks: false })
    await api.checkUpdates()

    const [policyURL, policyRequest] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(policyURL).toBe('/api/v1/admin/updates/policy')
    expect(policyRequest.method).toBe('PATCH')
    expect((policyRequest.headers as Headers).get('X-CSRF-Token')).toBe('csrf-bound')
    expect(JSON.parse(String(policyRequest.body))).toEqual({ channel: 'beta', automaticChecks: false })
    const [checkURL, checkRequest] = fetchMock.mock.calls[1] as [string, RequestInit]
    expect(checkURL).toBe('/api/v1/admin/updates/check')
    expect(checkRequest.method).toBe('POST')
    expect((checkRequest.headers as Headers).get('X-CSRF-Token')).toBe('csrf-bound')
  })
})
