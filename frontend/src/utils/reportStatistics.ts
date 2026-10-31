import type { Transaction } from '../api/types'
import {
  entryMatchesName,
  type CategoryEntry,
} from './categoryPreferences'

/** One drill-down level (top-level category or its child) of a report. */
export interface ReportCategory {
  /** Stable identity within its sibling list. */
  key: string
  name: string
  /** Aggregated amount, always fixed to two decimals. */
  amount: string
  count: number
  /** Share in 0..100, two decimal places: of the report at level one, of the parent below. */
  percentage: number
  /** Configured second level; always empty when the category has no children. */
  children: ReportCategory[]
  /**
   * Original transactions of this level, newest first. A top-level row keeps
   * its child transactions too, so drilling down never needs a new request.
   */
  transactions: Transaction[]
}

/** Complete category report for one kind (income or expense). */
export interface CategoryReport {
  amount: string
  count: number
  rows: ReportCategory[]
}

/**
 * A child entry may still carry an unusable parent reference in edge data
 * (missing or nested). Such an entry is reported as its own top-level row,
 * matching the hierarchy repair used by the category settings.
 */
interface EntryPlacement {
  top: CategoryEntry
  child?: CategoryEntry
}

interface Bucket {
  key: string
  name: string
  cents: bigint
  transactions: Transaction[]
  children: Map<string, Bucket>
}

/** NUL is rejected by category names, so the direct slice key never collides. */
const DIRECT_KEY_SUFFIX = '\u0000direct'
const DIRECT_NAME = '未细分'

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

/** Monthly totals stay independent of the selected category and report kind. */
export function sumReportAmounts(
  transactions: readonly Transaction[],
): { expense: string; income: string } {
  let expense = 0n
  let income = 0n
  for (const transaction of transactions) {
    if (transaction.kind === 'expense') {
      expense += amountToCents(transaction.amount)
    } else if (transaction.kind === 'income') {
      income += amountToCents(transaction.amount)
    }
  }
  return { expense: centsToAmount(expense), income: centsToAmount(income) }
}

/** Newest date first, then highest id first. */
function compareTransactions(a: Transaction, b: Transaction): number {
  if (a.date !== b.date) return a.date < b.date ? 1 : -1
  return b.id - a.id
}

function compareBuckets(a: Bucket, b: Bucket): number {
  if (a.cents !== b.cents) return a.cents > b.cents ? -1 : 1
  const byName = a.name.localeCompare(b.name, 'zh-Hans-CN')
  if (byName !== 0) return byName
  return a.key.localeCompare(b.key)
}

/**
 * Rounded share of `part` in `total`, clamped to 0..100. Amounts are always
 * positive in practice; the guards keep malformed data from producing NaN.
 */
function percentageOf(part: bigint, total: bigint): number {
  if (part <= 0n || total <= 0n) return 0
  const hundredths = (part * 10000n + total / 2n) / total
  if (hundredths >= 10000n) return 100
  return Number(hundredths) / 100
}

function createBucket(key: string, name: string): Bucket {
  return { key, name, cents: 0n, transactions: [], children: new Map() }
}

function addTransaction(bucket: Bucket, transaction: Transaction): void {
  bucket.cents += amountToCents(transaction.amount)
  bucket.transactions.push(transaction)
}

function toReportCategory(
  bucket: Bucket,
  denominator: bigint,
): ReportCategory {
  const children = [...bucket.children.values()].sort(compareBuckets)
  return {
    key: bucket.key,
    name: bucket.name,
    amount: centsToAmount(bucket.cents),
    count: bucket.transactions.length,
    percentage: percentageOf(bucket.cents, denominator),
    children: children.map((child) =>
      toReportCategory(child, bucket.cents),
    ),
    transactions: [...bucket.transactions].sort(compareTransactions),
  }
}

/**
 * Builds the income or expense category report shown by the statistics view.
 *
 * Transactions of the requested kind are grouped by their configured
 * top-level category. A category name resolves to its current entry name
 * first, then through historical names and the built-in default. Unknown
 * historical names stay independent top-level rows; disabled entries keep
 * counting. Transfers never appear. A top-level row gets a second level only
 * when the configuration declares child entries for it: recorded children
 * plus a direct "unclassified" row when the parent itself was recorded.
 * Rows and children are ordered by amount descending; transactions within a
 * row are ordered by date and then id, both descending.
 */
export function buildCategoryReport(
  transactions: readonly Transaction[],
  entries: readonly CategoryEntry[],
  kind: 'income' | 'expense',
): CategoryReport {
  const entryByName = new Map<string, CategoryEntry>()
  for (const entry of entries) {
    if (!entryByName.has(entry.name)) entryByName.set(entry.name, entry)
  }

  const placements = new Map<CategoryEntry, EntryPlacement>()
  const placementOf = (entry: CategoryEntry): EntryPlacement => {
    const cached = placements.get(entry)
    if (cached) return cached

    let placement: EntryPlacement = { top: entry }
    const parentName = entry.parentName
    if (parentName !== undefined) {
      const parent = entryByName.get(parentName)
      if (
        parent &&
        parent !== entry &&
        parent.parentName === undefined
      ) {
        placement = { top: parent, child: entry }
      }
    }
    placements.set(entry, placement)
    return placement
  }

  const parentsWithChildren = new Set<string>()
  for (const entry of entries) {
    const placement = placementOf(entry)
    if (placement.child) parentsWithChildren.add(placement.top.name)
  }

  const matchedEntries = new Map<string, CategoryEntry | null>()
  const entryForName = (name: string): CategoryEntry | undefined => {
    const cached = matchedEntries.get(name)
    if (cached !== undefined) return cached ?? undefined

    const entry =
      entryByName.get(name) ??
      entries.find((candidate) => entryMatchesName(candidate, name))
    matchedEntries.set(name, entry ?? null)
    return entry
  }

  const buckets = new Map<string, Bucket>()
  const bucketFor = (key: string, name: string): Bucket => {
    let bucket = buckets.get(key)
    if (!bucket) {
      bucket = createBucket(key, name)
      buckets.set(key, bucket)
    }
    return bucket
  }

  let totalCents = 0n
  let count = 0

  for (const transaction of transactions) {
    // The requested kind is income or expense, so transfers never pass here.
    if (transaction.kind !== kind) continue
    count += 1
    totalCents += amountToCents(transaction.amount)

    const entry = entryForName(transaction.category)
    let topKey: string
    let topName: string
    let placement: EntryPlacement | undefined

    if (entry) {
      placement = placementOf(entry)
      topKey = placement.top.name
      topName = placement.top.name
    } else {
      // Unknown historical category: keep its original name as its own row.
      topKey = transaction.category
      topName = transaction.category
    }

    const top = bucketFor(topKey, topName)
    addTransaction(top, transaction)

    if (placement?.child) {
      const childKey = placement.child.name
      const child =
        top.children.get(childKey) ??
        createBucket(childKey, placement.child.name)
      top.children.set(childKey, child)
      addTransaction(child, transaction)
    } else if (parentsWithChildren.has(top.name)) {
      const directKey = `${top.key}${DIRECT_KEY_SUFFIX}`
      const direct =
        top.children.get(directKey) ?? createBucket(directKey, DIRECT_NAME)
      top.children.set(directKey, direct)
      addTransaction(direct, transaction)
    }
  }

  const rows = [...buckets.values()]
    .sort(compareBuckets)
    .map((bucket) => toReportCategory(bucket, totalCents))

  return {
    amount: centsToAmount(totalCents),
    count,
    rows,
  }
}
