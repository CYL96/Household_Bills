import { ref } from 'vue'
import type { TransactionKind } from '../api/types'
import { EXPENSE_CATEGORIES, INCOME_CATEGORIES } from '../utils/categories'
import { isCategoryIconId, type CategoryIconId } from '../utils/categoryIcons'
import { authState } from './auth'

export type CategoryKind = 'expense' | 'income'

export interface CategoryEntry {
  name: string
  visible: boolean
  /** Explicit icon choice; omitted entries keep the built-in name mapping. */
  icon?: CategoryIconId
  /**
   * Built-in category this entry came from.
   * Kept across renames so the old default is never appended again.
   */
  defaultName?: string
  /** Names this entry used before a rename; historical transactions match. */
  previousNames?: string[]
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

/** Drops unusable legacy names while keeping the rest of the entry intact. */
function parsePreviousNames(value: unknown, name: string): string[] | undefined {
  if (!Array.isArray(value)) return undefined
  const previousNames: string[] = []
  const seen = new Set<string>()
  for (const item of value) {
    if (typeof item !== 'string') return undefined
    const previous = normalizeCategoryName(item)
    if (!previous) return undefined
    if (previous === name || seen.has(previous)) continue
    seen.add(previous)
    previousNames.push(previous)
  }
  return previousNames.length > 0 ? previousNames : undefined
}

function copyCategoryEntry(entry: CategoryEntry): CategoryEntry {
  const copy: CategoryEntry = { name: entry.name, visible: entry.visible }
  if (entry.icon !== undefined) copy.icon = entry.icon
  if (entry.defaultName !== undefined) copy.defaultName = entry.defaultName
  if (entry.previousNames !== undefined) {
    copy.previousNames = [...entry.previousNames]
  }
  return copy
}

/**
 * Fills `defaultName` for legacy entries that predate both markers so their
 * built-in origin survives the next save. Entries already carrying metadata
 * (a source or historical names) are left alone; a default already declared
 * by a renamed entry is never claimed twice.
 */
function inferDefaultNames(
  entries: CategoryEntry[],
  defaults: readonly string[],
): void {
  const defaultNames = new Set(defaults)
  const declared = new Set<string>()
  for (const entry of entries) {
    if (entry.defaultName !== undefined) declared.add(entry.defaultName)
  }
  for (const entry of entries) {
    if (entry.defaultName !== undefined || entry.previousNames !== undefined) {
      continue
    }
    if (!defaultNames.has(entry.name) || declared.has(entry.name)) continue
    entry.defaultName = entry.name
    declared.add(entry.name)
  }
}

function entryMatchesName(entry: CategoryEntry, name: string): boolean {
  return (
    entry.name === name ||
    entry.defaultName === name ||
    entry.previousNames?.includes(name) === true
  )
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

function parseStoredEntries(
  raw: string,
  defaults: readonly string[],
): CategoryEntry[] | null {
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

  const defaultNames = new Set(defaults)
  const declaredDefaults = new Set<string>()
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
    const entry: CategoryEntry = { name, visible: candidate.visible }
    // Older entries have no icon; invalid saved icons only lose the icon itself.
    if (isCategoryIconId(candidate.icon)) entry.icon = candidate.icon
    const previousNames = parsePreviousNames(candidate.previousNames, name)
    if (previousNames) entry.previousNames = previousNames
    // Invalid optional metadata only drops that field, never the whole entry.
    if (typeof candidate.defaultName === 'string') {
      const defaultName = normalizeCategoryName(candidate.defaultName)
      if (
        defaultName &&
        defaultNames.has(defaultName) &&
        !declaredDefaults.has(defaultName)
      ) {
        entry.defaultName = defaultName
        declaredDefaults.add(defaultName)
      }
    }
    entries.push(entry)
  }
  if (!entries.some((entry) => entry.visible)) return null
  return entries
}

/**
 * Reads the saved draft for the active household account.
 * Missing built-in categories are appended so new defaults are introduced
 * without resetting any saved order or visibility choices. Renamed defaults
 * keep their source marker, so their old default names are not appended again.
 */
function loadEntries(kind: CategoryKind): CategoryEntry[] {
  const defaults = kind === 'income' ? INCOME_CATEGORIES : EXPENSE_CATEGORIES
  let entries: CategoryEntry[] | null = null
  const key = storageKey(kind)
  const storage = key ? getStorage() : null
  if (key && storage) {
    try {
      const raw = storage.getItem(key)
      if (raw !== null) entries = parseStoredEntries(raw, defaults)
    } catch {
      entries = null
    }
  }

  const source: CategoryEntry[] =
    entries ?? defaults.map((name) => ({ name, visible: true }))
  const result = source.map(copyCategoryEntry)
  inferDefaultNames(result, defaults)
  for (const name of defaults) {
    // A renamed default still declares its source; a custom entry sharing a
    // default name must not be duplicated either.
    const represented = result.some(
      (entry) => entry.defaultName === name || entry.name === name,
    )
    if (represented) continue
    result.push({ name, visible: true, defaultName: name })
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
    if (current && !entries.some((entry) => entryMatchesName(entry, current))) {
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

  const defaults = kind === 'income' ? INCOME_CATEGORIES : EXPENSE_CATEGORIES
  const defaultNames = new Set(defaults)
  const declaredDefaults = new Set<string>()
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
    let icon: CategoryIconId | undefined
    if (candidate.icon !== undefined) {
      if (!isCategoryIconId(candidate.icon)) {
        throw new Error(`分类「${name}」的图标无效，请重新选择图标`)
      }
      icon = candidate.icon
    }
    let defaultName: string | undefined
    if (candidate.defaultName !== undefined) {
      const normalizedDefault =
        typeof candidate.defaultName === 'string'
          ? normalizeCategoryName(candidate.defaultName)
          : null
      if (
        !normalizedDefault ||
        !defaultNames.has(normalizedDefault) ||
        declaredDefaults.has(normalizedDefault)
      ) {
        throw new Error(`分类「${name}」的内置来源无效，请重新打开设置`)
      }
      defaultName = normalizedDefault
      declaredDefaults.add(defaultName)
    }
    let previousNames: string[] | undefined
    if (candidate.previousNames !== undefined) {
      if (!Array.isArray(candidate.previousNames)) {
        throw new Error(`分类「${name}」的旧名称无效，请重新打开设置`)
      }
      previousNames = []
      const seenPrevious = new Set<string>()
      for (const rawPrevious of candidate.previousNames) {
        const previous =
          typeof rawPrevious === 'string'
            ? normalizeCategoryName(rawPrevious)
            : null
        if (!previous) {
          throw new Error(`分类「${name}」的旧名称无效，请重新打开设置`)
        }
        if (previous === name || seenPrevious.has(previous)) continue
        seenPrevious.add(previous)
        previousNames.push(previous)
      }
      if (previousNames.length === 0) previousNames = undefined
    }
    const normalizedEntry: CategoryEntry = { name, visible: candidate.visible }
    if (icon !== undefined) normalizedEntry.icon = icon
    if (defaultName !== undefined) normalizedEntry.defaultName = defaultName
    if (previousNames !== undefined) normalizedEntry.previousNames = previousNames
    normalized.push(normalizedEntry)
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
