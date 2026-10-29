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

/** Local calendar date in YYYY-MM-DD, never derived through UTC. */
export function todayISO(): string {
  const now = new Date()
  return `${now.getFullYear()}-${pad2(now.getMonth() + 1)}-${pad2(now.getDate())}`
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
  kind: 'income' | 'expense',
): string {
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

export function formatDateLabel(date: string): string {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date)
  if (!match) return date
  return `${match[1]}年${Number(match[2])}月${Number(match[3])}日`
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
