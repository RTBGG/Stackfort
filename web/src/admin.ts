// SPDX-License-Identifier: AGPL-3.0-or-later

export type AdminPageKey =
  | 'overview'
  | 'packages'
  | 'accounts'
  | 'domains'
  | 'operations'
  | 'services'
  | 'audit'
  | 'updates'
  | 'settings'

export const workspaceNavigation = [
  { key: 'overview', icon: 'grid' },
  { key: 'packages', icon: 'package' },
  { key: 'accounts', icon: 'users' },
  { key: 'domains', icon: 'globe' },
  { key: 'operations', icon: 'pulse' },
] as const satisfies ReadonlyArray<{ key: AdminPageKey; icon: string }>

export const platformNavigation = [
  { key: 'services', icon: 'server' },
  { key: 'audit', icon: 'shield' },
  { key: 'updates', icon: 'update' },
  { key: 'settings', icon: 'settings' },
] as const satisfies ReadonlyArray<{ key: AdminPageKey; icon: string }>
