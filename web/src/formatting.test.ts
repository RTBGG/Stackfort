// SPDX-License-Identifier: AGPL-3.0-or-later

import { describe, expect, it } from 'vitest'
import {
  formatBytes,
  formatDateTime,
  formatDuration,
  formatNumber,
  formatPercent,
  formatRate,
  localeTag,
} from './formatting'

describe('locale-aware formatting', () => {
  it('maps application locales to explicit BCP 47 tags', () => {
    expect(localeTag('en')).toBe('en-US')
    expect(localeTag('de')).toBe('de-DE')
  })

  it('formats numbers and percentages for English and German', () => {
    expect(formatNumber(1_234_567.89, 'en')).toBe('1,234,567.89')
    expect(formatNumber(1_234_567.89, 'de')).toBe('1.234.567,89')
    expect(formatPercent(0.125, 'en')).toBe('12.5%')
    expect(formatPercent(0.125, 'de')).toBe('12,5 %')
  })

  it('formats binary byte quantities and rates with localized numbers', () => {
    expect(formatBytes(1_536, 'en')).toBe('1.5 KiB')
    expect(formatBytes(1_536, 'de')).toBe('1,5 KiB')
    expect(formatRate(1_572_864, 'en')).toBe('1.5 MiB /s')
    expect(formatRate(1_572_864, 'de')).toBe('1,5 MiB /s')
  })

  it('formats dates and durations through the selected locale', () => {
    const value = '2026-08-24T12:34:00Z'
    expect(formatDateTime(value, 'en', { timeZone: 'UTC' })).toBe('Aug 24, 2026, 12:34 PM')
    expect(formatDateTime(value, 'de', { timeZone: 'UTC' })).toBe('24.08.2026, 12:34')
    expect(formatDuration(3_661, 'en')).toContain('1 hr')
    expect(formatDuration(3_661, 'de')).toContain('1 Std.')
  })

  it('rejects invalid measurements instead of producing misleading output', () => {
    expect(() => formatBytes(-1, 'en')).toThrow(RangeError)
    expect(() => formatRate(Number.NaN, 'en')).toThrow(RangeError)
    expect(() => formatDuration(Number.POSITIVE_INFINITY, 'en')).toThrow(RangeError)
    expect(() => formatDateTime('not-a-date', 'en')).toThrow(RangeError)
  })
})
