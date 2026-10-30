import { ref } from 'vue'
import type { TransactionKind } from '../api/types'
import { EXPENSE_CATEGORIES, INCOME_CATEGORIES } from '../utils/categories'
import { authState } from './auth'

export type CategoryKind = 'expense' | 'income'

export interface CategoryEntry {
  name: string
  visible: boolean
}

interface StoredCategoryPreferences {
  version: 1
  entries: CategoryEntry[]
}

const STORAGE_KEY_PREFIX = 'household-bills.categories.v1'
const STORAGE_VERSION = 1
const MAX_CATEGORY_LENGTH = 64

/** Bumped after every successful save so computed consumers re-read storage. */
const categoryRevision = ref(0)

function normalizeCategoryName(value: string): string | null {
  const name = value.trim()
  if (!name || name.includes('\u0000')) return null
  if (Array.from(name).length > MAX_CATEGORY_LENGTH) return null
  return name
}

function storageKey(kind: CategoryKind): string | null {
  const userId = authState.user?.id
  const householdId = authState.household?.id
  if (userId === undefined || householdId === undefined) return null
  return `${STORAGE_KEY_PREFIX}.h${householdId}.u${userId}.${kind}`
}

function getStorage(): Storage | null {
  try {
    if (typeof window === 'undefined') return null
    return window.localStorage
  } catch {
    return null
  }
}

function parseStoredEntries(raw: string): CategoryEntry[] | null {
  let parsed: unknown
  try {
    parsed = JSON.parse(raw)
  } catch {
    return null
  }
  if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
    return null
  }
  const envelope = parsed as Record<string, unknown>
  if (envelope.version !== STORAGE_VERSION) return null
  if (!Array.isArray(envelope.entries) || envelope.entries.length === 0) return null

  const entries: CategoryEntry[] = []
  const seen = new Set<string>()
  for (const rawEntry of envelope.entries) {
    if (
      typeof rawEntry !== 'object' ||
      rawEntry === null ||
      Array.isArray(rawEntry)
    ) {
      return null
    }
    const candidate = rawEntry as Record<string, unknown>
    if (typeof candidate.name !== 'string') return null
    if (typeof candidate.visible !== 'boolean') return null
    const name = normalizeCategoryName(candidate.name)
    if (!name || seen.has(name)) return null
    seen.add(name)
    entries.push({ name, visible: candidate.visible })
  }
  if (!entries.some((entry) => entry.visible)) return null
  return entries
}

/**
 * Reads the saved draft for the active household account.
 * Missing built-in categories are appended so new defaults are introduced
 * without resetting any saved order or visibility choices.
 */
function loadEntries(kind: CategoryKind): CategoryEntry[] {
  let entries: CategoryEntry[] | null = null
  const key = storageKey(kind)
  const storage = key ? getStorage() : null
  if (key && storage) {
    try {
      const raw = storage.getItem(key)
      if (raw !== null) entries = parseStoredEntries(raw)
    } catch {
      entries = null
    }
  }

  const defaults = kind === 'income' ? INCOME_CATEGORIES : EXPENSE_CATEGORIES
  const source = entries ?? defaults.map((name) => ({ name, visible: true }))
  const result = source.map((entry) => ({ ...entry }))
  const known = new Set(result.map((entry) => entry.name))
  for (const name of defaults) {
    if (!known.has(name)) {
      known.add(name)
      result.push({ name, visible: true })
    }
  }
  return result
}

export function readCategoryEntries(
  kind: CategoryKind,
  currentCategory?: string,
): CategoryEntry[] {
  // Register the reactive dependency so settings changes refresh open drafts.
  void categoryRevision.value
  const entries = loadEntries(kind)
  if (typeof currentCategory === 'string') {
    const current = normalizeCategoryName(currentCategory)
    if (current && !entries.some((entry) => entry.name === current)) {
      entries.push({ name: current, visible: true })
    }
  }
  return entries
}

export function visibleCategorySuggestions(kind: TransactionKind): string[] {
  if (kind === 'transfer') return []
  // Reading auth state and the revision keeps computed callers up to date.
  void categoryRevision.value
  const userId = authState.user?.id
  const householdId = authState.household?.id
  if (userId === undefined || householdId === undefined) {
    return [...(kind === 'income' ? INCOME_CATEGORIES : EXPENSE_CATEGORIES)]
  }
  return readCategoryEntries(kind)
    .filter((entry) => entry.visible)
    .map((entry) => entry.name)
}

export function saveCategoryEntries(
  kind: CategoryKind,
  entries: CategoryEntry[],
): void {
  const key = storageKey(kind)
  if (!key) {
    throw new Error('请先登录，再保存本机分类设置')
  }
  if (!Array.isArray(entries)) {
    throw new Error('分类设置数据无效，请重新打开设置')
  }

  const normalized: CategoryEntry[] = []
  const seen = new Set<string>()
  for (const entry of entries as unknown[]) {
    if (typeof entry !== 'object' || entry === null || Array.isArray(entry)) {
      throw new Error('分类设置数据无效，请重新打开设置')
    }
    const candidate = entry as Record<string, unknown>
    if (typeof candidate.name !== 'string' || typeof candidate.visible !== 'boolean') {
      throw new Error('分类设置数据无效，请重新打开设置')
    }
    const name = normalizeCategoryName(candidate.name)
    if (!name) {
      throw new Error('分类名称需为 1 至 64 个字符，且不能包含空字符')
    }
    if (seen.has(name)) {
      throw new Error(`分类「${name}」重复，请调整后重试`)
    }
    seen.add(name)
    normalized.push({ name, visible: candidate.visible })
  }
  if (!normalized.some((entry) => entry.visible)) {
    throw new Error('请至少保留一个显示的分类')
  }

  const storage = getStorage()
  if (!storage) {
    throw new Error('无法使用本机存储，请检查浏览器设置后重试')
  }

  const payload: StoredCategoryPreferences = {
    version: STORAGE_VERSION,
    entries: normalized,
  }
  try {
    storage.setItem(key, JSON.stringify(payload))
  } catch {
    throw new Error('无法在本机保存分类设置，请检查浏览器存储权限或空间')
  }

  categoryRevision.value += 1
}
