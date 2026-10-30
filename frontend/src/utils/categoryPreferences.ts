import {
  EXPENSE_CATEGORIES,
  EXPENSE_CATEGORY_TREE,
  INCOME_CATEGORIES,
} from './categories'
import { isCategoryIconId, type CategoryIconId } from './categoryIcons'

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

/** One complete kind configuration as stored on the server. */
export interface CategoryPreferences {
  entries: CategoryEntry[]
  /** Built-in defaults the household deleted for good; they must never come back. */
  deletedDefaults: string[]
}

const MAX_CATEGORY_NAME_LENGTH = 64

export function categoryDefaults(kind: CategoryKind): readonly string[] {
  return kind === 'income' ? INCOME_CATEGORIES : EXPENSE_CATEGORIES
}

/** A built-in category and, for a child, the group that declares it. */
interface DefaultCategoryEntry {
  name: string
  parentName?: string
}

/**
 * Built-in entries for one kind in display order, parents before children.
 * Income stays a flat list; expense carries the two-level tree.
 */
function defaultCategoryEntries(kind: CategoryKind): DefaultCategoryEntry[] {
  if (kind === 'income') return INCOME_CATEGORIES.map((name) => ({ name }))
  return EXPENSE_CATEGORY_TREE.flatMap((group) => [
    { name: group.name },
    ...group.children.map((name) => ({ name, parentName: group.name })),
  ])
}

export function normalizeCategoryName(value: string): string | null {
  const name = value.trim()
  if (!name || name.includes('\u0000')) return null
  if (Array.from(name).length > MAX_CATEGORY_NAME_LENGTH) return null
  return name
}

/** Drops unusable stored names while keeping the rest of the entry intact. */
export function parsePreviousNames(
  value: unknown,
  name: string,
): string[] | undefined {
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

export function entryMatchesName(entry: CategoryEntry, name: string): boolean {
  return (
    entry.name === name ||
    entry.defaultName === name ||
    entry.previousNames?.includes(name) === true
  )
}

/**
 * Conservative hierarchy repair for stored data: a reference to a missing
 * entry, to itself, or to an entry that has its own parent would create a
 * third level, so that reference is dropped and the entry stays top level.
 */
export function repairCategoryHierarchy(entries: CategoryEntry[]): void {
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

export function parseDeletedDefaults(
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

/**
 * Materializes a stored snapshot for one kind. Missing built-in categories are
 * appended so new defaults are introduced without resetting any saved order or
 * visibility choices. Renamed defaults keep their source marker and are found
 * through their aliases, so an old default name is never appended again. A
 * missing child is appended only under its current top-level parent; a parent
 * that was deleted or moved below another entry leaves that child out instead
 * of creating a dangling or third level. Defaults the household deleted
 * permanently stay deleted. A `null` snapshot yields the complete tree.
 */
export function materializeCategoryEntries(
  kind: CategoryKind,
  preferences: CategoryPreferences | null | undefined,
): CategoryEntry[] {
  const defaults = defaultCategoryEntries(kind)
  const defaultNames = categoryDefaults(kind)
  const deletedDefaults = new Set(preferences?.deletedDefaults ?? [])
  const source: CategoryEntry[] =
    preferences?.entries && preferences.entries.length > 0
      ? preferences.entries
      : defaults.map((entry) => {
          const copy: CategoryEntry = {
            name: entry.name,
            visible: true,
            defaultName: entry.name,
          }
          if (entry.parentName !== undefined) copy.parentName = entry.parentName
          return copy
        })
  const result = source.map(copyCategoryEntry)
  inferDefaultNames(result, defaultNames)
  for (const entry of defaults) {
    if (deletedDefaults.has(entry.name)) continue
    // A renamed default, including one whose old name became a historical
    // alias, already represents this category; appending it would duplicate.
    if (result.some((candidate) => entryMatchesName(candidate, entry.name))) {
      continue
    }
    const parentDefaultName = entry.parentName
    let parentName: string | undefined
    if (parentDefaultName !== undefined) {
      // The default parent may have been renamed; only its current top-level
      // name is a valid reference. No parent means no reachable place for the
      // child, and a nested parent would create a third level.
      const parent = result.find(
        (candidate) =>
          candidate.parentName === undefined &&
          entryMatchesName(candidate, parentDefaultName),
      )
      if (!parent) continue
      parentName = parent.name
    }
    const appended: CategoryEntry = {
      name: entry.name,
      visible: true,
      defaultName: entry.name,
    }
    if (parentName !== undefined) appended.parentName = parentName
    result.push(appended)
  }
  // Stored data can still reference a missing or nested parent; keep the
  // two-level invariant after appending as well.
  repairCategoryHierarchy(result)
  return result
}

/**
 * Normalizes and validates a complete draft for one kind, using the same rules
 * the category settings dialog edits against. Returns the server payload shape
 * and derives `deletedDefaults` from the entries: a known default that no
 * entry represents was deleted on purpose.
 */
export function prepareCategoryPreferences(
  kind: CategoryKind,
  entries: readonly CategoryEntry[],
): CategoryPreferences {
  if (!Array.isArray(entries)) {
    throw new Error('分类设置数据无效，请重新打开设置')
  }

  const defaults = categoryDefaults(kind)
  const defaultNames = new Set(defaults)
  const declaredDefaults = new Set<string>()
  const normalized: CategoryEntry[] = []
  const seen = new Set<string>()
  for (const entry of entries as readonly unknown[]) {
    if (typeof entry !== 'object' || entry === null || Array.isArray(entry)) {
      throw new Error('分类设置数据无效，请重新打开设置')
    }
    const candidate = entry as Record<string, unknown>
    if (
      typeof candidate.name !== 'string' ||
      typeof candidate.visible !== 'boolean'
    ) {
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

  // An entry represents its default by current name, source marker, or a
  // historical alias, so a rename never counts as deleting the default.
  const deletedDefaults = defaults.filter(
    (name) => !normalized.some((entry) => entryMatchesName(entry, name)),
  )

  return { entries: normalized, deletedDefaults }
}
