// SPDX-License-Identifier: AGPL-3.0-or-later

import type { SupportedLocale } from './i18n'

const localeTags: Record<SupportedLocale, string> = {
  en: 'en-US',
  de: 'de-DE',
}

const byteUnits = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'] as const
const narrowNoBreakSpace = '\u202f'

export type DateTimeFormatOptions = Pick<
  Intl.DateTimeFormatOptions,
  'dateStyle' | 'timeStyle' | 'timeZone'
>

function assertFinite(value: number, label: string): void {
  if (!Number.isFinite(value)) throw new RangeError(`${label} must be finite`)
}

function assertNonNegative(value: number, label: string): void {
  assertFinite(value, label)
  if (value < 0) throw new RangeError(`${label} must not be negative`)
}

export function localeTag(locale: SupportedLocale): string {
  return localeTags[locale]
}

export function formatNumber(
  value: number,
  locale: SupportedLocale,
  options: Intl.NumberFormatOptions = {},
): string {
  assertFinite(value, 'number')
  return new Intl.NumberFormat(localeTag(locale), options).format(value)
}

export function formatPercent(
  value: number,
  locale: SupportedLocale,
  options: Intl.NumberFormatOptions = {},
): string {
  return formatNumber(value, locale, {
    style: 'percent',
    maximumFractionDigits: 1,
    ...options,
  })
}

export function formatDateTime(
  value: string | number | Date,
  locale: SupportedLocale,
  options: DateTimeFormatOptions = {},
): string {
  const date = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(date.getTime())) throw new RangeError('date must be valid')
  return new Intl.DateTimeFormat(localeTag(locale), {
    dateStyle: 'medium',
    timeStyle: 'short',
    ...options,
  }).format(date)
}

export function formatBytes(bytes: number, locale: SupportedLocale): string {
  assertNonNegative(bytes, 'bytes')
  const unitIndex = bytes === 0
    ? 0
    : Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), byteUnits.length - 1)
  const value = bytes / (1024 ** unitIndex)
  const amount = formatNumber(value, locale, {
    maximumFractionDigits: unitIndex > 0 && value < 10 ? 1 : 0,
  })
  return `${amount}${narrowNoBreakSpace}${byteUnits[unitIndex]}`
}

export function formatRate(bytesPerSecond: number, locale: SupportedLocale): string {
  assertNonNegative(bytesPerSecond, 'rate')
  return `${formatBytes(bytesPerSecond, locale)}${narrowNoBreakSpace}/s`
}

export function formatDuration(seconds: number, locale: SupportedLocale): string {
  assertNonNegative(seconds, 'duration')
  let remaining = Math.round(seconds)
  const units: Array<[Intl.NumberFormatOptions['unit'], number]> = [
    ['day', Math.floor(remaining / 86_400)],
    ['hour', Math.floor((remaining % 86_400) / 3_600)],
    ['minute', Math.floor((remaining % 3_600) / 60)],
    ['second', remaining % 60],
  ]
  const parts = units
    .filter(([, amount]) => amount > 0)
    .map(([unit, amount]) => new Intl.NumberFormat(localeTag(locale), {
      style: 'unit',
      unit,
      unitDisplay: 'short',
    }).format(amount))
  if (parts.length === 0) {
    parts.push(new Intl.NumberFormat(localeTag(locale), {
      style: 'unit',
      unit: 'second',
      unitDisplay: 'short',
    }).format(0))
  }
  return new Intl.ListFormat(localeTag(locale), {
    style: 'short',
    type: 'conjunction',
  }).format(parts)
}
