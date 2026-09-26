// SPDX-License-Identifier: AGPL-3.0-or-later
import { api, type Operation } from './api'

export const operationPending = (op: Operation) => ['pending', 'running', 'cancelling'].includes(op.status)

// Observation only: never resubmit a mutation when polling fails.
export function monitorAdminOperation(id: string, observed: (items: Operation[]) => void,
  completed: (operation: Operation) => Promise<void>, failed: () => void) {
  let stopped = false
  let timer: ReturnType<typeof setTimeout> | undefined
  const deadline = Date.now() + 10 * 60_000
  async function poll() {
    try {
      const items = await api.operations()
      if (stopped) return
      observed(items)
      const op = items.find((item) => item.id === id)
      if (!op || Date.now() >= deadline) { failed(); return }
      if (!operationPending(op)) { await completed(op); return }
      timer = setTimeout(() => { void poll() }, 2000)
    } catch { if (!stopped) failed() }
  }
  timer = setTimeout(() => { void poll() }, 2000)
  return () => { stopped = true; clearTimeout(timer) }
}
