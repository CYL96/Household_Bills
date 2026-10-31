import type { SummaryCategory } from '../api/types'
import {
  entryMatchesName,
  type CategoryEntry,
} from './categoryPreferences'

/** Parses an API amount (fixed two decimals) without floating point math. */
function amountToCents(amount: string): bigint {
  const [yuan = '0', fraction = ''] = amount.split('.')
  return BigInt(yuan + fraction.padEnd(2, '0').slice(0, 2))
}

function centsToAmount(cents: bigint): string {
  const negative = cents < 0n
  const absolute = negative ? -cents : cents
  const yuan = absolute / 100n
  const fraction = (absolute % 100n).toString().padStart(2, '0')
  return `${negative ? '-' : ''}${yuan}.${fraction}`
}

/**
 * Merges summary categories into the current household's top-level buckets.
 * A transaction name matches an entry by its current name first, then through
 * its previous names and default source. Child entries join their current
 * parent; names that match no entry stay independent under their original
 * name. Disabled entries are not filtered: historical transactions still
 * count. Results are sorted by amount descending, then by name.
 */
export function aggregateTopLevelCategories(
  categories: readonly SummaryCategory[],
  entries: readonly CategoryEntry[],
): SummaryCategory[] {
  const entriesByName = new Map(
    entries.map((entry) => [entry.name, entry] as const),
  )
  const totals = new Map<string, bigint>()

  for (const item of categories) {
    const entry =
      entriesByName.get(item.category) ??
      entries.find((candidate) =>
        entryMatchesName(candidate, item.category),
      )
    let label = item.category
    if (entry) {
      label = entry.name
      if (entry.parentName !== undefined) {
        const parent = entriesByName.get(entry.parentName)
        if (parent && parent.parentName === undefined) {
          label = parent.name
        }
      }
    }
    const cents = amountToCents(item.amount)
    totals.set(label, (totals.get(label) ?? 0n) + cents)
  }

  return [...totals]
    .map(([category, cents]) => ({ category, cents }))
    .sort((a, b) => {
      if (a.cents !== b.cents) return a.cents > b.cents ? -1 : 1
      return a.category.localeCompare(b.category, 'zh-Hans-CN')
    })
    .map(({ category, cents }) => ({
      category,
      amount: centsToAmount(cents),
    }))
}
