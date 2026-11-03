import type { TransactionKind } from '../api/types'

const moneyFormatter = new Intl.NumberFormat('zh-CN', {
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
})

const integerFormatter = new Intl.NumberFormat('zh-CN', {
  maximumFractionDigits: 0,
})

export function pad2(value: number): string {
  return String(value).padStart(2, '0')
}

/** Local calendar month in YYYY-MM, never derived through UTC. */
export function currentMonth(): string {
  const now = new Date()
  return `${now.getFullYear()}-${pad2(now.getMonth() + 1)}`
}

/** Local civil day in YYYY-MM-DD, never derived through UTC. */
export function localDayString(date: Date): string {
  return `${date.getFullYear()}-${pad2(date.getMonth() + 1)}-${pad2(date.getDate())}`
}

/** Last calendar day of a YYYY-MM month, computed without UTC conversion. */
export function monthEndDate(month: string): string {
  const match = /^(\d{4})-(\d{2})$/.exec(month)
  if (!match) return month
  const year = Number(match[1])
  const monthIndex = Number(match[2])
  if (monthIndex < 1 || monthIndex > 12) return month
  const leap = (year % 4 === 0 && year % 100 !== 0) || year % 400 === 0
  const days =
    monthIndex === 2
      ? leap
        ? 29
        : 28
      : [4, 6, 9, 11].includes(monthIndex)
        ? 30
        : 31
  return `${match[1]}-${match[2]}-${pad2(days)}`
}

/**
 * Local civil date-time in YYYY-MM-DDTHH:mm:ss, never derived through UTC.
 * Matches the transaction `date` API field exactly (no timezone offset).
 */
export function nowLocalDateTime(): string {
  const now = new Date()
  return `${now.getFullYear()}-${pad2(now.getMonth() + 1)}-${pad2(now.getDate())}T${pad2(now.getHours())}:${pad2(now.getMinutes())}:${pad2(now.getSeconds())}`
}

export function toNumber(value: string | number | null | undefined): number {
  if (value === null || value === undefined || value === '') return 0
  const number = typeof value === 'number' ? value : Number(value)
  return Number.isFinite(number) ? number : 0
}

export function formatMoney(value: string | number | null | undefined): string {
  const number = toNumber(value)
  const sign = number < 0 ? '−' : ''
  return `${sign}¥${moneyFormatter.format(Math.abs(number))}`
}

export function formatSignedAmount(
  amount: string | number,
  kind: TransactionKind,
): string {
  if (kind === 'transfer') return formatMoney(amount)
  return `${kind === 'income' ? '+' : '−'}${formatMoney(amount)}`
}

export function formatAxisAmount(value: number): string {
  const absolute = Math.abs(value)
  if (absolute >= 10000) {
    const wan = value / 10000
    const digits = Math.abs(wan) >= 10 ? 0 : 1
    const text = wan.toFixed(digits).replace(/\.0$/, '')
    return `${text}万`
  }
  return integerFormatter.format(value)
}

export function formatMonthLabel(month: string): string {
  const match = /^(\d{4})-(\d{2})$/.exec(month)
  if (!match) return month
  return `${match[1]}年${Number(match[2])}月`
}

/** Renders a YYYY-MM-DD calendar day as YYYY年M月D日. */
export function formatDateLabel(date: string): string {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date)
  if (!match) return date
  return `${match[1]}年${Number(match[2])}月${Number(match[3])}日`
}

/**
 * Renders a transaction `date` (YYYY-MM-DDTHH:mm:ss, local civil time) as
 * YYYY-MM-DD HH:mm:ss so the seconds stay visible. Returns the input unchanged
 * when it does not have the expected shape.
 */
export function formatDateTimeLabel(value: string): string {
  const match = /^(\d{4}-\d{2}-\d{2})T(\d{2}:\d{2}:\d{2})$/.exec(value)
  if (!match) return value
  return `${match[1]} ${match[2]}`
}

export function formatDayLabel(date: string): string {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date)
  if (!match) return date
  return `${match[2]}/${match[3]}`
}

/** Serializes a numeric amount with exactly two decimal places for the API. */
export function decimalString(value: number | null | undefined): string {
  const number = value === null || value === undefined ? 0 : value
  return (Number.isFinite(number) ? number : 0).toFixed(2)
}
