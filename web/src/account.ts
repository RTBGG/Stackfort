// SPDX-License-Identifier: AGPL-3.0-or-later

export type AccountPageKey = 'overview' | 'domains' | 'files' | 'backups' | 'logs' | 'databases' | 'jobs' | 'usage' | 'profile' | 'sessions'

export const accountNavigation = [
  { key: 'overview', icon: 'grid' },
  { key: 'domains', icon: 'globe' },
  { key: 'files', icon: 'folder' },
  { key: 'backups', icon: 'shield' },
  { key: 'logs', icon: 'pulse' },
  { key: 'databases', icon: 'database' },
  { key: 'jobs', icon: 'pulse' },
  { key: 'usage', icon: 'pulse' },
  { key: 'profile', icon: 'users' },
  { key: 'sessions', icon: 'shield' },
] as const satisfies ReadonlyArray<{ key: AccountPageKey; icon: string }>
