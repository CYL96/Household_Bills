import { isCategoryIconId } from './categoryIcons'
import {
  categoryDefaults,
  normalizeCategoryName,
  parseDeletedDefaults,
  parsePreviousNames,
  repairCategoryHierarchy,
  type CategoryEntry,
  type CategoryKind,
  type CategoryPreferences,
} from './categoryPreferences'

/**
 * Legacy category dictionaries lived in localStorage per household and user.
 * Only income is adopted when the server says the household has no shared
 * snapshot yet: the rebuilt expense tree replaces the old expense dictionary,
 * which is never read again. The keys stay in place as a backup and are never
 * written.
 */
const STORAGE_KEY_PREFIX = 'household-bills.categories.v1'

function storageKey(
  kind: CategoryKind,
  householdId: number,
  userId: number,
): string {
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

function parseStoredPreferences(
  raw: string,
  defaults: readonly string[],
): CategoryPreferences | null {
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
  // A saved configuration always keeps at least one usable top-level category.
  if (!entries.some((entry) => entry.parentName === undefined && entry.visible)) {
    return null
  }
  return { entries, deletedDefaults }
}

function readLegacyKind(
  kind: CategoryKind,
  householdId: number,
  userId: number,
): CategoryPreferences | null {
  const storage = getStorage()
  if (!storage) return null
  try {
    const raw = storage.getItem(storageKey(kind, householdId, userId))
    if (raw === null) return null
    return parseStoredPreferences(raw, categoryDefaults(kind))
  } catch {
    return null
  }
}

/**
 * Legacy preferences for the active account. Expense is always null because
 * the old expense dictionary is deliberately ignored, so cleared categories
 * cannot be imported again.
 */
export interface LegacyCategorySnapshot {
  expense: null
  income: CategoryPreferences | null
}

/**
 * Reads the current account's legacy income dictionary. Returns null when no
 * usable income configuration exists, even if only an old expense dictionary
 * does, so the household stays uninitialized and falls back to the built-in
 * tree instead of adopting the cleared expense categories.
 */
export function readLegacyCategorySnapshot(
  householdId: number,
  userId: number,
): LegacyCategorySnapshot | null {
  const income = readLegacyKind('income', householdId, userId)
  if (income === null) return null
  return { expense: null, income }
}
