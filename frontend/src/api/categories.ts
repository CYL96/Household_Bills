import { request } from './client'
import type {
  CategoryEntryWire,
  CategoryPreferencesWire,
  CategorySavePayloadWire,
  CategorySnapshotWire,
} from './types'
import { isCategoryIconId } from '../utils/categoryIcons'
import {
  categoryDefaults,
  normalizeCategoryName,
  parseDeletedDefaults,
  parsePreviousNames,
  repairCategoryHierarchy,
  type CategoryEntry,
  type CategoryKind,
  type CategoryPreferences,
} from '../utils/categoryPreferences'

/** Complete household snapshot in the shapes the app works with. */
export interface CategorySnapshot {
  initialized: boolean
  revision: number
  expense: CategoryPreferences | null
  income: CategoryPreferences | null
}

const INVALID_SNAPSHOT_MESSAGE = '分类设置数据格式异常，请刷新后重试。'

function requireRecord(value: unknown): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new Error(INVALID_SNAPSHOT_MESSAGE)
  }
  return value as Record<string, unknown>
}

function parseEntry(
  raw: unknown,
  kind: CategoryKind,
  declaredDefaults: Set<string>,
): CategoryEntry {
  const candidate = requireRecord(raw)
  if (
    typeof candidate.name !== 'string' ||
    typeof candidate.visible !== 'boolean'
  ) {
    throw new Error(INVALID_SNAPSHOT_MESSAGE)
  }
  const name = normalizeCategoryName(candidate.name)
  if (!name) throw new Error(INVALID_SNAPSHOT_MESSAGE)

  const entry: CategoryEntry = { name, visible: candidate.visible }
  // Unknown future icons only lose their icon here instead of breaking the
  // whole snapshot; known ids round-trip unchanged.
  if (isCategoryIconId(candidate.icon)) entry.icon = candidate.icon

  if (typeof candidate.default_name === 'string') {
    const defaultName = normalizeCategoryName(candidate.default_name)
    if (
      defaultName &&
      categoryDefaults(kind).includes(defaultName) &&
      !declaredDefaults.has(defaultName)
    ) {
      entry.defaultName = defaultName
      declaredDefaults.add(defaultName)
    }
  }

  const previousNames = parsePreviousNames(candidate.previous_names, name)
  if (previousNames) entry.previousNames = previousNames

  if (typeof candidate.parent_name === 'string') {
    const parentName = normalizeCategoryName(candidate.parent_name)
    if (parentName && parentName !== name) entry.parentName = parentName
  }
  return entry
}

function parsePreferences(
  raw: unknown,
  kind: CategoryKind,
): CategoryPreferences {
  const candidate = requireRecord(raw)
  if (!Array.isArray(candidate.entries) || candidate.entries.length === 0) {
    throw new Error(INVALID_SNAPSHOT_MESSAGE)
  }
  const declaredDefaults = new Set<string>()
  const entries: CategoryEntry[] = []
  const seen = new Set<string>()
  for (const rawEntry of candidate.entries) {
    const entry = parseEntry(rawEntry, kind, declaredDefaults)
    if (seen.has(entry.name)) throw new Error(INVALID_SNAPSHOT_MESSAGE)
    seen.add(entry.name)
    entries.push(entry)
  }
  repairCategoryHierarchy(entries)
  if (!entries.some((entry) => entry.parentName === undefined && entry.visible)) {
    throw new Error(INVALID_SNAPSHOT_MESSAGE)
  }
  return {
    entries,
    deletedDefaults: parseDeletedDefaults(
      candidate.deleted_defaults,
      categoryDefaults(kind),
    ),
  }
}

function parseSnapshot(payload: unknown): CategorySnapshot {
  const candidate = requireRecord(payload)
  const { initialized, revision } = candidate
  if (typeof initialized !== 'boolean') throw new Error(INVALID_SNAPSHOT_MESSAGE)
  if (
    typeof revision !== 'number' ||
    !Number.isInteger(revision) ||
    revision < 0
  ) {
    throw new Error(INVALID_SNAPSHOT_MESSAGE)
  }
  if (!initialized) {
    return { initialized: false, revision: 0, expense: null, income: null }
  }
  if (revision < 1) throw new Error(INVALID_SNAPSHOT_MESSAGE)
  return {
    initialized: true,
    revision,
    expense: parsePreferences(candidate.expense, 'expense'),
    income: parsePreferences(candidate.income, 'income'),
  }
}

function toWireEntry(entry: CategoryEntry): CategoryEntryWire {
  const wire: CategoryEntryWire = { name: entry.name, visible: entry.visible }
  if (entry.parentName !== undefined) wire.parent_name = entry.parentName
  if (entry.icon !== undefined) wire.icon = entry.icon
  if (entry.defaultName !== undefined) wire.default_name = entry.defaultName
  if (entry.previousNames !== undefined && entry.previousNames.length > 0) {
    wire.previous_names = [...entry.previousNames]
  }
  return wire
}

function toWirePreferences(
  preferences: CategoryPreferences,
): CategoryPreferencesWire {
  return {
    entries: preferences.entries.map(toWireEntry),
    deleted_defaults: [...preferences.deletedDefaults],
  }
}

export async function getCategories(): Promise<CategorySnapshot> {
  const payload = await request<CategorySnapshotWire>('/api/categories/')
  return parseSnapshot(payload)
}

/**
 * Atomically replaces both kinds. `baseRevision` must be the revision captured
 * when the draft was opened (0 for the household's first save); the server
 * rejects a stale value with 409 instead of overwriting another member.
 */
export async function saveCategories(
  baseRevision: number,
  expense: CategoryPreferences,
  income: CategoryPreferences,
): Promise<CategorySnapshot> {
  const body: CategorySavePayloadWire = {
    base_revision: baseRevision,
    expense: toWirePreferences(expense),
    income: toWirePreferences(income),
  }
  const payload = await request<CategorySnapshotWire>('/api/categories/', {
    method: 'PUT',
    body,
  })
  return parseSnapshot(payload)
}
