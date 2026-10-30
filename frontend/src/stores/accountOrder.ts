import type { Account } from '../api/types'
import { authState } from './auth'

interface StoredAccountOrder {
  version: 1
  order: number[]
}

const STORAGE_KEY_PREFIX = 'household-bills.account-order.v1'
const STORAGE_VERSION = 1

function isAccountId(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value > 0
}

/** Current household + signed-in user scope; no implicit fallback identity. */
function storageKey(): string | null {
  const userId = authState.user?.id
  const householdId = authState.household?.id
  if (!isAccountId(userId) || !isAccountId(householdId)) return null
  return `${STORAGE_KEY_PREFIX}.h${householdId}.u${userId}`
}

function getStorage(): Storage | null {
  try {
    if (typeof window === 'undefined') return null
    return window.localStorage
  } catch {
    return null
  }
}

/** Keeps the first occurrence of each valid positive safe integer ID. */
function sanitizeIds(values: readonly unknown[]): number[] {
  const ids: number[] = []
  const seen = new Set<number>()
  for (const value of values) {
    if (!isAccountId(value) || seen.has(value)) continue
    seen.add(value)
    ids.push(value)
  }
  return ids
}

function parseStoredOrder(raw: string): number[] {
  let parsed: unknown
  try {
    parsed = JSON.parse(raw)
  } catch {
    return []
  }
  if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
    return []
  }
  const envelope = parsed as Record<string, unknown>
  if (envelope.version !== STORAGE_VERSION || !Array.isArray(envelope.order)) {
    return []
  }
  return sanitizeIds(envelope.order)
}

/**
 * Reads the saved account order for the active household and user.
 * Missing, corrupt, foreign-version, or unavailable storage reads yield [].
 */
export function readAccountOrder(): number[] {
  const key = storageKey()
  const storage = key ? getStorage() : null
  if (!key || !storage) return []
  try {
    const raw = storage.getItem(key)
    return raw === null ? [] : parseStoredOrder(raw)
  } catch {
    return []
  }
}

/**
 * Saves the account order for the active household and user.
 * Throws instead of reporting success when login scope, storage, or the write
 * itself is unavailable.
 */
export function saveAccountOrder(ids: readonly number[]): void {
  const key = storageKey()
  if (!key) {
    throw new Error('请先登录，再保存账户顺序')
  }
  if (!Array.isArray(ids)) {
    throw new Error('账户顺序数据无效，请刷新页面后重试')
  }

  const order: number[] = []
  const seen = new Set<number>()
  for (const id of ids) {
    if (!isAccountId(id)) {
      throw new Error('账户顺序数据无效，请刷新页面后重试')
    }
    if (seen.has(id)) continue
    seen.add(id)
    order.push(id)
  }

  const storage = getStorage()
  if (!storage) {
    throw new Error('无法使用本机存储，请检查浏览器设置后重试')
  }

  const payload: StoredAccountOrder = {
    version: STORAGE_VERSION,
    order,
  }
  try {
    storage.setItem(key, JSON.stringify(payload))
  } catch {
    throw new Error('无法在本机保存账户顺序，请检查浏览器存储权限或空间')
  }
}

/**
 * Returns accounts in saved order, ignoring stale and duplicate saved IDs.
 * Accounts missing from the saved order keep their input order at the end.
 * Neither the accounts nor the ID list is mutated.
 */
export function orderAccounts(
  accounts: readonly Account[],
  ids: readonly number[],
): Account[] {
  const byId = new Map<number, Account>()
  for (const account of accounts) {
    if (!byId.has(account.id)) byId.set(account.id, account)
  }

  const ordered: Account[] = []
  const emitted = new Set<number>()
  for (const id of ids) {
    if (!isAccountId(id) || emitted.has(id)) continue
    const account = byId.get(id)
    if (!account) continue
    emitted.add(id)
    ordered.push(account)
  }
  for (const account of accounts) {
    if (emitted.has(account.id)) continue
    emitted.add(account.id)
    ordered.push(account)
  }
  return ordered
}
