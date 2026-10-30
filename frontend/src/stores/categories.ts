import { reactive, watch } from 'vue'
import {
  getCategories,
  saveCategories,
  type CategorySnapshot,
} from '../api/categories'
import { ApiError, getErrorMessage } from '../api/client'
import { authState } from './auth'
import {
  readLegacyCategorySnapshot,
  type LegacyCategorySnapshot,
} from '../utils/categoryMigration'
import {
  entryMatchesName,
  materializeCategoryEntries,
  normalizeCategoryName,
  prepareCategoryPreferences,
  type CategoryEntry,
  type CategoryKind,
  type CategoryPreferences,
} from '../utils/categoryPreferences'

export type { CategoryEntry, CategoryKind } from '../utils/categoryPreferences'

export interface CategoryState {
  /** Identity the in-memory snapshot belongs to; null while signed out. */
  householdId: number | null
  userId: number | null
  initialized: boolean
  /** True only after a successful load for the current identity. */
  loaded: boolean
  loading: boolean
  error: string | null
  revision: number
  entries: Record<CategoryKind, CategoryEntry[]>
  deletedDefaults: Record<CategoryKind, string[]>
}

function defaultEntries(): Record<CategoryKind, CategoryEntry[]> {
  return {
    expense: materializeCategoryEntries('expense', null),
    income: materializeCategoryEntries('income', null),
  }
}

/**
 * One reactive, household-scoped snapshot. Synchronous consumers read this
 * state; only `ensureCategories` and `saveCategorySnapshot` talk to the API.
 */
export const categoryState = reactive<CategoryState>({
  householdId: null,
  userId: null,
  initialized: false,
  loaded: false,
  loading: false,
  error: null,
  revision: 0,
  entries: defaultEntries(),
  deletedDefaults: { expense: [], income: [] },
})

let identityEpoch = 0
let stateRequest = 0
let loadRequest = 0
let normalLoad: Promise<void> | null = null
let forceLoad: Promise<void> | null = null

function currentIdentity(): { householdId: number; userId: number } | null {
  const householdId = authState.household?.id
  const userId = authState.user?.id
  if (typeof householdId !== 'number' || typeof userId !== 'number') return null
  return { householdId, userId }
}

function identityKey(): string | null {
  const identity = currentIdentity()
  return identity ? `${identity.householdId}:${identity.userId}` : null
}

function isActive(epoch: number, key: string): boolean {
  return epoch === identityEpoch && key === identityKey()
}

function syncIdentityFields(): void {
  categoryState.householdId = authState.household?.id ?? null
  categoryState.userId = authState.user?.id ?? null
}

/**
 * Drops every trace of the previous identity. Callers may still hold old
 * promises; the epoch guard makes sure their responses are ignored.
 */
export function resetCategoryState(): void {
  identityEpoch += 1
  normalLoad = null
  forceLoad = null
  syncIdentityFields()
  categoryState.initialized = false
  categoryState.loaded = false
  categoryState.loading = false
  categoryState.error = null
  categoryState.revision = 0
  categoryState.entries = defaultEntries()
  categoryState.deletedDefaults = { expense: [], income: [] }
}

function applyInitializedSnapshot(snapshot: CategorySnapshot): void {
  const { expense, income } = snapshot
  if (!expense || !income) {
    throw new Error('分类设置数据格式异常，请刷新后重试。')
  }
  categoryState.entries = {
    expense: materializeCategoryEntries('expense', expense),
    income: materializeCategoryEntries('income', income),
  }
  categoryState.deletedDefaults = {
    expense: [...expense.deletedDefaults],
    income: [...income.deletedDefaults],
  }
  syncIdentityFields()
  categoryState.initialized = true
  categoryState.revision = snapshot.revision
  categoryState.loaded = true
  categoryState.error = null
}

function applyUninitializedSnapshot(): void {
  syncIdentityFields()
  categoryState.entries = defaultEntries()
  categoryState.deletedDefaults = { expense: [], income: [] }
  categoryState.initialized = false
  categoryState.revision = 0
  categoryState.loaded = true
  categoryState.error = null
}

/**
 * Applies a server snapshot only when it cannot roll the same identity back.
 * Revisions increase on every server write, so an older GET/PUT response is
 * ignored; an uninitialized response never replaces an initialized snapshot.
 */
function applySnapshotSafely(snapshot: CategorySnapshot): void {
  if (!snapshot.initialized) {
    if (categoryState.initialized) return
    applyUninitializedSnapshot()
    return
  }
  if (
    categoryState.initialized &&
    snapshot.revision < categoryState.revision
  ) {
    return
  }
  applyInitializedSnapshot(snapshot)
}

function migrationPayload(
  legacy: LegacyCategorySnapshot,
): { expense: CategoryPreferences; income: CategoryPreferences } {
  return {
    expense: prepareCategoryPreferences(
      'expense',
      materializeCategoryEntries('expense', legacy.expense),
    ),
    income: prepareCategoryPreferences(
      'income',
      materializeCategoryEntries('income', legacy.income),
    ),
  }
}

async function loadOnce(epoch: number, key: string): Promise<void> {
  if (!isActive(epoch, key)) return
  const requestId = ++stateRequest
  const loadId = ++loadRequest
  categoryState.loading = true
  categoryState.error = null
  let migrating = false
  try {
    const snapshot = await getCategories()
    if (!isActive(epoch, key)) return
    if (snapshot.initialized) {
      applySnapshotSafely(snapshot)
      return
    }
    // A concurrent save may have initialized the household while this GET was
    // in flight; an uninitialized response must not undo that snapshot.
    if (categoryState.initialized) return

    const identity = currentIdentity()
    if (!identity) return
    const legacy = readLegacyCategorySnapshot(
      identity.householdId,
      identity.userId,
    )
    if (!legacy) {
      // No usable legacy income dictionary exists. Defaults stay explicit and
      // in memory until the first actual save creates the shared snapshot.
      applySnapshotSafely(snapshot)
      return
    }

    // One-time adoption: the first account with a usable legacy income
    // dictionary establishes income; expense always starts from the built-in
    // rebuilt tree. Old keys stay as backups and are never written again.
    migrating = true
    const payload = migrationPayload(legacy)
    let migrated: CategorySnapshot
    try {
      migrated = await saveCategories(0, payload.expense, payload.income)
    } catch (error) {
      if (!isActive(epoch, key)) return
      if (error instanceof ApiError && error.status === 409) {
        // Another member won the initialization race; adopt the winner.
        const winner = await getCategories()
        if (!isActive(epoch, key)) return
        applySnapshotSafely(winner)
        return
      }
      throw error
    }
    if (!isActive(epoch, key)) return
    applySnapshotSafely(migrated)
  } catch (error) {
    if (!isActive(epoch, key)) return
    // A newer load or save owns the current state; an older refresh failure
    // must not clear loaded/error under it.
    if (requestId !== stateRequest) return
    const message = getErrorMessage(error)
    categoryState.error = migrating
      ? `本机分类迁移失败：${message}`
      : message
    categoryState.loaded = false
  } finally {
    if (isActive(epoch, key) && loadId === loadRequest) {
      categoryState.loading = false
    }
  }
}

/**
 * Loads the household snapshot for the current identity. Concurrent callers
 * share one request; `force` re-reads before opening a dialog so remote edits
 * are visible. Never falls back to defaults on failure: the error is exposed
 * and `loaded` stays false so saves cannot overwrite unseen settings.
 */
export function ensureCategories(force = false): Promise<void> {
  const identity = currentIdentity()
  if (!identity) return Promise.resolve()
  const key = `${identity.householdId}:${identity.userId}`
  if (
    !force &&
    categoryState.loaded &&
    categoryState.householdId === identity.householdId &&
    categoryState.userId === identity.userId
  ) {
    return Promise.resolve()
  }
  const epoch = identityEpoch
  if (force) {
    if (forceLoad) return forceLoad
    const previous = normalLoad?.catch(() => undefined) ?? Promise.resolve()
    let tracked: Promise<void>
    tracked = previous
      .then(() => loadOnce(epoch, key))
      .finally(() => {
        if (forceLoad === tracked) forceLoad = null
      })
    forceLoad = tracked
    return tracked
  }

  if (normalLoad) return normalLoad
  if (forceLoad) return forceLoad
  let tracked: Promise<void>
  tracked = loadOnce(epoch, key).finally(() => {
    if (normalLoad === tracked) normalLoad = null
  })
  normalLoad = tracked
  return tracked
}

/**
 * Reactive snapshot for the current household. Callers treat it as read-only;
 * settings drafts always copy entries before editing.
 */
export function readCategoryEntries(kind: CategoryKind): CategoryEntry[] {
  // Expose defaults only after the server explicitly reports no saved set.
  // Unloaded/failed identities must not offer unverified category choices.
  if (
    !categoryState.loaded ||
    categoryState.userId !== (authState.user?.id ?? null) ||
    categoryState.householdId !== (authState.household?.id ?? null)
  ) {
    return []
  }
  return categoryState.entries[kind]
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
 * Atomically saves both kinds with the revision captured when the draft was
 * opened. A 409 from the server is propagated untouched: the draft remains in
 * the dialog and nothing is retried or overwritten automatically.
 */
export async function saveCategorySnapshot(
  drafts: Readonly<Record<CategoryKind, readonly CategoryEntry[]>>,
  baseRevision: number,
): Promise<CategorySnapshot> {
  const key = identityKey()
  if (key === null) {
    throw new Error('请先登录，再保存分类设置')
  }
  if (!categoryState.loaded) {
    throw new Error(
      categoryState.error ?? '分类设置尚未加载完成，请关闭后重新打开',
    )
  }
  if (!Number.isInteger(baseRevision) || baseRevision < 0) {
    throw new Error('分类版本无效，请重新打开分类管理后再保存')
  }
  const expense = prepareCategoryPreferences('expense', drafts.expense)
  const income = prepareCategoryPreferences('income', drafts.income)
  const epoch = identityEpoch
  // A save owns the state from this point: an older delayed GET failure must
  // not clear loaded/error, and its older revision must not be applied later.
  stateRequest += 1
  const snapshot = await saveCategories(baseRevision, expense, income)
  if (isActive(epoch, key)) applySnapshotSafely(snapshot)
  return snapshot
}

// Identity changes (login, logout, 401, household switch) clear the snapshot
// immediately; the epoch guard discards responses that belonged to the old
// account even if they arrive later.
watch(
  identityKey,
  (key) => {
    resetCategoryState()
    if (key !== null) void ensureCategories()
  },
  { immediate: true },
)
