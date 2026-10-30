<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  ref,
  watch,
} from 'vue'
import {
  ArrowDown,
  Back,
  Close,
  Delete,
  EditPen,
  MoreFilled,
  Plus,
  Right,
  Switch as SwitchIcon,
  Tickets,
  Top,
  WarningFilled,
} from '@element-plus/icons-vue'
import { authState } from '../stores/auth'
import {
  categoryBillNames,
  categoryState,
  ensureCategories,
  isCategoryVisible,
  readCategoryEntries,
  saveCategorySnapshot,
  type CategoryEntry,
  type CategoryKind,
} from '../stores/categories'
import { categoryIcon } from '../utils/categoryIcons'
import CategoryEditor from './CategoryEditor.vue'

const props = defineProps<{
  modelValue: boolean
  kind: CategoryKind
}>()

const emit = defineEmits<{
  (event: 'update:modelValue', value: boolean): void
  (event: 'saved', selectedCategory?: string): void
  (event: 'view-bills', payload: { kind: CategoryKind; categories: string[] }): void
}>()

const KINDS: CategoryKind[] = ['expense', 'income']
const LONG_PRESS_DELAY = 420
const DRAG_MOVE_THRESHOLD = 8
const MOUSE_DRAG_THRESHOLD = 6
const DRAG_EDGE_SIZE = 64
const DRAG_MAX_SCROLL_SPEED = 18
const KEY_DELTAS: Record<string, number> = {
  ArrowUp: -1,
  ArrowDown: 1,
  ArrowLeft: -1,
  ArrowRight: 1,
}

interface EditorState {
  kind: CategoryKind
  mode: 'create' | 'edit'
  parentName?: string
  entryName?: string
}

interface ActionTarget {
  kind: CategoryKind
  name: string
}

type SheetMode = 'actions' | 'move' | 'delete'

interface LayoutSlot {
  left: number
  top: number
  width: number
  height: number
}

interface DragSession {
  kind: CategoryKind
  name: string
  parentName?: string
  topVisible: boolean
  pointerId: number
  pointerType: string
  startX: number
  startY: number
  lastX: number
  lastY: number
  active: boolean
  snapshot: CategoryEntry[]
  slots: LayoutSlot[]
  captureTarget: HTMLElement
  longPressTimer: ReturnType<typeof setTimeout> | null
  rafId: number | null
}

const drafts = ref<Record<CategoryKind, CategoryEntry[]>>({
  expense: [],
  income: [],
})
const baselineSignatures = ref<Record<CategoryKind, string>>({
  expense: '',
  income: '',
})
const addedNames = ref<Record<CategoryKind, string[]>>({ expense: [], income: [] })
const activeKind = ref<CategoryKind>(props.kind)
const errorMessage = ref('')
const loading = ref(false)
const saving = ref(false)
const busy = computed(
  () => saving.value || loading.value || !categoryState.loaded,
)
const liveMessage = ref('')
const editorState = ref<EditorState | null>(null)
const expandedKeys = ref<Set<string>>(new Set())
const actionTarget = ref<ActionTarget | null>(null)
const sheetMode = ref<SheetMode>('actions')
const dragName = ref<string | null>(null)
const dragBusy = ref(false)
const scrollRef = ref<HTMLElement | null>(null)
const sheetRef = ref<HTMLElement | null>(null)
const actionReturnFocus = ref<HTMLElement | null>(null)

let openedScope: string | null = null
let baselineRevision = 0
let loadToken = 0
let dragSession: DragSession | null = null
let suppressClick = false
let suppressClickTimer: ReturnType<typeof setTimeout> | null = null

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const kindLabel = (kind: CategoryKind): string => (kind === 'income' ? '收入' : '支出')
const activeLabel = computed(() => kindLabel(activeKind.value))
const dialogTitle = computed(() => editorState.value ? editorHeading.value : '分类管理')

function currentScope(): string | null {
  const userId = authState.user?.id
  const householdId = authState.household?.id
  if (userId === undefined || householdId === undefined) return null
  return `${householdId}:${userId}`
}

function copyEntry(entry: CategoryEntry): CategoryEntry {
  const copy: CategoryEntry = { ...entry }
  if (entry.previousNames) copy.previousNames = [...entry.previousNames]
  return copy
}

function copyEntries(entries: readonly CategoryEntry[]): CategoryEntry[] {
  return entries.map(copyEntry)
}

function entrySignature(entry: CategoryEntry): string {
  return JSON.stringify([
    entry.name,
    entry.visible,
    entry.parentName ?? null,
    entry.icon ?? null,
    entry.defaultName ?? null,
    entry.previousNames ?? null,
  ])
}

function entriesSignature(entries: readonly CategoryEntry[]): string {
  return entries.map(entrySignature).join('\n')
}

function setDraft(kind: CategoryKind, entries: CategoryEntry[]): void {
  drafts.value = { ...drafts.value, [kind]: entries }
}

function topLevelEntries(kind: CategoryKind): CategoryEntry[] {
  return drafts.value[kind].filter((entry) => entry.parentName === undefined)
}

function childrenOf(kind: CategoryKind, parentName: string): CategoryEntry[] {
  return drafts.value[kind].filter((entry) => entry.parentName === parentName)
}

function entryByName(kind: CategoryKind, name: string): CategoryEntry | undefined {
  return drafts.value[kind].find((entry) => entry.name === name)
}

function expansionKey(kind: CategoryKind, name: string): string {
  return `${kind}\u0000${name}`
}

function isExpanded(parentName: string): boolean {
  return expandedKeys.value.has(expansionKey(activeKind.value, parentName))
}

function toggleExpanded(parentName: string): void {
  if (saving.value) return
  const key = expansionKey(activeKind.value, parentName)
  const next = new Set(expandedKeys.value)
  if (next.has(key)) next.delete(key)
  else next.add(key)
  expandedKeys.value = next
}

const activeEntries = computed(() => drafts.value[activeKind.value])
const visibleParents = computed(() =>
  topLevelEntries(activeKind.value).filter((entry) => entry.visible),
)
const hiddenParents = computed(() =>
  topLevelEntries(activeKind.value).filter((entry) => !entry.visible),
)
const activeEntryCount = computed(() => activeEntries.value.length)
const activeChildCount = computed(
  () => activeEntries.value.length - topLevelEntries(activeKind.value).length,
)
const dirtyKinds = computed<CategoryKind[]>(() =>
  KINDS.filter(
    (kind) =>
      entriesSignature(drafts.value[kind]) !== baselineSignatures.value[kind],
  ),
)

const editorHeading = computed(() => {
  const state = editorState.value
  if (!state) return ''
  if (state.mode === 'edit') return `修改${kindLabel(state.kind)}分类`
  if (state.parentName) return `添加子分类 · ${state.parentName}`
  return `新增${kindLabel(state.kind)}一级分类`
})
const editorKind = computed<CategoryKind>(
  () => editorState.value?.kind ?? activeKind.value,
)
const editorEntry = computed<CategoryEntry | undefined>(() => {
  const state = editorState.value
  if (!state || state.mode !== 'edit' || !state.entryName) return undefined
  return entryByName(state.kind, state.entryName)
})
const editorNames = computed(() => {
  const state = editorState.value
  return state ? drafts.value[state.kind].map((entry) => entry.name) : []
})

const actionEntry = computed<CategoryEntry | undefined>(() => {
  const target = actionTarget.value
  return target ? entryByName(target.kind, target.name) : undefined
})
const actionIsChild = computed(() => actionEntry.value?.parentName !== undefined)
const actionParent = computed<CategoryEntry | undefined>(() => {
  const entry = actionEntry.value
  if (!entry?.parentName) return undefined
  return entryByName(activeKind.value, entry.parentName)
})
const actionChildCount = computed(() => {
  const entry = actionEntry.value
  if (!entry || entry.parentName) return 0
  return childrenOf(activeKind.value, entry.name).length
})
const otherParents = computed(() =>
  topLevelEntries(activeKind.value).filter(
    (entry) => entry.name !== actionEntry.value?.parentName && entry.name !== actionEntry.value?.name,
  ),
)
const sheetHeading = computed(() => {
  const entry = actionEntry.value
  if (!entry) return ''
  if (entry.parentName) return `子分类 · ${entry.parentName}`
  return '一级分类'
})
const deleteWarning = computed(() => {
  const entry = actionEntry.value
  if (!entry) return ''
  const count = entry.parentName ? 0 : actionChildCount.value
  const childText =
    count > 0
      ? `其下 ${count} 个子分类会提升为一级分类。`
      : ''
  return `永久删除「${entry.name}」？${childText}历史账目会保留，不会删除或改写。`
})
const parentSections = computed(() => {
  const sections: {
    key: string
    title: string
    scope: string
    parents: CategoryEntry[]
    disabled: boolean
  }[] = []
  if (visibleParents.value.length > 0) {
    sections.push({
      key: 'visible',
      title: '一级分类',
      scope: 'top:visible',
      parents: visibleParents.value,
      disabled: false,
    })
  }
  if (hiddenParents.value.length > 0) {
    sections.push({
      key: 'hidden',
      title: `已停用 ${hiddenParents.value.length} 个一级分类`,
      scope: 'top:hidden',
      parents: hiddenParents.value,
      disabled: true,
    })
  }
  return sections
})

function clearSuppressClickTimer(): void {
  if (suppressClickTimer === null) return
  clearTimeout(suppressClickTimer)
  suppressClickTimer = null
}

function consumeSuppressedClick(): boolean {
  if (!suppressClick) return false
  suppressClick = false
  return true
}

function scheduleSuppressReset(): void {
  clearSuppressClickTimer()
  suppressClick = true
  suppressClickTimer = setTimeout(() => {
    suppressClick = false
    suppressClickTimer = null
  }, 0)
}

async function loadDrafts(): Promise<void> {
  const token = ++loadToken
  const scope = currentScope()
  openedScope = scope
  closeEditor()
  closeActionSheet()
  endDrag(false, false)
  drafts.value = { expense: [], income: [] }
  baselineSignatures.value = { expense: '', income: '' }
  addedNames.value = { expense: [], income: [] }
  activeKind.value = props.kind
  expandedKeys.value = new Set()
  errorMessage.value = ''
  liveMessage.value = ''
  saving.value = false
  sheetMode.value = 'actions'
  baselineRevision = 0
  if (scope === null) {
    loading.value = false
    errorMessage.value = '请先登录，再管理分类设置'
    return
  }
  loading.value = true
  try {
    await ensureCategories(true)
  } catch (error) {
    if (token !== loadToken) return
    loading.value = false
    errorMessage.value = errorMessageFrom(error)
    return
  }
  if (token !== loadToken || !props.modelValue || currentScope() !== scope) {
    return
  }
  if (!categoryState.loaded) {
    loading.value = false
    errorMessage.value = categoryState.error ?? '分类设置加载失败，请稍后重试'
    return
  }
  const expense = copyEntries(readCategoryEntries('expense'))
  const income = copyEntries(readCategoryEntries('income'))
  drafts.value = { expense, income }
  baselineSignatures.value = {
    expense: entriesSignature(expense),
    income: entriesSignature(income),
  }
  baselineRevision = categoryState.revision
  loading.value = false
}

function clearTransientState(): void {
  loadToken += 1
  closeEditor()
  closeActionSheet()
  endDrag(false, false)
  drafts.value = { expense: [], income: [] }
  baselineSignatures.value = { expense: '', income: '' }
  addedNames.value = { expense: [], income: [] }
  expandedKeys.value = new Set()
  errorMessage.value = ''
  liveMessage.value = ''
  loading.value = false
  saving.value = false
  baselineRevision = 0
  sheetMode.value = 'actions'
  openedScope = null
}

watch(
  () => props.modelValue,
  (open) => {
    if (open) void loadDrafts()
    else clearTransientState()
  },
  { immediate: true },
)

watch([() => authState.user?.id, () => authState.household?.id], () => {
  if (!props.modelValue) return
  if (currentScope() !== openedScope) {
    clearTransientState()
    emit('update:modelValue', false)
  }
})

onBeforeUnmount(() => {
  loadToken += 1
  endDrag(false, false)
  closeActionSheet()
  clearSuppressClickTimer()
})

function closeEditor(): void {
  editorState.value = null
}

function openCreateParent(): void {
  if (saving.value || dragBusy.value) return
  endDrag(false, true)
  closeActionSheet()
  errorMessage.value = ''
  editorState.value = { kind: activeKind.value, mode: 'create' }
  liveMessage.value = `已打开新增${activeLabel.value}一级分类表单`
}

function openCreateChild(parentName: string): void {
  if (saving.value || dragBusy.value) return
  const parent = entryByName(activeKind.value, parentName)
  if (!parent || parent.parentName) {
    errorMessage.value = '上级分类不存在，请刷新后重试'
    return
  }
  endDrag(false, true)
  closeActionSheet()
  errorMessage.value = ''
  editorState.value = {
    kind: activeKind.value,
    mode: 'create',
    parentName,
  }
  liveMessage.value = `已打开为「${parentName}」添加子分类的表单`
}

function openEdit(entry: CategoryEntry): void {
  if (saving.value || dragBusy.value) return
  closeActionSheet()
  errorMessage.value = ''
  editorState.value = {
    kind: activeKind.value,
    mode: 'edit',
    entryName: entry.name,
  }
  liveMessage.value = `已打开修改分类${entry.name}`
}

function editActionTarget(): void {
  const entry = actionEntry.value
  if (entry) openEdit(entry)
}

function cancelEditor(): void {
  if (saving.value) return
  closeEditor()
  errorMessage.value = ''
  liveMessage.value = '已取消编辑，分类草稿未改变'
}

function rejectApply(message: string): void {
  errorMessage.value = message
}

function applyEditorEntry(entered: CategoryEntry): void {
  const state = editorState.value
  if (!state || saving.value) return
  const entries = drafts.value[state.kind]
  const newName = entered.name
  if (entries.some((entry) => entry.name === newName && entry.name !== state.entryName)) {
    rejectApply('该分类已存在（包括已停用分类），请换个名称')
    return
  }

  if (state.mode === 'create') {
    const created: CategoryEntry = { name: newName, visible: true }
    if (state.parentName !== undefined) created.parentName = state.parentName
    if (entered.icon !== undefined) created.icon = entered.icon
    const next = [...entries]
    if (state.parentName === undefined) {
      next.push(created)
    } else {
      const childIndexes = next
        .map((entry, index) => (entry.parentName === state.parentName ? index : -1))
        .filter((index) => index >= 0)
      const parentIndex = next.findIndex((entry) => entry.name === state.parentName)
      const insertAt =
        childIndexes.length > 0
          ? childIndexes[childIndexes.length - 1]! + 1
          : parentIndex + 1
      next.splice(insertAt, 0, created)
      expandedKeys.value = new Set([
        ...expandedKeys.value,
        expansionKey(state.kind, state.parentName),
      ])
    }
    setDraft(state.kind, next)
    addedNames.value = {
      ...addedNames.value,
      [state.kind]: [...addedNames.value[state.kind], newName],
    }
    errorMessage.value = ''
    closeEditor()
    liveMessage.value = `已添加${
      state.parentName ? `子分类${newName}` : `一级分类${newName}`
    }，保存设置后生效`
    void nextTick(() => revealCard(newName))
    return
  }

  const index = entries.findIndex((entry) => entry.name === state.entryName)
  if (index < 0) {
    rejectApply('分类已不存在，请返回列表后重试')
    return
  }
  const current = entries[index]!
  const updated = copyEntry(current)
  updated.name = newName
  if (entered.icon !== undefined) updated.icon = entered.icon
  if (current.name !== newName) {
    const previous = new Set(current.previousNames ?? [])
    previous.delete(newName)
    previous.add(current.name)
    updated.previousNames = [...previous]
  }
  const next = [...entries]
  next[index] = updated
  if (!current.parentName) {
    for (const entry of next) {
      if (entry.parentName === current.name) entry.parentName = newName
    }
    if (expandedKeys.value.has(expansionKey(state.kind, current.name))) {
      const keys = new Set(expandedKeys.value)
      keys.delete(expansionKey(state.kind, current.name))
      keys.add(expansionKey(state.kind, newName))
      expandedKeys.value = keys
    }
  }
  if (current.name !== newName) {
    addedNames.value = {
      ...addedNames.value,
      [state.kind]: addedNames.value[state.kind].map((name) =>
        name === current.name ? newName : name,
      ),
    }
  }
  setDraft(state.kind, next)
  errorMessage.value = ''
  closeEditor()
  liveMessage.value = `已修改分类${current.name}${
    current.name === newName ? '' : `为${newName}`
  }，保存设置后生效`
  void nextTick(() => revealCard(newName))
}

function revealCard(name: string): void {
  const scroll = scrollRef.value
  if (!scroll) return
  const card = Array.from(
    scroll.querySelectorAll<HTMLElement>('[data-category-card-name]'),
  ).find((element) => element.dataset.categoryCardName === name)
  if (!card) return
  const scrollRect = scroll.getBoundingClientRect()
  const cardRect = card.getBoundingClientRect()
  if (cardRect.top < scrollRect.top) {
    scroll.scrollTop -= scrollRect.top - cardRect.top + 8
  } else if (cardRect.bottom > scrollRect.bottom) {
    scroll.scrollTop += cardRect.bottom - scrollRect.bottom + 8
  }
}

function switchKind(next: CategoryKind): void {
  if (next === activeKind.value || saving.value || editorState.value) return
  if (actionTarget.value) return
  endDrag(false, true)
  closeActionSheet()
  errorMessage.value = ''
  activeKind.value = next
  liveMessage.value = `已切换到${kindLabel(next)}分类，未保存的修改仍保留在草稿中`
}

function openActionSheet(entry: CategoryEntry): void {
  if (saving.value || dragBusy.value || editorState.value) return
  if (typeof document !== 'undefined' && document.activeElement instanceof HTMLElement) {
    actionReturnFocus.value = document.activeElement
  } else {
    actionReturnFocus.value = null
  }
  actionTarget.value = { kind: activeKind.value, name: entry.name }
  sheetMode.value = 'actions'
  void nextTick(() => {
    sheetRef.value
      ?.querySelector<HTMLElement>('[data-sheet-autofocus]')
      ?.focus()
  })
}

function closeActionSheet(): void {
  const previous = actionReturnFocus.value
  actionTarget.value = null
  sheetMode.value = 'actions'
  actionReturnFocus.value = null
  if (previous) {
    void nextTick(() => {
      if (previous.isConnected) previous.focus()
    })
  }
}

function focusSheetAction(): void {
  void nextTick(() => {
    sheetRef.value
      ?.querySelector<HTMLElement>('[data-sheet-autofocus]')
      ?.focus()
  })
}

function backToActions(): void {
  sheetMode.value = 'actions'
  focusSheetAction()
}

function toggleActionVisibility(): void {
  const entry = actionEntry.value
  if (!entry || saving.value) return
  const kind = activeKind.value
  const next = copyEntries(drafts.value[kind])
  const target = next.find((candidate) => candidate.name === entry.name)
  if (!target) return
  target.visible = !target.visible
  setDraft(kind, next)
  errorMessage.value = ''
  if (target.visible) {
    liveMessage.value = `已启用分类${entry.name}`
  } else {
    liveMessage.value = `已停用分类${entry.name}，历史账目不会删除`
    if (!next.some((candidate) => !candidate.parentName && candidate.visible)) {
      errorMessage.value = `${kindLabel(kind)}分类请至少保留一个显示的一级分类`
    }
  }
  closeActionSheet()
}

function promoteAction(): void {
  const entry = actionEntry.value
  if (!entry?.parentName || saving.value) return
  const kind = activeKind.value
  const next = [...drafts.value[kind]]
  const index = next.findIndex((candidate) => candidate.name === entry.name)
  if (index < 0) return
  const [moved] = next.splice(index, 1)
  if (!moved) return
  delete moved.parentName
  next.push(moved)
  setDraft(kind, next)
  errorMessage.value = ''
  closeActionSheet()
  liveMessage.value = `已将子分类${entry.name}提升为一级分类，保存设置后生效`
  void nextTick(() => revealCard(entry.name))
}

function openMoveSheet(): void {
  if (saving.value) return
  sheetMode.value = 'move'
  void nextTick(() => {
    sheetRef.value
      ?.querySelector<HTMLElement>('[data-sheet-autofocus]')
      ?.focus()
  })
}

function moveActionTo(parentName: string): void {
  const entry = actionEntry.value
  if (!entry || saving.value || childrenOf(activeKind.value, entry.name).length > 0) return
  const kind = activeKind.value
  const parent = entryByName(kind, parentName)
  if (!parent || parent.parentName || parent.name === entry.name) {
    errorMessage.value = '目标一级分类不存在，请返回后重试'
    closeActionSheet()
    return
  }
  const next = copyEntries(drafts.value[kind])
  const target = next.find((candidate) => candidate.name === entry.name)
  if (!target) return
  target.parentName = parentName
  setDraft(kind, next)
  errorMessage.value = ''
  closeActionSheet()
  liveMessage.value = `已将子分类${entry.name}移至一级分类${parentName}，保存设置后生效`
  expandedKeys.value = new Set([
    ...expandedKeys.value,
    expansionKey(kind, parentName),
  ])
  void nextTick(() => revealCard(parentName))
}

function beginDelete(): void {
  if (saving.value) return
  sheetMode.value = 'delete'
  void nextTick(() => {
    sheetRef.value
      ?.querySelector<HTMLElement>('[data-sheet-autofocus]')
      ?.focus()
  })
}

function confirmDelete(): void {
  const entry = actionEntry.value
  if (!entry || saving.value) return
  const kind = activeKind.value
  const entries = drafts.value[kind]
  const index = entries.findIndex((candidate) => candidate.name === entry.name)
  if (index < 0) return
  let promoted = 0
  let next: CategoryEntry[]
  if (entry.parentName === undefined) {
    const children = entries.filter((candidate) => candidate.parentName === entry.name)
    promoted = children.length
    const remaining = entries.filter(
      (candidate) =>
        candidate.name !== entry.name && candidate.parentName !== entry.name,
    )
    const promotedEntries = children.map((child) => {
      const copy = copyEntry(child)
      delete copy.parentName
      return copy
    })
    const insertAt = Math.min(index, remaining.length)
    remaining.splice(insertAt, 0, ...promotedEntries)
    next = remaining
    const key = expansionKey(kind, entry.name)
    if (expandedKeys.value.has(key)) {
      const keys = new Set(expandedKeys.value)
      keys.delete(key)
      expandedKeys.value = keys
    }
  } else {
    next = entries.filter((candidate) => candidate.name !== entry.name)
  }
  setDraft(kind, next)
  addedNames.value = {
    ...addedNames.value,
    [kind]: addedNames.value[kind].filter((name) => name !== entry.name),
  }
  if (!next.some((candidate) => candidate.parentName === undefined && candidate.visible)) {
    errorMessage.value = `${kindLabel(kind)}分类请至少保留一个显示的一级分类`
  } else {
    errorMessage.value = ''
  }
  closeActionSheet()
  liveMessage.value = promoted
    ? `已永久删除一级分类${entry.name}，${promoted} 个子分类已提升为一级分类，历史账目保留`
    : `已永久删除分类${entry.name}，历史账目保留`
}

async function viewBillsAction(): Promise<void> {
  const entry = actionEntry.value
  if (!entry || busy.value) return
  const kind = activeKind.value
  if (currentScope() !== openedScope) {
    closeActionSheet()
    errorMessage.value = '账号或家庭已变更，分类设置未保存，请重新打开'
    return
  }
  const entries = drafts.value[kind]
  const categories = categoryBillNames(entries, entry.name)
  if (categories.length === 0) {
    closeActionSheet()
    errorMessage.value = '该分类没有可查询的名称'
    return
  }
  saving.value = true
  try {
    if (dirtyKinds.value.length > 0) await persistDrafts()
  } catch (error) {
    saving.value = false
    closeActionSheet()
    errorMessage.value = errorMessageFrom(error)
    return
  }
  saving.value = false
  closeActionSheet()
  emit('view-bills', { kind, categories })
  emit('update:modelValue', false)
}

function errorMessageFrom(error: unknown): string {
  if (error instanceof Error && error.message) return error.message
  return '保存分类设置失败，请稍后重试'
}

function validateDraftForSave(kind: CategoryKind): void {
  const entries = drafts.value[kind]
  const names = new Set<string>()
  const parents = new Set<string>()
  for (const entry of entries) {
    if (!entry.name.trim()) {
      throw new Error('分类名称不能为空，请修改后重试')
    }
    if (names.has(entry.name)) {
      throw new Error(`分类「${entry.name}」重复，请调整后重试`)
    }
    names.add(entry.name)
    if (entry.parentName === undefined) parents.add(entry.name)
  }
  for (const entry of entries) {
    if (entry.parentName === undefined) continue
    if (!parents.has(entry.parentName)) {
      throw new Error(`分类「${entry.name}」的上级分类不存在，请重新选择`)
    }
  }
  if (!entries.some((entry) => entry.parentName === undefined && entry.visible)) {
    throw new Error(`${kindLabel(kind)}分类请至少保留一个显示的一级分类`)
  }
}

async function persistDrafts(): Promise<void> {
  validateDraftForSave('expense')
  validateDraftForSave('income')
  if (currentScope() !== openedScope) {
    throw new Error('账号或家庭已变更，分类设置未保存，请重新打开')
  }
  const snapshot = await saveCategorySnapshot(
    { expense: drafts.value.expense, income: drafts.value.income },
    baselineRevision,
  )
  baselineRevision = snapshot.revision
  baselineSignatures.value = {
    expense: entriesSignature(drafts.value.expense),
    income: entriesSignature(drafts.value.income),
  }
}

function selectionForActiveKind(): string | undefined {
  if (activeKind.value !== props.kind) return undefined
  const kind = activeKind.value
  const entries = drafts.value[kind]
  const names = addedNames.value[kind]
  for (let index = names.length - 1; index >= 0; index -= 1) {
    const name = names[index]
    if (name === undefined) continue
    const entry = entryByName(kind, name)
    if (entry && isCategoryVisible(entry, entries)) return entry.name
  }
  return undefined
}

async function submit(): Promise<void> {
  if (busy.value) return
  if (dragSession) {
    endDrag(false, true)
    errorMessage.value = '请先完成拖动，再保存设置'
    return
  }
  errorMessage.value = ''
  if (currentScope() !== openedScope) {
    errorMessage.value = '账号或家庭已变更，分类设置未保存，请重新打开'
    return
  }
  const needsSave =
    dirtyKinds.value.length > 0 || baselineRevision === 0
  saving.value = true
  try {
    if (needsSave) await persistDrafts()
  } catch (error) {
    saving.value = false
    errorMessage.value = errorMessageFrom(error)
    return
  }
  const selected = selectionForActiveKind()
  saving.value = false
  emit('saved', selected)
  emit('update:modelValue', false)
}

function closeDialog(): void {
  if (saving.value) return
  closeEditor()
  closeActionSheet()
  endDrag(false, false)
  emit('update:modelValue', false)
}

function handleHeaderClose(): void {
  if (actionTarget.value) {
    closeActionSheet()
    return
  }
  if (editorState.value) {
    cancelEditor()
    return
  }
  closeDialog()
}

function handleDialogClosed(): void {
  closeEditor()
  closeActionSheet()
  endDrag(false, false)
}

function handleParentClick(entry: CategoryEntry): void {
  if (consumeSuppressedClick()) return
  if (saving.value) return
  toggleExpanded(entry.name)
}

function handleChildClick(entry: CategoryEntry): void {
  if (consumeSuppressedClick()) return
  openActionSheet(entry)
}

function handleSortKeydown(event: KeyboardEvent, entry: CategoryEntry): void {
  if (saving.value || editorState.value || actionTarget.value || dragSession) return
  if (event.altKey || event.ctrlKey || event.metaKey) return
  const delta = KEY_DELTAS[event.key]
  if (delta === undefined) return
  const target = event.target
  if (
    target instanceof HTMLElement &&
    target.closest('input, textarea, select')
  ) {
    return
  }
  event.preventDefault()
  moveEntryBy(entry, delta)
}

function siblingEntries(entry: CategoryEntry): CategoryEntry[] {
  const kind = activeKind.value
  if (entry.parentName !== undefined) return childrenOf(kind, entry.parentName)
  return topLevelEntries(kind).filter(
    (candidate) => candidate.visible === entry.visible,
  )
}

function moveWithinGroup(
  kind: CategoryKind,
  sourceName: string,
  targetName: string,
): boolean {
  const entries = drafts.value[kind]
  const from = entries.findIndex((entry) => entry.name === sourceName)
  const to = entries.findIndex((entry) => entry.name === targetName)
  if (from < 0 || to < 0 || from === to) return false
  const next = [...entries]
  const [entry] = next.splice(from, 1)
  if (!entry) return false
  next.splice(to, 0, entry)
  setDraft(kind, next)
  errorMessage.value = ''
  return true
}

function moveEntryBy(entry: CategoryEntry, delta: number): boolean {
  if (!Number.isInteger(delta) || delta === 0) return false
  const group = siblingEntries(entry)
  const index = group.findIndex((candidate) => candidate.name === entry.name)
  const target = group[index + delta]
  if (!target) return false
  if (!moveWithinGroup(activeKind.value, entry.name, target.name)) return false
  const nextGroup = siblingEntries(entry)
  const nextIndex = nextGroup.findIndex((candidate) => candidate.name === entry.name)
  const level = entry.parentName ? '子分类' : entry.visible ? '一级分类' : '已停用一级分类'
  liveMessage.value = `已将${level}${entry.name}移动到第 ${nextIndex + 1} 位`
  return true
}

function pointInRect(
  x: number,
  y: number,
  rect: Pick<DOMRect, 'left' | 'right' | 'top' | 'bottom'>,
): boolean {
  return x >= rect.left && x <= rect.right && y >= rect.top && y <= rect.bottom
}

function sessionScope(session: DragSession): string {
  if (session.parentName !== undefined) return `child:${session.parentName}`
  return session.topVisible ? 'top:visible' : 'top:hidden'
}

function findScopeContainer(scope: string): HTMLElement | null {
  const scroll = scrollRef.value
  if (!scroll) return null
  const candidates = scroll.querySelectorAll<HTMLElement>('[data-sort-scope]')
  for (const candidate of candidates) {
    if (candidate.dataset.sortScope === scope) return candidate
  }
  return null
}

function captureLayoutSlots(container: HTMLElement): LayoutSlot[] {
  // Read layout offsets before highlighting the dragged card. Unlike rects,
  // these ignore FLIP transforms, including an unfinished previous move.
  return Array.from(container.children)
    .filter((tile): tile is HTMLElement =>
      tile instanceof HTMLElement && tile.hasAttribute('data-category-card-name'),
    )
    .map((tile) => ({
      left: tile.offsetLeft,
      top: tile.offsetTop,
      width: tile.offsetWidth,
      height: tile.offsetHeight,
    }))
}

function applyDropTarget(x: number, y: number): void {
  const session = dragSession
  if (!session?.active) return
  if (session.slots.length === 0) return
  const container = findScopeContainer(sessionScope(session))
  if (!container) return
  const rect = container.getBoundingClientRect()
  const localX = x - rect.left - container.clientLeft + container.scrollLeft
  const localY = y - rect.top - container.clientTop + container.scrollTop
  let bestIndex = -1
  let bestDistance = Number.POSITIVE_INFINITY
  for (let index = 0; index < session.slots.length; index += 1) {
    const slot = session.slots[index]
    if (!slot) continue
    const dx = localX - Math.min(Math.max(localX, slot.left), slot.left + slot.width)
    const dy = localY - Math.min(Math.max(localY, slot.top), slot.top + slot.height)
    const distance = dx * dx + dy * dy
    if (distance < bestDistance) {
      bestDistance = distance
      bestIndex = index
    }
  }
  const entries = drafts.value[session.kind].filter((entry) =>
    session.parentName !== undefined
      ? entry.parentName === session.parentName
      : entry.parentName === undefined && entry.visible === session.topVisible,
  )
  const targetName = entries[bestIndex]?.name
  if (!targetName || targetName === session.name) return
  moveWithinGroup(session.kind, session.name, targetName)
}

function autoScrollFrame(): void {
  const session = dragSession
  if (!session?.active) return
  session.rafId = null
  const scroll = scrollRef.value
  if (scroll && scroll.scrollHeight > scroll.clientHeight) {
    const rect = scroll.getBoundingClientRect()
    if (
      rect.height > 0 &&
      session.lastX >= rect.left &&
      session.lastX <= rect.right
    ) {
      const edge = Math.min(DRAG_EDGE_SIZE, Math.max(24, rect.height / 3))
      let speed = 0
      if (session.lastY < rect.top + edge) {
        const ratio = Math.min(
          1,
          Math.max(0, (rect.top + edge - session.lastY) / edge),
        )
        speed = -Math.ceil(ratio * DRAG_MAX_SCROLL_SPEED)
      } else if (session.lastY > rect.bottom - edge) {
        const ratio = Math.min(
          1,
          Math.max(0, (session.lastY - (rect.bottom - edge)) / edge),
        )
        speed = Math.ceil(ratio * DRAG_MAX_SCROLL_SPEED)
      }
      if (speed !== 0) {
        const maxScroll = scroll.scrollHeight - scroll.clientHeight
        const next = Math.min(
          maxScroll,
          Math.max(0, scroll.scrollTop + speed),
        )
        if (next !== scroll.scrollTop) {
          scroll.scrollTop = next
          applyDropTarget(session.lastX, session.lastY)
        }
      }
    }
  }
  if (dragSession === session && session.active) {
    session.rafId = requestAnimationFrame(autoScrollFrame)
  }
}

function startDragAutoScroll(session: DragSession): void {
  if (session.rafId !== null || typeof requestAnimationFrame !== 'function') return
  session.rafId = requestAnimationFrame(autoScrollFrame)
}

function preventTouchScroll(event: TouchEvent): void {
  if (dragSession?.active && event.cancelable) event.preventDefault()
}

function activateDrag(session: DragSession): void {
  if (dragSession !== session || session.active) return
  const container = findScopeContainer(sessionScope(session))
  if (!container) return
  session.slots = captureLayoutSlots(container)
  session.active = true
  if (session.longPressTimer !== null) {
    clearTimeout(session.longPressTimer)
    session.longPressTimer = null
  }
  dragName.value = session.name
  liveMessage.value = `正在拖动分类${session.name}，松手放到同级新位置`
  try {
    if (typeof session.captureTarget.setPointerCapture === 'function') {
      session.captureTarget.setPointerCapture(session.pointerId)
    }
  } catch {
    // Window listeners keep the gesture usable without pointer capture.
  }
  session.captureTarget.addEventListener(
    'lostpointercapture',
    handleLostPointerCapture,
  )
  startDragAutoScroll(session)
}

function cleanupDragSession(session: DragSession): void {
  if (session.longPressTimer !== null) {
    clearTimeout(session.longPressTimer)
    session.longPressTimer = null
  }
  if (typeof window !== 'undefined') {
    window.removeEventListener('pointermove', handlePointerMove)
    window.removeEventListener('pointerup', handlePointerUp)
    window.removeEventListener('pointercancel', handlePointerCancel)
    window.removeEventListener('blur', handleWindowBlur)
    window.removeEventListener('touchmove', preventTouchScroll)
  }
  session.captureTarget.removeEventListener(
    'lostpointercapture',
    handleLostPointerCapture,
  )
  if (session.rafId !== null) {
    if (typeof cancelAnimationFrame === 'function') {
      cancelAnimationFrame(session.rafId)
    }
    session.rafId = null
  }
  try {
    if (typeof session.captureTarget.releasePointerCapture === 'function') {
      session.captureTarget.releasePointerCapture(session.pointerId)
    }
  } catch {
    // The pointer may already be released.
  }
}

function endDrag(commit: boolean, restore: boolean): void {
  const session = dragSession
  if (!session) {
    dragBusy.value = false
    dragName.value = null
    return
  }
  dragSession = null
  cleanupDragSession(session)
  dragName.value = null
  dragBusy.value = false
  if (session.active) {
    if (restore && !commit) {
      setDraft(session.kind, copyEntries(session.snapshot))
      liveMessage.value = '已取消本次拖动，顺序保持原样'
    } else {
      liveMessage.value = `已调整分类${session.name}的位置`
    }
    scheduleSuppressReset()
  }
}


function beginPointerDrag(event: PointerEvent, entry: CategoryEntry): void {
  if (saving.value || editorState.value || actionTarget.value) return
  if (dragSession) return
  if (event.pointerType === 'mouse' && event.button !== 0) return
  const captureTarget = scrollRef.value
  if (!captureTarget) return
  const session: DragSession = {
    kind: activeKind.value,
    name: entry.name,
    parentName: entry.parentName,
    topVisible: entry.visible,
    pointerId: event.pointerId,
    pointerType: event.pointerType,
    startX: event.clientX,
    startY: event.clientY,
    lastX: event.clientX,
    lastY: event.clientY,
    active: false,
    snapshot: copyEntries(drafts.value[activeKind.value]),
    slots: [],
    captureTarget,
    longPressTimer: null,
    rafId: null,
  }
  dragSession = session
  dragBusy.value = true
  session.longPressTimer = setTimeout(() => {
    if (dragSession === session) activateDrag(session)
  }, LONG_PRESS_DELAY)
  if (typeof window !== 'undefined') {
    window.addEventListener('pointermove', handlePointerMove, { passive: false })
    window.addEventListener('pointerup', handlePointerUp)
    window.addEventListener('pointercancel', handlePointerCancel)
    window.addEventListener('blur', handleWindowBlur)
    if (event.pointerType !== 'mouse') {
      window.addEventListener('touchmove', preventTouchScroll, { passive: false })
    }
  }
}

function handlePointerMove(event: PointerEvent): void {
  const session = dragSession
  if (!session || event.pointerId !== session.pointerId) return
  session.lastX = event.clientX
  session.lastY = event.clientY
  if (!session.active) {
    const dx = event.clientX - session.startX
    const dy = event.clientY - session.startY
    const distance = Math.sqrt(dx * dx + dy * dy)
    if (session.pointerType === 'mouse') {
      if (distance < MOUSE_DRAG_THRESHOLD) return
      activateDrag(session)
    } else if (distance > DRAG_MOVE_THRESHOLD) {
      endDrag(false, false)
      return
    }
  }
  if (!session.active) return
  if (event.cancelable) event.preventDefault()
  applyDropTarget(event.clientX, event.clientY)
}

function handlePointerUp(event: PointerEvent): void {
  const session = dragSession
  if (!session || event.pointerId !== session.pointerId) return
  if (event.pointerType === 'mouse' && event.button !== 0) return
  if (session.active && event.cancelable) event.preventDefault()
  const droppedInside = !session.active
    ? true
    : (() => {
        const scroll = scrollRef.value
        if (!scroll) return false
        return pointInRect(
          event.clientX,
          event.clientY,
          scroll.getBoundingClientRect(),
        )
      })()
  endDrag(droppedInside, true)
}

function handlePointerCancel(event: PointerEvent): void {
  const session = dragSession
  if (!session || event.pointerId !== session.pointerId) return
  endDrag(false, true)
}

function handleWindowBlur(): void {
  endDrag(false, true)
}

function handleLostPointerCapture(event: Event): void {
  const session = dragSession
  if (!session || event.target !== session.captureTarget) return
  endDrag(false, true)
}

function suppressContextMenu(event: Event): void {
  event.preventDefault()
}
</script>

<template>
  <el-dialog
    v-model="visible"
    :title="dialogTitle"
    class="ledger-dialog category-settings-dialog"
    :class="{ 'is-editing': editorState !== null }"
    fullscreen
    append-to-body
    :close-on-click-modal="!saving && !editorState && !actionTarget"
    :close-on-press-escape="!saving && !editorState && !actionTarget"
    :show-close="false"
    @closed="handleDialogClosed"
  >
    <template #header>
      <div class="category-settings-header">
        <button
          type="button"
          class="category-settings-header__close"
          :aria-label="editorState ? '返回分类管理' : '关闭分类设置'"
          :title="editorState ? '返回分类管理' : '关闭'"
          :disabled="saving"
          @click="handleHeaderClose"
        >
          <el-icon>
            <Back v-if="editorState" />
            <Close v-else />
          </el-icon>
        </button>
        <div class="category-settings-header__heading">
          <h2>{{ dialogTitle }}</h2>
          <p v-if="!editorState">
            分类由全家共享，保存后所有成员都会看到；长按卡片可拖动排序
          </p>
        </div>
        <button
          v-if="!editorState"
          type="button"
          class="category-settings-header__add"
          aria-label="新增一级分类"
          title="新增一级分类"
          :disabled="busy || dragBusy"
          @click="openCreateParent"
        >
          <el-icon aria-hidden="true"><Plus /></el-icon>
        </button>
      </div>
    </template>

    <div class="category-settings" :class="{ 'is-editing': editorState !== null }">
      <section
        v-if="editorState"
        class="category-editor-panel"
        aria-label="分类编辑表单"
      >
        <el-alert
          v-if="errorMessage"
          class="category-save-error"
          :title="errorMessage"
          type="error"
          :closable="false"
          show-icon
        />
        <CategoryEditor
          :kind="editorKind"
          :entry="editorEntry"
          :existing-names="editorNames"
          :disabled="saving"
          fullscreen
          @apply="applyEditorEntry"
          @cancel="cancelEditor"
        />
      </section>

      <template v-else>
        <div v-if="loading" class="category-load-state" role="status">
          正在加载家庭共享分类…
        </div>
        <section
          v-else-if="!categoryState.loaded"
          class="category-load-state is-error"
          aria-live="polite"
        >
          <p>分类设置尚未加载，暂时无法编辑。</p>
          <el-button type="primary" plain @click="loadDrafts">重新加载</el-button>
        </section>
        <template v-else>
        <div class="category-kind-tabs" role="group" aria-label="分类类型">
          <button
            v-for="kind in KINDS"
            :key="kind"
            type="button"
            class="category-kind-tab"
            :class="{ 'is-active': activeKind === kind }"
            :aria-pressed="activeKind === kind"
            :data-kind="kind"
            :disabled="saving || dragBusy"
            @click="switchKind(kind)"
          >
            {{ kindLabel(kind) }}
            <span class="category-kind-tab__count">
              {{ topLevelEntries(kind).length }}
            </span>
          </button>
        </div>

        <div
          ref="scrollRef"
          class="category-list-scroll"
        >
          <template v-for="section in parentSections" :key="section.key">
            <section
              class="category-section"
              :class="{ 'is-disabled-section': section.disabled }"
              :aria-label="section.title"
            >
              <header v-if="section.disabled" class="category-section__head">
                <h3>{{ section.title }}</h3>
                <span>展开后可恢复子分类</span>
              </header>
              <TransitionGroup
                tag="ul"
                name="category-sort"
                class="category-parent-list"
                :data-sort-scope="section.scope"
                role="list"
                :aria-label="section.disabled ? '已停用的一级分类' : '一级分类，可拖动排序'"
              >
                <li
                  v-for="parent in section.parents"
                  :key="parent.name"
                  class="category-parent"
                  :class="{
                    'is-disabled': !parent.visible,
                    'is-dragging': dragName === parent.name,
                  }"
                  :data-category-card-name="parent.name"
                  :data-category-kind="activeKind"
                >
                  <div class="category-parent__head">
                    <button
                      type="button"
                      class="category-parent__main"
                      :aria-expanded="isExpanded(parent.name)"
                      :aria-label="`${parent.name}，${
                        parent.visible ? '显示中' : '已停用'
                      }，${childrenOf(activeKind, parent.name).length} 个子分类，${
                        isExpanded(parent.name) ? '已展开' : '已收起'
                      }；点击${
                        isExpanded(parent.name) ? '收起' : '展开'
                      }子分类；使用方向键调整同级顺序`"
                      :disabled="saving"
                      @pointerdown="beginPointerDrag($event, parent)"
                      @click="handleParentClick(parent)"
                      @keydown="handleSortKeydown($event, parent)"
                      @contextmenu="suppressContextMenu"
                    >
                      <span class="category-parent__icon" aria-hidden="true">
                        <el-icon>
                          <component :is="categoryIcon(parent.name, parent.icon)" />
                        </el-icon>
                      </span>
                      <span class="category-parent__text">
                        <span class="category-parent__name">{{ parent.name }}</span>
                        <span v-if="!parent.visible" class="category-parent__meta">
                          已停用
                        </span>
                      </span>
                      <span class="category-parent__meta category-parent__count">
                        {{ childrenOf(activeKind, parent.name).length }} 个子分类
                      </span>
                      <el-icon
                        class="category-parent__chevron"
                        :class="{ 'is-open': isExpanded(parent.name) }"
                        aria-hidden="true"
                      >
                        <ArrowDown />
                      </el-icon>
                    </button>
                    <button
                      type="button"
                      class="category-parent__more"
                      :aria-label="`管理${parent.name}`"
                      title="更多操作"
                      :disabled="saving"
                      @click.stop="openActionSheet(parent)"
                    >
                      <el-icon aria-hidden="true"><MoreFilled /></el-icon>
                    </button>
                  </div>

                  <div v-if="isExpanded(parent.name)" class="category-children">
                    <TransitionGroup
                      tag="div"
                      name="category-sort"
                      class="category-child-grid"
                      :data-sort-scope="`child:${parent.name}`"
                      role="group"
                      :aria-label="`${parent.name}的子分类`"
                    >
                      <button
                        v-for="child in childrenOf(activeKind, parent.name)"
                        :key="`child:${child.name}`"
                        type="button"
                        class="category-child"
                        :class="{
                          'is-disabled': !isCategoryVisible(child, activeEntries),
                          'is-dragging': dragName === child.name,
                        }"
                        :data-category-card-name="child.name"
                        :aria-label="`${child.name}，${
                          isCategoryVisible(child, activeEntries)
                            ? '显示中'
                            : child.visible
                              ? '随一级分类停用'
                              : '已停用'
                        }，点击打开操作菜单；使用方向键调整同级顺序`"
                        :disabled="saving"
                        @pointerdown="beginPointerDrag($event, child)"
                        @click="handleChildClick(child)"
                        @keydown="handleSortKeydown($event, child)"
                        @contextmenu="suppressContextMenu"
                      >
                        <span class="category-child__icon" aria-hidden="true">
                          <el-icon>
                            <component :is="categoryIcon(child.name, child.icon)" />
                          </el-icon>
                        </span>
                        <span class="category-child__name">{{ child.name }}</span>
                        <span
                          v-if="!isCategoryVisible(child, activeEntries)"
                          class="category-child__badge"
                        >
                          {{ child.visible ? '随一级停用' : '已停用' }}
                        </span>
                      </button>
                      <button
                        key="add"
                        type="button"
                        class="category-child category-child-add"
                        :aria-label="`为${parent.name}添加子分类`"
                        title="添加子分类"
                        :disabled="saving"
                        @click="openCreateChild(parent.name)"
                      >
                        <el-icon aria-hidden="true"><Plus /></el-icon>
                      </button>
                    </TransitionGroup>
                  </div>
                </li>
              </TransitionGroup>
            </section>
          </template>

          <p
            v-if="visibleParents.length === 0 && hiddenParents.length > 0"
            class="category-list-empty is-warning"
            role="status"
          >
            当前没有显示中的一级分类。请在“已停用”区域展开分类并启用，或新增一个一级分类；保存前至少保留一个显示的一级分类。
          </p>
          <p
            v-if="hiddenParents.length === 0 && visibleParents.length > 0"
            class="category-list-hint"
          >
            长按一级分类或子分类卡片可拖动排序；点击“更多”可修改、停用或查看账单。
          </p>
          <p
            v-if="activeEntryCount === 0"
            class="category-list-empty"
            role="status"
          >
            还没有{{ activeLabel }}分类，点击右上角的加号新增一级分类。
          </p>
        </div>
        </template>
      </template>

      <p class="sr-only" aria-live="polite">{{ liveMessage }}</p>
    </div>

    <template v-if="!editorState" #footer>
      <div class="category-settings-footer">
        <el-alert
          v-if="errorMessage"
          class="category-save-error"
          :title="errorMessage"
          type="error"
          :closable="false"
          show-icon
        />
        <div class="category-settings-footer__row">
          <p
            class="category-settings-footer__status"
            :class="{
              'is-warning': categoryState.loaded && visibleParents.length === 0,
              'is-dragging': dragName !== null,
            }"
          >
            <template v-if="loading">正在加载家庭共享分类…</template>
            <template v-else-if="saving">正在保存分类设置…</template>
            <template v-else-if="dragName">
              正在拖动「{{ dragName }}」，松手放到同级新位置
            </template>
            <template v-else-if="!categoryState.loaded">
              分类设置尚未加载，暂时无法保存
            </template>
            <template v-else-if="visibleParents.length === 0">
              请至少保留一个显示的一级分类
            </template>
            <template v-else>
              {{ activeLabel }}：{{ visibleParents.length }} 个显示一级分类 ·
              {{ activeChildCount }} 个子分类<template v-if="hiddenParents.length > 0"
                >，{{ hiddenParents.length }} 个停用一级分类</template
              ><template v-if="dirtyKinds.length > 0"> · 有未保存修改</template>
            </template>
          </p>
          <div class="dialog-footer category-settings-footer__actions">
            <el-button
              class="category-cancel-button"
              :disabled="saving || dragBusy"
              @click="closeDialog"
            >
              取消
            </el-button>
            <el-button
              class="category-save-button"
              type="primary"
              :loading="saving"
              :disabled="busy || dragBusy"
              @click="submit"
            >
              保存设置
            </el-button>
          </div>
        </div>
      </div>
    </template>

    <div v-if="actionTarget" class="category-action-layer">
      <div class="category-action-backdrop" @click="closeActionSheet" />
      <section
        ref="sheetRef"
        class="category-action-sheet"
        role="dialog"
        aria-modal="true"
        aria-labelledby="category-action-title"
        tabindex="-1"
        @keydown.esc.stop.prevent="closeActionSheet"
      >
        <header class="category-action-sheet__head">
          <span class="category-action-sheet__icon" aria-hidden="true">
            <el-icon>
              <component :is="categoryIcon(actionEntry?.name ?? '', actionEntry?.icon)" />
            </el-icon>
          </span>
          <div class="category-action-sheet__title">
            <h3 id="category-action-title">{{ actionEntry?.name }}</h3>
            <p>
              {{ sheetHeading }} ·
              {{ actionEntry?.visible ? '显示中' : '已停用' }}
              <template v-if="actionEntry?.parentName && actionParent">
                · 上级：{{ actionParent?.name
                }}<template v-if="!actionParent?.visible">（已停用）</template>
              </template>
            </p>
          </div>
          <button
            type="button"
            class="category-action-sheet__close"
            aria-label="关闭操作菜单"
            @click="closeActionSheet"
          >
            <el-icon aria-hidden="true"><Close /></el-icon>
          </button>
        </header>

        <div v-if="sheetMode === 'actions'" class="category-action-sheet__actions">
          <button
            type="button"
            class="category-action-sheet__action"
            data-action="toggle"
            data-sheet-autofocus
            @click="toggleActionVisibility"
          >
            <el-icon aria-hidden="true"><SwitchIcon /></el-icon>
            <span>{{ actionEntry?.visible ? '停用' : '启用' }}</span>
          </button>
          <button
            type="button"
            class="category-action-sheet__action"
            data-action="edit"
            @click="editActionTarget"
          >
            <el-icon aria-hidden="true"><EditPen /></el-icon>
            <span>修改</span>
          </button>
          <button
            v-if="actionIsChild"
            type="button"
            class="category-action-sheet__action"
            data-action="promote"
            @click="promoteAction"
          >
            <el-icon aria-hidden="true"><Top /></el-icon>
            <span>子类升一级</span>
          </button>
          <button
            v-if="actionIsChild || actionChildCount === 0"
            type="button"
            class="category-action-sheet__action"
            data-action="move"
            :disabled="otherParents.length === 0"
            @click="openMoveSheet"
          >
            <el-icon aria-hidden="true"><Right /></el-icon>
            <span>移至其他一级</span>
            <span v-if="otherParents.length === 0" class="category-action-sheet__note">
              暂无其他一级分类
            </span>
          </button>
          <button
            type="button"
            class="category-action-sheet__action"
            data-action="bills"
            @click="viewBillsAction"
          >
            <el-icon aria-hidden="true"><Tickets /></el-icon>
            <span>查看账单</span>
          </button>
          <button
            type="button"
            class="category-action-sheet__action is-danger"
            data-action="delete"
            @click="beginDelete"
          >
            <el-icon aria-hidden="true"><Delete /></el-icon>
            <span>永久删除</span>
          </button>
          <button
            type="button"
            class="category-action-sheet__action is-cancel"
            data-action="cancel"
            @click="closeActionSheet"
          >
            <el-icon aria-hidden="true"><Close /></el-icon>
            <span>取消</span>
          </button>
        </div>

        <div v-else-if="sheetMode === 'move'" class="category-action-sheet__sub">
          <button
            type="button"
            class="category-action-sheet__back"
            data-action="back"
            data-sheet-autofocus
            @click="backToActions"
          >
            <el-icon aria-hidden="true"><Back /></el-icon>
            返回操作
          </button>
          <p class="category-action-sheet__lead">选择新的一级分类：</p>
          <button
            v-for="parent in otherParents"
            :key="parent.name"
            type="button"
            class="category-action-sheet__parent"
            data-action="move-to"
            :data-parent="parent.name"
            @click="moveActionTo(parent.name)"
          >
            <el-icon aria-hidden="true">
              <component :is="categoryIcon(parent.name, parent.icon)" />
            </el-icon>
            <span>{{ parent.name }}</span>
            <span v-if="!parent.visible" class="category-action-sheet__note">
              已停用
            </span>
          </button>
          <p v-if="otherParents.length === 0" class="category-action-sheet__empty">
            暂无其他一级分类，请先新增一个一级分类。
          </p>
        </div>

        <div v-else class="category-action-sheet__sub">
          <p class="category-action-sheet__warning">
            <el-icon aria-hidden="true"><WarningFilled /></el-icon>
            {{ deleteWarning }}
          </p>
          <button
            type="button"
            class="category-action-sheet__action is-danger"
            data-action="confirm-delete"
            data-sheet-autofocus
            @click="confirmDelete"
          >
            确认永久删除
          </button>
          <button
            type="button"
            class="category-action-sheet__action is-cancel"
            data-action="back"
            @click="backToActions"
          >
            返回
          </button>
        </div>
      </section>
    </div>
  </el-dialog>
</template>
<style scoped>
:global(.el-dialog.category-settings-dialog.is-fullscreen) {
  display: flex;
  flex-direction: column;
  width: 100%;
  max-width: none;
  height: 100vh;
  height: 100dvh;
  margin: 0;
  padding: 0;
  overflow: hidden;
  border-radius: 0;
  background: var(--hb-surface);
}

:global(.el-dialog.category-settings-dialog.is-fullscreen .el-dialog__header) {
  flex: 0 0 auto;
  padding: calc(12px + env(safe-area-inset-top, 0px))
    calc(20px + env(safe-area-inset-right, 0px)) 12px
    calc(20px + env(safe-area-inset-left, 0px));
  margin: 0;
  border-bottom: 1px solid var(--hb-line);
}

:global(.el-dialog.category-settings-dialog.is-fullscreen .el-dialog__body) {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  min-height: 0;
  padding: 14px calc(20px + env(safe-area-inset-right, 0px)) 0
    calc(20px + env(safe-area-inset-left, 0px));
  overflow: hidden;
}

:global(.el-dialog.category-settings-dialog.is-editing .el-dialog__body) {
  padding-bottom: calc(16px + env(safe-area-inset-bottom, 0px));
}

:global(.el-dialog.category-settings-dialog.is-fullscreen .el-dialog__footer) {
  flex: 0 0 auto;
  padding: 12px calc(20px + env(safe-area-inset-right, 0px))
    calc(12px + env(safe-area-inset-bottom, 0px))
    calc(20px + env(safe-area-inset-left, 0px));
  margin: 0;
  background: var(--hb-surface);
  border-top: 1px solid var(--hb-line);
}

.category-settings-header {
  display: flex;
  gap: 12px;
  align-items: center;
  width: 100%;
  max-width: 1180px;
  margin: 0 auto;
}

.category-settings-header__close,
.category-settings-header__add {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  width: 40px;
  height: 40px;
  padding: 0;
  color: var(--hb-ink-soft);
  font-size: 18px;
  cursor: pointer;
  background: var(--hb-bg);
  border: 1px solid var(--hb-line);
  border-radius: 12px;
  -webkit-tap-highlight-color: transparent;
}

.category-settings-header__add {
  margin-left: auto;
  color: var(--hb-teal-dark);
  background: var(--hb-teal-faint);
  border-color: var(--hb-teal-soft);
}

.category-settings-header__close:hover:not(:disabled),
.category-settings-header__add:hover:not(:disabled) {
  color: var(--hb-ink);
  border-color: var(--hb-line-strong);
}

.category-settings-header__add:hover:not(:disabled) {
  color: var(--hb-teal-dark);
  border-color: var(--hb-teal);
}

.category-settings-header__close:disabled,
.category-settings-header__add:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

.category-settings-header__close:focus-visible,
.category-settings-header__add:focus-visible {
  outline: 2px solid var(--hb-teal);
  outline-offset: 2px;
}

.category-settings-header__heading {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.category-settings-header__heading h2 {
  overflow: hidden;
  color: var(--hb-ink);
  font-family: var(--hb-display);
  font-size: 20px;
  font-weight: 600;
  line-height: 1.3;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.category-settings-header__heading p {
  color: var(--hb-ink-faint);
  font-size: 11.5px;
  line-height: 1.5;
}

.category-settings {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  gap: 12px;
  width: 100%;
  max-width: 1180px;
  min-height: 0;
  margin: 0 auto;
}

.category-settings.is-editing {
  max-width: none;
}

.category-kind-tabs {
  display: flex;
  flex: 0 0 auto;
  gap: 8px;
  padding: 4px;
  background: var(--hb-bg);
  border: 1px solid var(--hb-line);
  border-radius: 14px;
}

.category-kind-tab {
  display: inline-flex;
  flex: 1 1 0;
  gap: 8px;
  align-items: center;
  justify-content: center;
  min-height: 40px;
  padding: 6px 14px;
  color: var(--hb-ink-soft);
  font: inherit;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  background: transparent;
  border: 1px solid transparent;
  border-radius: 10px;
  appearance: none;
  -webkit-tap-highlight-color: transparent;
  transition:
    color 0.16s ease,
    background-color 0.16s ease,
    border-color 0.16s ease;
}

.category-kind-tab:hover:not(:disabled) {
  color: var(--hb-ink);
  background: var(--hb-surface);
}

.category-kind-tab.is-active {
  color: var(--hb-teal-dark);
  background: var(--hb-surface);
  border-color: var(--hb-teal-soft);
  box-shadow: var(--hb-shadow-sm);
}

.category-kind-tab:focus-visible {
  outline: 2px solid var(--hb-teal);
  outline-offset: 2px;
}

.category-kind-tab:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}

.category-kind-tab__count {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 22px;
  min-height: 20px;
  padding: 0 6px;
  color: var(--hb-ink-faint);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  background: var(--hb-bg-deep);
  border-radius: 999px;
}

.category-kind-tab.is-active .category-kind-tab__count {
  color: var(--hb-teal-dark);
  background: var(--hb-teal-soft);
}

.category-list-scroll {
  flex: 1 1 auto;
  min-height: 0;
  padding: 2px 4px 28px;
  overflow-y: auto;
  overscroll-behavior: contain;
  touch-action: pan-y;
  scrollbar-gutter: stable;
}

.category-section + .category-section {
  margin-top: 22px;
}

.category-section__head {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 12px;
  align-items: baseline;
  justify-content: space-between;
  margin: 0 2px 10px;
}

.category-section__head h3 {
  color: var(--hb-ink);
  font-size: 14px;
  font-weight: 600;
}

.category-section__head span {
  color: var(--hb-ink-faint);
  font-size: 11px;
}

.category-parent-list {
  position: relative;
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 10px;
  margin: 0;
  padding: 2px;
  list-style: none;
}

.category-parent {
  min-width: 0;
  background: var(--hb-surface-raised);
  border: 1px solid var(--hb-line);
  border-radius: 16px;
  box-shadow: var(--hb-shadow-sm);
  transition:
    background-color 0.16s ease,
    border-color 0.16s ease,
    box-shadow 0.16s ease,
    scale 0.16s ease;
}

.category-parent.is-disabled {
  background: var(--hb-surface);
  border-style: dashed;
  box-shadow: none;
}

.category-parent.is-dragging,
.category-child.is-dragging {
  z-index: 1;
  background: var(--hb-teal-faint);
  border-color: var(--hb-teal);
  box-shadow: 0 12px 30px rgba(10, 93, 87, 0.18);
  scale: 1.02;
}

.category-parent__head {
  display: flex;
  gap: 4px;
  align-items: stretch;
  min-width: 0;
}

.category-parent__main {
  display: flex;
  flex: 1 1 auto;
  gap: 10px;
  align-items: center;
  min-width: 0;
  min-height: 64px;
  padding: 10px 6px 10px 14px;
  color: var(--hb-ink);
  font: inherit;
  text-align: left;
  cursor: pointer;
  background: transparent;
  border: 0;
  border-radius: 16px 0 0 16px;
  appearance: none;
  -webkit-tap-highlight-color: transparent;
  -webkit-user-select: none;
  user-select: none;
  -webkit-touch-callout: none;
  touch-action: pan-y;
}

.category-parent__main:hover:not(:disabled) {
  background: var(--hb-bg);
}

.category-parent__main:focus-visible {
  outline: 2px solid var(--hb-teal);
  outline-offset: -2px;
}

.category-parent__main:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.category-parent__icon,
.category-child__icon {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  color: var(--hb-teal);
}

.category-parent__icon {
  width: 38px;
  height: 38px;
  background: var(--hb-teal-faint);
  border: 1px solid var(--hb-teal-soft);
  border-radius: 12px;
}

.category-parent__icon :deep(.el-icon) {
  font-size: 20px;
}

.category-parent.is-disabled .category-parent__icon {
  color: var(--hb-ink-faint);
  background: var(--hb-bg);
  border-color: var(--hb-line);
}

.category-parent__text {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.category-parent__name {
  overflow: hidden;
  color: var(--hb-ink);
  font-size: 14px;
  font-weight: 600;
  line-height: 1.35;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.category-parent.is-disabled .category-parent__name {
  color: var(--hb-ink-soft);
}

.category-parent__meta {
  color: var(--hb-ink-faint);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  line-height: 1.4;
}

.category-parent__count {
  flex: 0 0 auto;
  white-space: nowrap;
}

.category-parent__chevron {
  flex: 0 0 auto;
  color: var(--hb-ink-faint);
  font-size: 14px;
  transition: transform 0.18s ease;
}

.category-parent__chevron.is-open {
  transform: rotate(180deg);
}

.category-parent__more {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  width: 40px;
  min-height: 40px;
  margin: 10px 8px 10px 0;
  padding: 0;
  color: var(--hb-ink-faint);
  font-size: 17px;
  cursor: pointer;
  background: transparent;
  border: 1px solid transparent;
  border-radius: 10px;
  appearance: none;
  -webkit-tap-highlight-color: transparent;
}

.category-parent__more:hover:not(:disabled) {
  color: var(--hb-ink);
  background: var(--hb-bg);
  border-color: var(--hb-line);
}

.category-parent__more:focus-visible {
  outline: 2px solid var(--hb-teal);
  outline-offset: 2px;
}

.category-parent__more:disabled {
  cursor: not-allowed;
  opacity: 0.45;
}

.category-children {
  padding: 8px 12px 12px;
}

.category-child-grid {
  position: relative;
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(76px, 1fr));
  gap: 8px;
  padding: 2px;
}

.category-child {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 5px;
  align-items: center;
  justify-content: center;
  min-width: 0;
  min-height: 78px;
  padding: 9px 5px 8px;
  color: var(--hb-ink-soft);
  font: inherit;
  font-size: 11.5px;
  line-height: 1.25;
  text-align: center;
  cursor: pointer;
  background: var(--hb-bg);
  border: 1px solid var(--hb-line);
  border-radius: 12px;
  appearance: none;
  -webkit-tap-highlight-color: transparent;
  -webkit-user-select: none;
  user-select: none;
  -webkit-touch-callout: none;
  touch-action: pan-y;
  transition:
    color 0.16s ease,
    background-color 0.16s ease,
    border-color 0.16s ease,
    scale 0.16s ease;
}

.category-child:hover:not(:disabled) {
  background: var(--hb-surface-raised);
  border-color: var(--hb-line-strong);
}

.category-child.is-disabled {
  background: var(--hb-surface);
  border-style: dashed;
  opacity: 0.72;
}

.category-child:focus-visible {
  outline: 2px solid var(--hb-teal);
  outline-offset: 2px;
}

.category-child:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.category-child__icon :deep(.el-icon) {
  font-size: 19px;
}

.category-child.is-disabled .category-child__icon {
  color: var(--hb-ink-faint);
}

.category-child__name {
  max-width: 100%;
  overflow: hidden;
  color: var(--hb-ink);
  font-weight: 500;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.category-child__badge {
  padding: 0 5px;
  color: var(--hb-ink-faint);
  font-size: 9.5px;
  line-height: 1.5;
  background: var(--hb-bg-deep);
  border-radius: 999px;
}

.category-child-add {
  color: var(--hb-teal-dark);
  font-size: 16px;
  background: var(--hb-teal-faint);
  border-color: var(--hb-teal-soft);
}

.category-child-add:hover:not(:disabled) {
  background: var(--hb-teal-soft);
  border-color: var(--hb-teal);
}

.category-list-empty,
.category-list-hint {
  max-width: 560px;
  margin: 18px auto 0;
  padding: 14px 16px;
  color: var(--hb-ink-faint);
  font-size: 12px;
  line-height: 1.7;
  text-align: center;
  background: var(--hb-surface-raised);
  border: 1px dashed var(--hb-line-strong);
  border-radius: 14px;
}

.category-list-empty.is-warning {
  color: var(--hb-danger);
  background: var(--hb-danger-soft);
  border-color: var(--hb-danger);
}

.category-list-hint {
  padding: 0;
  background: transparent;
  border: 0;
}

.category-load-state {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  gap: 14px;
  align-items: center;
  justify-content: center;
  padding: 48px 20px;
  color: var(--hb-ink-faint);
  font-size: 13px;
  line-height: 1.6;
  text-align: center;
}

.category-load-state p {
  margin: 0;
}

.category-load-state.is-error {
  color: var(--hb-danger);
}

.category-editor-panel {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  gap: 12px;
  min-width: 0;
  min-height: 0;
  padding: 14px;
  overflow-y: auto;
  overscroll-behavior: contain;
  background: var(--hb-surface-raised);
  border: 1px solid var(--hb-line);
  border-radius: 18px;
}

.category-settings-footer {
  width: 100%;
  max-width: 1180px;
  margin: 0 auto;
}

.category-save-error {
  margin-bottom: 10px;
  text-align: left;
}

.category-settings-footer__row {
  display: flex;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
}

.category-settings-footer__status {
  min-width: 0;
  margin: 0;
  color: var(--hb-ink-faint);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  line-height: 1.5;
}

.category-settings-footer__status.is-warning {
  color: var(--hb-danger);
  font-weight: 600;
}

.category-settings-footer__status.is-dragging {
  color: var(--hb-teal);
  font-weight: 600;
}

.category-settings-footer__actions {
  flex: 0 0 auto;
}

.category-sort-move {
  transition:
    transform 0.22s cubic-bezier(0.2, 0, 0, 1),
    color 0.16s ease,
    background-color 0.16s ease,
    border-color 0.16s ease,
    box-shadow 0.16s ease,
    scale 0.16s ease;
}

.category-action-layer {
  position: fixed;
  inset: 0;
  z-index: 2300;
  display: flex;
  align-items: flex-end;
  justify-content: center;
  padding: 16px;
}

.category-action-backdrop {
  position: absolute;
  inset: 0;
  background: rgba(32, 27, 18, 0.46);
  backdrop-filter: blur(2px);
}

.category-action-sheet {
  position: relative;
  width: min(100%, 560px);
  max-height: min(78vh, 720px);
  padding: 14px;
  overflow-y: auto;
  overscroll-behavior: contain;
  background: var(--hb-surface);
  border: 1px solid var(--hb-line);
  border-radius: 20px;
  box-shadow: var(--hb-shadow-lg);
  animation: category-sheet-in 0.18s ease-out;
}

@keyframes category-sheet-in {
  from {
    opacity: 0;
    transform: translateY(18px);
  }

  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.category-action-sheet:focus {
  outline: none;
}

.category-action-sheet__head {
  display: flex;
  gap: 10px;
  align-items: center;
  min-width: 0;
  padding: 2px 2px 10px;
  border-bottom: 1px solid var(--hb-line);
}

.category-action-sheet__icon {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  width: 40px;
  height: 40px;
  color: var(--hb-teal);
  background: var(--hb-teal-faint);
  border: 1px solid var(--hb-teal-soft);
  border-radius: 12px;
}

.category-action-sheet__icon :deep(.el-icon) {
  font-size: 20px;
}

.category-action-sheet__title {
  flex: 1 1 auto;
  min-width: 0;
}

.category-action-sheet__title h3 {
  overflow: hidden;
  color: var(--hb-ink);
  font-size: 16px;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.category-action-sheet__title p {
  margin-top: 2px;
  color: var(--hb-ink-faint);
  font-size: 11px;
  line-height: 1.5;
}

.category-action-sheet__close {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  width: 34px;
  height: 34px;
  padding: 0;
  color: var(--hb-ink-faint);
  font-size: 16px;
  cursor: pointer;
  background: transparent;
  border: 1px solid transparent;
  border-radius: 10px;
  appearance: none;
}

.category-action-sheet__close:hover {
  color: var(--hb-ink);
  background: var(--hb-bg);
  border-color: var(--hb-line);
}

.category-action-sheet__close:focus-visible {
  outline: 2px solid var(--hb-teal);
  outline-offset: 2px;
}

.category-action-sheet__actions {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding-top: 10px;
}

.category-action-sheet__action {
  display: flex;
  gap: 10px;
  align-items: center;
  width: 100%;
  min-height: 46px;
  padding: 10px 12px;
  color: var(--hb-ink);
  font: inherit;
  font-size: 14px;
  font-weight: 500;
  text-align: left;
  cursor: pointer;
  background: var(--hb-surface-raised);
  border: 1px solid var(--hb-line);
  border-radius: 12px;
  appearance: none;
  -webkit-tap-highlight-color: transparent;
}

.category-action-sheet__action:hover:not(:disabled) {
  background: var(--hb-bg);
  border-color: var(--hb-line-strong);
}

.category-action-sheet__action:focus-visible {
  outline: 2px solid var(--hb-teal);
  outline-offset: 2px;
}

.category-action-sheet__action.is-danger {
  color: var(--hb-danger);
  background: var(--hb-danger-soft);
  border-color: transparent;
}

.category-action-sheet__action.is-cancel {
  justify-content: center;
  color: var(--hb-ink-soft);
  background: transparent;
}

.category-action-sheet__action:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.category-action-sheet__note {
  margin-left: auto;
  color: var(--hb-ink-faint);
  font-size: 11px;
}

.category-action-sheet__sub {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding-top: 10px;
}

.category-action-sheet__back {
  display: inline-flex;
  gap: 5px;
  align-items: center;
  align-self: flex-start;
  min-height: 34px;
  padding: 4px 8px;
  color: var(--hb-ink-soft);
  font: inherit;
  font-size: 12px;
  cursor: pointer;
  background: transparent;
  border: 0;
  border-radius: 8px;
}

.category-action-sheet__back:hover {
  background: var(--hb-bg);
}

.category-action-sheet__back:focus-visible {
  outline: 2px solid var(--hb-teal);
  outline-offset: 2px;
}

.category-action-sheet__lead {
  margin: 2px 2px 2px;
  color: var(--hb-ink-faint);
  font-size: 12px;
}

.category-action-sheet__parent {
  display: flex;
  gap: 10px;
  align-items: center;
  width: 100%;
  min-height: 46px;
  padding: 8px 12px;
  color: var(--hb-ink);
  font: inherit;
  font-size: 14px;
  text-align: left;
  cursor: pointer;
  background: var(--hb-surface-raised);
  border: 1px solid var(--hb-line);
  border-radius: 12px;
  appearance: none;
}

.category-action-sheet__parent:hover {
  background: var(--hb-bg);
  border-color: var(--hb-line-strong);
}

.category-action-sheet__parent:focus-visible {
  outline: 2px solid var(--hb-teal);
  outline-offset: 2px;
}

.category-action-sheet__parent :deep(.el-icon) {
  color: var(--hb-teal);
  font-size: 18px;
}

.category-action-sheet__warning {
  display: flex;
  gap: 8px;
  align-items: flex-start;
  margin: 2px 2px 6px;
  padding: 10px 12px;
  color: var(--hb-danger);
  font-size: 12px;
  line-height: 1.7;
  background: var(--hb-danger-soft);
  border-radius: 10px;
}

.category-action-sheet__warning :deep(.el-icon) {
  flex: 0 0 auto;
  margin-top: 2px;
  font-size: 16px;
}

.category-action-sheet__empty {
  margin: 4px 0;
  color: var(--hb-ink-faint);
  font-size: 12px;
  line-height: 1.6;
  text-align: center;
}

@media (max-width: 640px) {
  :global(.el-dialog.category-settings-dialog.is-fullscreen .el-dialog__header) {
    padding: calc(8px + env(safe-area-inset-top, 0px))
      calc(12px + env(safe-area-inset-right, 0px)) 8px
      calc(12px + env(safe-area-inset-left, 0px));
  }

  :global(.el-dialog.category-settings-dialog.is-fullscreen .el-dialog__body) {
    padding: 10px calc(12px + env(safe-area-inset-right, 0px)) 0
      calc(12px + env(safe-area-inset-left, 0px));
  }

  :global(.el-dialog.category-settings-dialog.is-fullscreen .el-dialog__footer) {
    padding: 10px calc(12px + env(safe-area-inset-right, 0px))
      calc(10px + env(safe-area-inset-bottom, 0px))
      calc(12px + env(safe-area-inset-left, 0px));
  }

  .category-settings-header__close,
  .category-settings-header__add {
    width: 38px;
    height: 38px;
  }

  .category-settings-header__heading h2 {
    font-size: 18px;
  }

  .category-settings-header__heading p {
    display: none;
  }

  .category-parent-list {
    grid-template-columns: minmax(0, 1fr);
    gap: 8px;
  }

  .category-child-grid {
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 6px;
  }

  .category-child {
    min-height: 72px;
    padding: 8px 3px 7px;
    font-size: 11px;
  }

  .category-settings-footer__row {
    flex-direction: column;
    align-items: stretch;
    gap: 8px;
  }

  .category-settings-footer__status {
    font-size: 11.5px;
  }

  .category-settings-footer__actions {
    justify-content: flex-end;
  }

  .category-editor-panel {
    padding: 12px;
    border-radius: 14px;
  }

  .category-action-layer {
    padding: 0;
  }

  .category-action-sheet {
    width: 100%;
    max-height: 86vh;
    padding: 12px calc(12px + env(safe-area-inset-right, 0px))
      calc(12px + env(safe-area-inset-bottom, 0px))
      calc(12px + env(safe-area-inset-left, 0px));
    border-radius: 20px 20px 0 0;
  }
}

@media (prefers-reduced-motion: reduce) {
  .category-parent,
  .category-child,
  .category-sort-move,
  .category-parent__chevron,
  .category-action-sheet {
    animation: none;
    transition: none;
  }
}
</style>
