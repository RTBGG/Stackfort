// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, expect, it } from 'vitest'
import { messages, supportedLocales } from './i18n'

function leafKeys(value: unknown, prefix = ''): string[] {
  if (typeof value !== 'object' || value === null) return [prefix]
  return Object.entries(value).flatMap(([key, child]) =>
    leafKeys(child, prefix ? `${prefix}.${key}` : key),
  )
}

function leafMessages(value: unknown, prefix = ''): Array<[string, string]> {
  if (typeof value === 'string') return [[prefix, value]]
  if (typeof value !== 'object' || value === null) return []
  return Object.entries(value).flatMap(([key, child]) =>
    leafMessages(child, prefix ? `${prefix}.${key}` : key),
  )
}

function placeholders(message: string): string[] {
  return [...message.matchAll(/\{([A-Za-z][A-Za-z0-9_]*)\}/g)]
    .map((match) => match[1] ?? '')
    .sort()
}

describe('translations', () => {
  it('keeps every supported locale structurally complete', () => {
    const sourceKeys = leafKeys(messages.en).sort()
    for (const locale of supportedLocales) {
      expect(leafKeys(messages[locale]).sort()).toEqual(sourceKeys)
    }
  })

  it('uses English as the source locale', () => {
    expect(supportedLocales[0]).toBe('en')
  })

  it('keeps messages non-empty and interpolation placeholders aligned', () => {
    const sourceMessages = new Map(leafMessages(messages.en))
    for (const locale of supportedLocales) {
      for (const [key, message] of leafMessages(messages[locale])) {
        expect(message.trim(), `${locale}.${key} is empty`).not.toBe('')
        expect(placeholders(message), `${locale}.${key} has different placeholders`).toEqual(
          placeholders(sourceMessages.get(key) ?? ''),
        )
      }
    }
  })
})
