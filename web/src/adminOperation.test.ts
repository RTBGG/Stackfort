// SPDX-License-Identifier: AGPL-3.0-or-later
import { afterEach, expect, it, vi } from 'vitest'
import { api, type Operation } from './api'
import { monitorAdminOperation } from './adminOperation'
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks() })

it('stops observation on disposal and never retries a mutation', async () => {
  vi.useFakeTimers()
  const request = vi.spyOn(api, 'operations').mockResolvedValue([{ id: 'op', status: 'running' } as Operation])
  const failed = vi.fn(), complete = vi.fn()
  const stop = monitorAdminOperation('op', vi.fn(), complete, failed)
  await vi.advanceTimersByTimeAsync(2000)
  expect(request).toHaveBeenCalledTimes(1)
  stop()
  await vi.advanceTimersByTimeAsync(60_000)
  expect(request).toHaveBeenCalledTimes(1)
  expect(complete).not.toHaveBeenCalled()
  expect(failed).not.toHaveBeenCalled()
})

it('reports failed operations and stops on unavailable status', async () => {
  vi.useFakeTimers()
  vi.spyOn(api, 'operations').mockResolvedValue([{ id: 'op', status: 'failed' } as Operation])
  const complete = vi.fn().mockResolvedValue(undefined), failed = vi.fn()
  monitorAdminOperation('op', vi.fn(), complete, failed)
  await vi.advanceTimersByTimeAsync(2000)
  expect(complete).toHaveBeenCalledWith(expect.objectContaining({ status: 'failed' }))
  vi.mocked(api.operations).mockRejectedValue(new Error('network unavailable'))
  monitorAdminOperation('op', vi.fn(), complete, failed)
  await vi.advanceTimersByTimeAsync(60_000)
  expect(failed).toHaveBeenCalledTimes(1)
})
