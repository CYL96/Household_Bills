import { ref } from 'vue'
import type { TransactionKind } from '../api/types'
import { EXPENSE_CATEGORIES, INCOME_CATEGORIES } from '../utils/categories'
import { isCategoryIconId, type CategoryIconId } from '../utils/categoryIcons'
import { authState } from './auth'

export type CategoryKind = 'expense' | 'income'

export interface CategoryEntry {
  name: string
  visible: boolean
  /**
   * Name of the top-level entry this entry belongs to.
   * Omitted for top-level entries; a tree is never deeper than two levels.
   */
  parentName?: string
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

interface ParsedCategoryPreferences {
  entries: CategoryEntry[]
  /** Built-in defaults the user deleted for good; they must never come back. */
  deletedDefaults: string[]
}

/** Current saved shape: hierarchical entries plus permanent deletions. */
interface StoredCategoryPreferencesV2 {
  version: 2
  entries: CategoryEntry[]
  deletedDefaults: string[]
}

/**
 * The `.v1` suffix is historical: the shape moved on to version 2 but the key
 * stays put so saved preferences (order, icons, renames) survive the upgrade.
 */
const STORAGE_KEY_PREFIX = 'household-bills.categories.v1'
const STORAGE_VERSION = 2
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
  if (entry.parentName !== undefined) copy.parentName = entry.parentName
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

/**
 * Parses one saved entry list. Version 1 lists are flat by definition, so any
 * stray `parentName` is ignored there; version 2 keeps well-formed references
 * and repairs the rest later.
 */
function parseStoredEntryList(
  rawEntries: unknown[],
  defaults: readonly string[],
  allowParents: boolean,
): CategoryEntry[] | null {
  const defaultNames = new Set(defaults)
  const declaredDefaults = new Set<string>()
  const entries: CategoryEntry[] = []
  const seen = new Set<string>()
  for (const rawEntry of rawEntries) {
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
    // A broken parent reference is dropped below; the entry becomes top level.
    if (allowParents && typeof candidate.parentName === 'string') {
      const parentName = normalizeCategoryName(candidate.parentName)
      if (parentName && parentName !== name) entry.parentName = parentName
    }
    entries.push(entry)
  }
  return entries
}

/**
 * Conservative hierarchy repair for stored data: a reference to a missing
 * entry, to itself, or to an entry that has its own parent would create a
 * third level, so that reference is dropped and the entry stays top level.
 */
function repairCategoryHierarchy(entries: CategoryEntry[]): void {
  const byName = new Map(entries.map((entry) => [entry.name, entry] as const))
  for (const entry of entries) {
    const parentName = entry.parentName
    if (parentName === undefined) continue
    const parent = byName.get(parentName)
    if (!parent || parent.parentName !== undefined) {
      delete entry.parentName
    }
  }
}

function parseDeletedDefaults(
  value: unknown,
  defaults: readonly string[],
): string[] {
  if (!Array.isArray(value)) return []
  const defaultNames = new Set(defaults)
  const deleted: string[] = []
  const seen = new Set<string>()
  for (const item of value) {
    if (typeof item !== 'string') continue
    const name = normalizeCategoryName(item)
    if (!name || !defaultNames.has(name) || seen.has(name)) continue
    seen.add(name)
    deleted.push(name)
  }
  return deleted
}

function parseStoredPreferences(
  raw: string,
  defaults: readonly string[],
): ParsedCategoryPreferences | null {
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
  if (!Array.isArray(envelope.entries) || envelope.entries.length === 0) return null

  let entries: CategoryEntry[] | null
  let deletedDefaults: string[] = []
  if (envelope.version === 1) {
    // Version 1 knew no hierarchy, so every saved entry is a top-level one.
    entries = parseStoredEntryList(envelope.entries, defaults, false)
  } else if (envelope.version === 2) {
    entries = parseStoredEntryList(envelope.entries, defaults, true)
    if (entries) {
      repairCategoryHierarchy(entries)
      deletedDefaults = parseDeletedDefaults(envelope.deletedDefaults, defaults)
    }
  } else {
    return null
  }
  if (!entries) return null
  // The tree always keeps at least one usable top-level category.
  if (!entries.some((entry) => entry.parentName === undefined && entry.visible)) {
    return null
  }
  return { entries, deletedDefaults }
}

/**
 * Reads the saved draft for the active household account.
 * Missing built-in categories are appended so new defaults are introduced
 * without resetting any saved order or visibility choices. Renamed defaults
 * keep their source marker, so their old default names are not appended again.
 * Defaults the user deleted permanently stay deleted.
 */
function loadEntries(kind: CategoryKind): CategoryEntry[] {
  const defaults = kind === 'income' ? INCOME_CATEGORIES : EXPENSE_CATEGORIES
  let preferences: ParsedCategoryPreferences | null = null
  const key = storageKey(kind)
  const storage = key ? getStorage() : null
  if (key && storage) {
    try {
      const raw = storage.getItem(key)
      if (raw !== null) preferences = parseStoredPreferences(raw, defaults)
    } catch {
      preferences = null
    }
  }

  const source: CategoryEntry[] =
    preferences?.entries ?? defaults.map((name) => ({ name, visible: true }))
  const deletedDefaults = new Set(preferences?.deletedDefaults ?? [])
  const result = source.map(copyCategoryEntry)
  inferDefaultNames(result, defaults)
  for (const name of defaults) {
    if (deletedDefaults.has(name)) continue
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

/** Saved categories for the active account; nothing is synthesized. */
export function readCategoryEntries(kind: CategoryKind): CategoryEntry[] {
  // Register the reactive dependency so settings changes refresh open drafts.
  void categoryRevision.value
  return loadEntries(kind)
}

/**
 * Effective visibility: an entry shows up only when it is enabled itself and
 * every ancestor is enabled too. Disabling a top-level category hides its
 * whole subtree while the children keep their own `visible` flags.
 * Dangling parent references behave as top level; reference cycles never show.
 */
export function isCategoryVisible(
  entry: CategoryEntry,
  entries: readonly CategoryEntry[],
): boolean {
  if (!entry.visible) return false
  const seen = new Set<string>([entry.name])
  let parentName = entry.parentName
  while (parentName !== undefined) {
    const parent = entries.find((candidate) => candidate.name === parentName)
    if (!parent) return true
    if (!parent.visible) return false
    if (seen.has(parent.name)) return false
    seen.add(parent.name)
    parentName = parent.parentName
  }
  return true
}

/**
 * Names that may appear on saved transactions for a category and its children:
 * the current names plus historical/default aliases, deduplicated. Only the
 * two supported levels are walked, and disabled entries stay included because
 * their historical bills still exist.
 */
export function categoryBillNames(
  entries: readonly CategoryEntry[],
  name: string,
): string[] {
  const current = normalizeCategoryName(name)
  if (!current) return []
  const entry =
    entries.find((candidate) => candidate.name === current) ??
    entries.find((candidate) => entryMatchesName(candidate, current))
  if (!entry) return [current]

  const names: string[] = []
  const seen = new Set<string>()
  const add = (value: string | undefined): void => {
    if (value === undefined || seen.has(value)) return
    seen.add(value)
    names.push(value)
  }
  add(entry.name)
  for (const previous of entry.previousNames ?? []) add(previous)
  add(entry.defaultName)
  for (const child of entries) {
    if (child.parentName !== entry.name) continue
    add(child.name)
    for (const previous of child.previousNames ?? []) add(previous)
    add(child.defaultName)
  }
  return names
}

/**
 * Flat names for legacy consumers that only understand a single list.
 * Every effectively visible entry (both levels) appears once, in saved order.
 */
export function visibleCategorySuggestions(kind: TransactionKind): string[] {
  if (kind === 'transfer') return []
  // Reading auth state and the revision keeps computed callers up to date.
  void categoryRevision.value
  const userId = authState.user?.id
  const householdId = authState.household?.id
  if (userId === undefined || householdId === undefined) {
    return [...(kind === 'income' ? INCOME_CATEGORIES : EXPENSE_CATEGORIES)]
  }
  const entries = readCategoryEntries(kind)
  return entries
    .filter((entry) => isCategoryVisible(entry, entries))
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
    let parentName: string | undefined
    if (candidate.parentName !== undefined) {
      const normalizedParent =
        typeof candidate.parentName === 'string'
          ? normalizeCategoryName(candidate.parentName)
          : null
      if (!normalizedParent) {
        throw new Error(`分类「${name}」的上级分类无效，请重新选择`)
      }
      parentName = normalizedParent
    }
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
    if (parentName !== undefined) normalizedEntry.parentName = parentName
    if (icon !== undefined) normalizedEntry.icon = icon
    if (defaultName !== undefined) normalizedEntry.defaultName = defaultName
    if (previousNames !== undefined) normalizedEntry.previousNames = previousNames
    normalized.push(normalizedEntry)
  }

  // Hierarchy is validated only after every name is known, so entry order in
  // the incoming draft cannot hide a broken parent reference.
  const byName = new Map(normalized.map((entry) => [entry.name, entry] as const))
  for (const entry of normalized) {
    const parentName = entry.parentName
    if (parentName === undefined) continue
    if (parentName === entry.name) {
      throw new Error(`分类「${entry.name}」不能以自身作为上级分类`)
    }
    const parent = byName.get(parentName)
    if (!parent) {
      throw new Error(
        `分类「${entry.name}」的上级分类「${parentName}」不存在，请重新选择`,
      )
    }
    if (parent.parentName !== undefined) {
      throw new Error(
        `分类「${entry.name}」不能放在二级分类「${parentName}」下，最多支持两级分类`,
      )
    }
  }
  if (!normalized.some((entry) => entry.parentName === undefined && entry.visible)) {
    throw new Error('请至少保留一个显示的一级分类')
  }

  // A known default that no entry represents was deleted on purpose.
  const deletedDefaults = defaults.filter(
    (name) =>
      !normalized.some(
        (entry) => entry.defaultName === name || entry.name === name,
      ),
  )

  const storage = getStorage()
  if (!storage) {
    throw new Error('无法使用本机存储，请检查浏览器设置后重试')
  }

  const payload: StoredCategoryPreferencesV2 = {
    version: STORAGE_VERSION,
    entries: normalized,
    deletedDefaults,
  }
  try {
    storage.setItem(key, JSON.stringify(payload))
  } catch {
    throw new Error('无法在本机保存分类设置，请检查浏览器存储权限或空间')
  }

  categoryRevision.value += 1
}
