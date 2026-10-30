<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  ref,
  watch,
  type ComponentPublicInstance,
} from 'vue'
import { Back, Close, Plus, Rank } from '@element-plus/icons-vue'
import { authState } from '../stores/auth'
import {
  readCategoryEntries,
  saveCategoryEntries,
  type CategoryEntry,
  type CategoryKind,
} from '../stores/categories'
import { categoryIcon } from '../utils/categoryIcons'
import CategoryEditor from './CategoryEditor.vue'

const props = defineProps<{
  modelValue: boolean
  kind: CategoryKind
  currentCategory?: string
}>()

const emit = defineEmits<{
  (event: 'update:modelValue', value: boolean): void
  (event: 'saved', selectedCategory?: string): void
}>()

const DRAG_THRESHOLD = 6
const DRAG_EDGE_SIZE = 64
const DRAG_MAX_SCROLL_SPEED = 18
const HIDDEN_HINT_ID = 'category-hidden-hint'
const KEY_DELTAS: Record<string, number> = {
  ArrowUp: -1,
  ArrowDown: 1,
  ArrowLeft: -1,
  ArrowRight: 1,
}

type EditorMode = 'create' | 'edit' | null

interface LayoutSlot {
  left: number
  top: number
  width: number
  height: number
}

interface DragSession {
  pointerId: number
  name: string
  startX: number
  startY: number
  lastX: number
  lastY: number
  active: boolean
  snapshot: CategoryEntry[]
  captureTarget: HTMLElement
  rafId: number | null
  slots: LayoutSlot[]
}

const draft = ref<CategoryEntry[]>([])
const addedNames = ref<string[]>([])
const errorMessage = ref('')
const saving = ref(false)
const editorMode = ref<EditorMode>(null)
const editingName = ref<string | null>(null)
const preferredName = ref<string | null>(null)
const dragName = ref<string | null>(null)
const dragBusy = ref(false)
const liveMessage = ref('')
const gridScrollRef = ref<HTMLElement | null>(null)
const gridGroupRef = ref<ComponentPublicInstance | null>(null)
const gridRef = computed<HTMLElement | null>(() => {
  const element = gridGroupRef.value?.$el
  return element instanceof HTMLElement ? element : null
})

let openedScope: string | null = null
let dragSession: DragSession | null = null

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const kindLabel = computed(() => (props.kind === 'income' ? '收入' : '支出'))
const dialogTitle = computed(() => `${kindLabel.value}分类设置`)
const visibleCount = computed(
  () => draft.value.filter((entry) => entry.visible).length,
)
const hiddenCount = computed(() => draft.value.length - visibleCount.value)
const visibleEntries = computed(() =>
  draft.value.filter((entry) => entry.visible),
)
const hiddenEntries = computed(() =>
  draft.value.filter((entry) => !entry.visible),
)
const orderedEntries = computed(() => [
  ...visibleEntries.value,
  ...hiddenEntries.value,
])
const draftNames = computed(() => draft.value.map((entry) => entry.name))
const editorOpen = computed(() => editorMode.value !== null)
const isCreating = computed(() => editorMode.value === 'create')
const editingEntry = computed(() =>
  editingName.value === null
    ? undefined
    : draft.value.find((entry) => entry.name === editingName.value),
)
const editorKey = computed(() =>
  editorMode.value === 'create'
    ? 'create'
    : (editingName.value ?? 'none'),
)

function currentScope(): string | null {
  const userId = authState.user?.id
  const householdId = authState.household?.id
  if (userId === undefined || householdId === undefined) return null
  return `${householdId}:${userId}`
}

function currentCategoryName(): string | null {
  const current = props.currentCategory?.trim()
  return current ? current : null
}

function copyEntry(entry: CategoryEntry): CategoryEntry {
  const copy: CategoryEntry = { ...entry }
  if (entry.previousNames) copy.previousNames = [...entry.previousNames]
  return copy
}

function copyEntries(entries: CategoryEntry[]): CategoryEntry[] {
  return entries.map(copyEntry)
}

function loadDraft(): void {
  closeEditor()
  cancelDrag(false)
  openedScope = currentScope()
  draft.value = copyEntries(
    readCategoryEntries(props.kind, props.currentCategory),
  )
  addedNames.value = []
  preferredName.value = currentCategoryName()
  errorMessage.value = ''
  liveMessage.value = ''
}

function clearTransientState(): void {
  closeEditor()
  cancelDrag(false)
  draft.value = []
  addedNames.value = []
  preferredName.value = null
  errorMessage.value = ''
  liveMessage.value = ''
  openedScope = null
}

watch(
  [() => props.modelValue, () => props.kind],
  ([open]) => {
    if (open) {
      loadDraft()
    } else {
      clearTransientState()
    }
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
  cancelDrag(false)
})

function closeEditor(): void {
  editorMode.value = null
  editingName.value = null
}

function closeDialog(): void {
  closeEditor()
  cancelDrag(false)
  emit('update:modelValue', false)
}

function handleHeaderClose(): void {
  if (isCreating.value) {
    cancelEditor()
    return
  }
  closeDialog()
}

function handleDialogClosed(): void {
  closeEditor()
  cancelDrag(false)
}

function openCreate(): void {
  if (saving.value) return
  if (editorMode.value !== null) {
    errorMessage.value = '请先应用或取消当前编辑，再新增分类'
    return
  }
  cancelDrag(true)
  editorMode.value = 'create'
  editingName.value = null
  errorMessage.value = ''
  liveMessage.value = '已打开新增分类表单'
}

function openEdit(name: string): void {
  if (saving.value) return
  if (editorMode.value !== null) {
    errorMessage.value = '请先应用或取消当前编辑，再编辑其他分类'
    return
  }
  const entry = draft.value.find((item) => item.name === name)
  if (!entry) return
  cancelDrag(true)
  editorMode.value = 'edit'
  editingName.value = name
  errorMessage.value = ''
  liveMessage.value = `正在编辑分类${name}`
}

function cancelEditor(): void {
  if (saving.value) return
  const mode = editorMode.value
  closeEditor()
  errorMessage.value = ''
  liveMessage.value =
    mode === 'edit' ? '已取消编辑，分类保持原样' : '已取消新增分类'
}

function mergePreviousNames(
  entry: CategoryEntry,
  newName: string,
): string[] {
  const merged: string[] = []
  for (const previous of [...(entry.previousNames ?? []), entry.name]) {
    if (previous === newName || merged.includes(previous)) continue
    merged.push(previous)
  }
  return merged
}

function applyEditorEntry(entry: CategoryEntry): void {
  if (saving.value) return
  const mode = editorMode.value
  if (mode === null) return
  errorMessage.value = ''
  // The editor owns only the name and icon. Visibility and rename metadata
  // always come from the current draft entry so later list changes win over
  // the snapshot taken when the editor opened.
  const newName = entry.name
  const newIcon = entry.icon

  if (mode === 'create') {
    if (draft.value.some((item) => item.name === newName)) {
      errorMessage.value = '该分类已存在（包括已隐藏分类），请换个名称'
      return
    }
    const created: CategoryEntry = { name: newName, visible: true }
    if (newIcon !== undefined) created.icon = newIcon
    draft.value = [...draft.value, created]
    addedNames.value = Array.from(new Set([...addedNames.value, newName]))
    liveMessage.value = `已添加分类${newName}，当前为显示状态`
    closeEditor()
    void nextTick(() => revealCard(newName))
    return
  }

  const originalName = editingName.value
  if (originalName === null) {
    closeEditor()
    return
  }
  const index = draft.value.findIndex((item) => item.name === originalName)
  const oldEntry = index >= 0 ? draft.value[index] : undefined
  if (!oldEntry) {
    closeEditor()
    return
  }
  if (
    newName !== originalName &&
    draft.value.some(
      (item, itemIndex) => itemIndex !== index && item.name === newName,
    )
  ) {
    errorMessage.value = '该分类已存在（包括已隐藏分类），请换个名称'
    return
  }

  const nextEntry: CategoryEntry = {
    ...oldEntry,
    name: newName,
    visible: oldEntry.visible,
  }
  if (newIcon !== undefined) nextEntry.icon = newIcon
  if (oldEntry.defaultName !== undefined) {
    nextEntry.defaultName = oldEntry.defaultName
  }
  if (newName !== originalName) {
    const previousNames = mergePreviousNames(oldEntry, newName)
    if (previousNames.length > 0) {
      nextEntry.previousNames = previousNames
    } else {
      delete nextEntry.previousNames
    }
    addedNames.value = Array.from(
      new Set(
        addedNames.value.map((name) =>
          name === originalName ? newName : name,
        ),
      ),
    )
    if (preferredName.value === originalName) {
      preferredName.value = newName
    }
    liveMessage.value = `已将分类${originalName}重命名为${newName}，历史账目保持不变`
  } else {
    liveMessage.value = `已更新分类${newName}的图标`
  }

  draft.value = draft.value.map((item, itemIndex) =>
    itemIndex === index ? nextEntry : item,
  )
  closeEditor()
  void nextTick(() => revealCard(newName))
}

function revealCard(name: string): void {
  const scroll = gridScrollRef.value
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

function handleVisibilityChange(entry: CategoryEntry): void {
  if (dragSession) {
    cancelDrag(true)
    errorMessage.value = '请先完成拖动，再调整分类的显示状态'
    return
  }
  errorMessage.value = ''
  if (entry.visible) {
    liveMessage.value = `已显示分类${entry.name}`
    return
  }
  liveMessage.value = `已隐藏分类${entry.name}，历史账目不会删除`
  if (!draft.value.some((item) => item.visible)) {
    errorMessage.value = '请至少保留一个显示的分类'
  }
}

function moveEntryToName(sourceName: string, targetName: string): boolean {
  const from = draft.value.findIndex((entry) => entry.name === sourceName)
  const to = draft.value.findIndex((entry) => entry.name === targetName)
  if (from < 0 || to < 0 || from === to) return false
  const next = [...draft.value]
  const [entry] = next.splice(from, 1)
  if (!entry) return false
  next.splice(to, 0, entry)
  draft.value = next
  errorMessage.value = ''
  return true
}

function canMoveEntry(entry: CategoryEntry, delta: number): boolean {
  const group = entry.visible ? visibleEntries.value : hiddenEntries.value
  const index = group.findIndex((item) => item.name === entry.name)
  if (index < 0) return false
  const target = index + delta
  return target >= 0 && target < group.length
}

function moveEntryBy(name: string, delta: number): boolean {
  if (dragSession) cancelDrag(true)
  if (!Number.isInteger(delta) || delta === 0) return false
  const entry = draft.value.find((item) => item.name === name)
  if (!entry) return false
  const group = entry.visible ? visibleEntries.value : hiddenEntries.value
  const index = group.findIndex((item) => item.name === name)
  const target = group[index + delta]
  if (!target) return false
  if (!moveEntryToName(name, target.name)) return false
  const nextGroup = entry.visible ? visibleEntries.value : hiddenEntries.value
  const nextIndex = nextGroup.findIndex((item) => item.name === name)
  const groupLabel = entry.visible ? '显示' : '隐藏'
  liveMessage.value = `已将分类${name}移动到${groupLabel}顺序第 ${nextIndex + 1} 位`
  return true
}

function handleCardKeydown(event: KeyboardEvent, name: string): void {
  if (saving.value || dragSession) return
  if (event.altKey || event.ctrlKey || event.metaKey) return
  const target = event.target
  if (
    target instanceof HTMLElement &&
    target.closest('input, textarea, select')
  ) {
    return
  }
  const delta = KEY_DELTAS[event.key]
  if (delta === undefined) return
  event.preventDefault()
  moveEntryBy(name, delta)
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value))
}

function pointInRect(
  x: number,
  y: number,
  rect: Pick<DOMRect, 'left' | 'right' | 'top' | 'bottom'>,
): boolean {
  return (
    x >= rect.left && x <= rect.right && y >= rect.top && y <= rect.bottom
  )
}

function isPointInsideGrid(x: number, y: number): boolean {
  const grid = gridRef.value
  if (!grid) return false
  return pointInRect(x, y, grid.getBoundingClientRect())
}

function captureLayoutSlots(): LayoutSlot[] {
  const grid = gridRef.value
  if (!grid) return []
  const measured = new Map<string, LayoutSlot>()
  const tiles = grid.querySelectorAll<HTMLElement>('[data-category-sort-name]')
  for (const tile of tiles) {
    const name = tile.dataset.categorySortName
    if (!name) continue
    // offset* ignore transforms, so later FLIP and scale styling cannot
    // change these layout coordinates while the gesture is running.
    measured.set(name, {
      left: tile.offsetLeft,
      top: tile.offsetTop,
      width: tile.offsetWidth,
      height: tile.offsetHeight,
    })
  }
  const slots: LayoutSlot[] = []
  for (const entry of visibleEntries.value) {
    const slot = measured.get(entry.name)
    if (!slot) return []
    slots.push(slot)
  }
  return slots
}

function nearestSlotIndex(
  x: number,
  y: number,
  slots: LayoutSlot[],
): number {
  let bestIndex = -1
  let bestDistance = Number.POSITIVE_INFINITY
  for (let index = 0; index < slots.length; index += 1) {
    const slot = slots[index]
    if (!slot) continue
    const dx = x - clamp(x, slot.left, slot.left + slot.width)
    const dy = y - clamp(y, slot.top, slot.top + slot.height)
    const distance = dx * dx + dy * dy
    if (distance < bestDistance) {
      bestDistance = distance
      bestIndex = index
    }
  }
  return bestIndex
}

function applyDropTarget(x: number, y: number): void {
  const session = dragSession
  if (!session || !session.active || session.slots.length === 0) return
  const grid = gridRef.value
  if (!grid) return
  const rect = grid.getBoundingClientRect()
  // Leaving the grid must not reorder anything; the drop is treated as a
  // cancel on pointerup instead.
  if (!pointInRect(x, y, rect)) return
  const slotIndex = nearestSlotIndex(
    x - rect.left - grid.clientLeft,
    y - rect.top - grid.clientTop,
    session.slots,
  )
  if (slotIndex < 0) return
  const entries = visibleEntries.value
  const currentIndex = entries.findIndex(
    (entry) => entry.name === session.name,
  )
  // Holding a steady pointer maps to the same slot as the dragged tile's
  // current index, so repeated pointermove frames cannot reorder again.
  if (slotIndex === currentIndex) return
  const target = entries[slotIndex]
  if (!target) return
  moveEntryToName(session.name, target.name)
}

function autoScrollFrame(): void {
  const session = dragSession
  if (!session || !session.active) return
  session.rafId = null

  const scroll = gridScrollRef.value
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
        const ratio = clamp(
          (rect.top + edge - session.lastY) / edge,
          0,
          1,
        )
        speed = -Math.ceil(ratio * DRAG_MAX_SCROLL_SPEED)
      } else if (session.lastY > rect.bottom - edge) {
        const ratio = clamp(
          (session.lastY - (rect.bottom - edge)) / edge,
          0,
          1,
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
  if (session.rafId !== null || typeof requestAnimationFrame !== 'function') {
    return
  }
  session.rafId = requestAnimationFrame(autoScrollFrame)
}

function cleanupDragSession(session: DragSession): void {
  if (typeof window !== 'undefined') {
    window.removeEventListener('pointermove', handlePointerMove)
    window.removeEventListener('pointerup', handlePointerUp)
    window.removeEventListener('pointercancel', handlePointerCancel)
    window.removeEventListener('blur', handleWindowBlur)
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
    session.captureTarget.releasePointerCapture(session.pointerId)
  } catch {
    // The pointer may already be released.
  }
  dragBusy.value = false
}

function cancelDrag(restore = true): void {
  const session = dragSession
  if (!session) {
    dragBusy.value = false
    return
  }
  dragSession = null
  cleanupDragSession(session)
  dragName.value = null
  if (restore && session.active) {
    draft.value = copyEntries(session.snapshot)
    liveMessage.value = '已取消本次拖动，顺序保持原样'
  }
}

function finishDrag(commit: boolean): void {
  const session = dragSession
  if (!session) return
  dragSession = null
  cleanupDragSession(session)
  dragName.value = null
  if (session.active && !commit) {
    draft.value = copyEntries(session.snapshot)
    liveMessage.value = '已取消本次拖动，顺序保持原样'
  } else if (session.active) {
    liveMessage.value = `已调整分类${session.name}的位置`
  }
}

function handleLostPointerCapture(event: Event): void {
  const session = dragSession
  if (!session || event.target !== session.captureTarget) return
  finishDrag(false)
}

function handlePointerMove(event: PointerEvent): void {
  const session = dragSession
  if (!session || event.pointerId !== session.pointerId) return
  session.lastX = event.clientX
  session.lastY = event.clientY

  if (!session.active) {
    const dx = event.clientX - session.startX
    const dy = event.clientY - session.startY
    if (dx * dx + dy * dy < DRAG_THRESHOLD * DRAG_THRESHOLD) return
    // Measure before the dragged tile starts scaling or any FLIP transform
    // runs, so the recorded slot boxes are the pure grid layout.
    session.slots = captureLayoutSlots()
    session.active = true
    dragName.value = session.name
    liveMessage.value = `正在拖动分类${session.name}，松手放到新位置`
    startDragAutoScroll(session)
  }

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
    : isPointInsideGrid(event.clientX, event.clientY)
  finishDrag(droppedInside)
}

function handlePointerCancel(event: PointerEvent): void {
  const session = dragSession
  if (!session || event.pointerId !== session.pointerId) return
  finishDrag(false)
}

function handleWindowBlur(): void {
  finishDrag(false)
}

function suppressContextMenu(event: Event): void {
  event.preventDefault()
}

function beginTileDrag(event: PointerEvent, name: string): void {
  // Mouse can drag from the card surface; touch uses the dedicated grip so
  // the grid itself stays scrollable by finger.
  if (event.pointerType !== 'mouse') return
  beginDrag(event, name)
}

function beginDrag(event: PointerEvent, name: string): void {
  if (saving.value || editorMode.value !== null) return
  // A second finger or a second pointerdown while a gesture is pending must
  // not replace or cancel the gesture that owns the capture.
  if (dragSession) return
  if (event.pointerType === 'mouse' && event.button !== 0) return
  const entry = draft.value.find((item) => item.name === name)
  if (!entry || !entry.visible) return

  // Capture on the scroll container: it never takes part in the keyed list
  // moves, so reinserting the dragged tile cannot drop pointer capture.
  const captureTarget = gridScrollRef.value
  if (!captureTarget) return

  event.preventDefault()
  event.stopPropagation()

  dragSession = {
    pointerId: event.pointerId,
    name,
    startX: event.clientX,
    startY: event.clientY,
    lastX: event.clientX,
    lastY: event.clientY,
    active: false,
    snapshot: copyEntries(draft.value),
    captureTarget,
    rafId: null,
    slots: [],
  }

  dragBusy.value = true

  try {
    captureTarget.setPointerCapture(event.pointerId)
  } catch {
    // Window listeners below still keep the gesture usable without capture.
  }
  captureTarget.addEventListener(
    'lostpointercapture',
    handleLostPointerCapture,
  )
  if (typeof window !== 'undefined') {
    window.addEventListener('pointermove', handlePointerMove, {
      passive: false,
    })
    window.addEventListener('pointerup', handlePointerUp)
    window.addEventListener('pointercancel', handlePointerCancel)
    window.addEventListener('blur', handleWindowBlur)
  }
}

function lastVisibleAddedName(): string | undefined {
  for (let index = addedNames.value.length - 1; index >= 0; index -= 1) {
    const name = addedNames.value[index]
    if (
      name !== undefined &&
      draft.value.some((entry) => entry.name === name && entry.visible)
    ) {
      return name
    }
  }
  return undefined
}

function renamedCurrentSelection(): string | undefined {
  const original = currentCategoryName()
  const preferred = preferredName.value
  if (!original || !preferred || original === preferred) return undefined
  const entry = draft.value.find((item) => item.name === preferred)
  return entry?.visible ? preferred : undefined
}

function submit(): void {
  if (saving.value) return
  if (editorMode.value !== null) {
    errorMessage.value = '请先应用或取消正在编辑的分类，再保存设置'
    liveMessage.value = errorMessage.value
    return
  }
  if (dragSession) {
    cancelDrag(true)
    errorMessage.value = '请先完成拖动，再保存设置'
    return
  }
  errorMessage.value = ''

  if (currentScope() !== openedScope) {
    errorMessage.value = '账号或家庭已变更，分类设置未保存，请重新打开'
    return
  }
  if (!draft.value.some((entry) => entry.visible)) {
    errorMessage.value = '请至少保留一个显示的分类'
    return
  }

  saving.value = true
  try {
    saveCategoryEntries(
      props.kind,
      orderedEntries.value.map(copyEntry),
    )
    const selected = lastVisibleAddedName() ?? renamedCurrentSelection()
    emit('saved', selected)
    closeDialog()
  } catch (error) {
    errorMessage.value =
      error instanceof Error && error.message
        ? error.message
        : '保存分类设置失败，请稍后重试'
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <el-dialog
    v-model="visible"
    :title="dialogTitle"
    class="ledger-dialog category-settings-dialog"
    :class="{ 'is-creating': isCreating }"
    fullscreen
    append-to-body
    :close-on-click-modal="!saving && !editorOpen"
    :close-on-press-escape="!saving && !editorOpen"
    :show-close="false"
    @closed="handleDialogClosed"
  >
    <template #header>
      <div
        class="category-settings-header"
        :class="{ 'is-creating': isCreating }"
      >
        <button
          type="button"
          class="category-settings-header__close"
          :aria-label="isCreating ? '返回分类管理' : '关闭分类设置'"
          :title="isCreating ? '返回分类管理' : '关闭'"
          :disabled="saving"
          @click="handleHeaderClose"
        >
          <el-icon>
            <Back v-if="isCreating" />
            <Close v-else />
          </el-icon>
        </button>
        <button
          v-if="!isCreating"
          type="button"
          class="category-settings-header__add"
          aria-label="新增分类"
          title="新增分类"
          :disabled="saving || editorOpen || dragBusy"
          @click="openCreate"
        >
          <el-icon aria-hidden="true"><Plus /></el-icon>
        </button>
      </div>
    </template>

    <div class="category-settings" :class="{ 'is-creating': isCreating }">

      <el-alert
        v-if="isCreating && errorMessage"
        class="category-save-error category-create-error"
        :title="errorMessage"
        type="error"
        :closable="false"
        show-icon
      />

      <div
        class="category-workspace"
        :class="{
          'has-editor': editorMode === 'edit',
          'is-creating': isCreating,
        }"
      >
        <section
          v-if="editorOpen"
          class="category-editor-panel"
          aria-label="分类编辑区"
        >
          <CategoryEditor
            :key="editorKey"
            :kind="kind"
            :entry="editorMode === 'edit' ? editingEntry : undefined"
            :existing-names="draftNames"
            :disabled="saving"
            :fullscreen="isCreating"
            @apply="applyEditorEntry"
            @cancel="cancelEditor"
          />
        </section>

        <div v-show="!isCreating" class="category-list-column">
          <div ref="gridScrollRef" class="category-sort-scroll">
            <TransitionGroup
              v-if="visibleEntries.length > 0"
              ref="gridGroupRef"
              tag="div"
              name="category-sort"
              class="category-sort-grid"
              role="list"
              aria-label="显示中的分类，可拖动排序"
            >
              <div
                v-for="(entry, index) in visibleEntries"
                :key="entry.name"
                class="category-sort-tile"
                :class="{ 'is-dragging': dragName === entry.name }"
                role="listitem"
                :data-category-sort-name="entry.name"
                :data-category-card-name="entry.name"
                @pointerdown="beginTileDrag($event, entry.name)"
              >
                <button
                  type="button"
                  class="category-sort-tile__main"
                  :disabled="saving"
                  :aria-label="`${entry.name}，显示顺序第 ${index + 1} 位。使用方向键调整顺序`"
                  @keydown="handleCardKeydown($event, entry.name)"
                >
                  <span class="category-sort-tile__icon" aria-hidden="true">
                    <el-icon><component :is="categoryIcon(entry.name, entry.icon)" /></el-icon>
                  </span>
                  <span class="category-sort-tile__name" :title="entry.name">
                    {{ entry.name }}
                  </span>
                </button>

                <label
                  class="category-sort-tile__toggle"
                  @pointerdown.stop
                >
                  <input
                    v-model="entry.visible"
                    class="category-sort-tile__checkbox"
                    type="checkbox"
                    :disabled="saving || dragBusy"
                    :aria-label="`${entry.name}显示状态`"
                    @change="handleVisibilityChange(entry)"
                  />
                  <span>{{ entry.visible ? '显示' : '隐藏' }}</span>
                </label>

                <div
                  class="category-sort-tile__actions"
                  @pointerdown.stop
                >
                  <button
                    type="button"
                    class="category-sort-tile__action"
                    :disabled="saving || editorOpen || dragBusy"
                    :aria-label="`编辑分类${entry.name}`"
                    @click="openEdit(entry.name)"
                  >
                    编辑
                  </button>
                  <button
                    type="button"
                    class="category-sort-tile__action"
                    :disabled="saving || dragBusy || !canMoveEntry(entry, -1)"
                    :aria-label="`将分类${entry.name}前移`"
                    @click="moveEntryBy(entry.name, -1)"
                  >
                    前移
                  </button>
                  <button
                    type="button"
                    class="category-sort-tile__action"
                    :disabled="saving || dragBusy || !canMoveEntry(entry, 1)"
                    :aria-label="`将分类${entry.name}后移`"
                    @click="moveEntryBy(entry.name, 1)"
                  >
                    后移
                  </button>
                </div>

                <button
                  type="button"
                  class="category-sort-tile__grip"
                  :aria-label="`拖动分类${entry.name}排序`"
                  title="按住拖动排序"
                  tabindex="-1"
                  :disabled="saving || editorOpen"
                  @pointerdown="beginDrag($event, entry.name)"
                  @contextmenu="suppressContextMenu"
                >
                  <el-icon><Rank /></el-icon>
                </button>
              </div>
            </TransitionGroup>

            <div v-else class="category-sort-empty" role="status">
              <p>没有显示中的分类。请在下方卡片上勾选“显示”，至少保留一个。</p>
            </div>

            <template v-if="hiddenEntries.length > 0">
              <p class="category-hidden-heading">
                已隐藏 {{ hiddenCount }} 个分类，排在显示分类之后
              </p>
              <TransitionGroup
                tag="div"
                name="category-sort"
                class="category-hidden-grid"
                role="list"
                aria-label="已隐藏的分类，可调整隐藏顺序"
              >
                <div
                  v-for="(entry, index) in hiddenEntries"
                  :key="entry.name"
                  class="category-sort-tile is-hidden"
                  role="listitem"
                  :data-category-card-name="entry.name"
                >
                  <button
                    type="button"
                    class="category-sort-tile__main"
                    :disabled="saving"
                    :aria-label="`${entry.name}，隐藏顺序第 ${index + 1} 位。使用方向键调整顺序`"
                    :aria-describedby="HIDDEN_HINT_ID"
                    @keydown="handleCardKeydown($event, entry.name)"
                  >
                    <span class="category-sort-tile__icon" aria-hidden="true">
                      <el-icon><component :is="categoryIcon(entry.name, entry.icon)" /></el-icon>
                    </span>
                    <span class="category-sort-tile__name" :title="entry.name">
                      {{ entry.name }}
                    </span>
                  </button>

                  <label
                    class="category-sort-tile__toggle"
                    @pointerdown.stop
                  >
                    <input
                      v-model="entry.visible"
                      class="category-sort-tile__checkbox"
                      type="checkbox"
                      :disabled="saving || dragBusy"
                      :aria-label="`${entry.name}显示状态`"
                      @change="handleVisibilityChange(entry)"
                    />
                    <span>{{ entry.visible ? '显示' : '隐藏' }}</span>
                  </label>

                  <div
                    class="category-sort-tile__actions"
                    @pointerdown.stop
                  >
                    <button
                      type="button"
                      class="category-sort-tile__action"
                      :disabled="saving || editorOpen || dragBusy"
                      :aria-label="`编辑分类${entry.name}`"
                      @click="openEdit(entry.name)"
                    >
                      编辑
                    </button>
                    <button
                      type="button"
                      class="category-sort-tile__action"
                      :disabled="saving || dragBusy || !canMoveEntry(entry, -1)"
                      :aria-label="`将分类${entry.name}前移`"
                      @click="moveEntryBy(entry.name, -1)"
                    >
                      前移
                    </button>
                    <button
                      type="button"
                      class="category-sort-tile__action"
                      :disabled="saving || dragBusy || !canMoveEntry(entry, 1)"
                      :aria-label="`将分类${entry.name}后移`"
                      @click="moveEntryBy(entry.name, 1)"
                    >
                      后移
                    </button>
                  </div>
                </div>
              </TransitionGroup>
              <p :id="HIDDEN_HINT_ID" class="category-hidden-hint">
                隐藏分类不会出现在记账候选中，也不能拖动；可用前移、后移调整隐藏组内的相对顺序。
              </p>
            </template>
          </div>
        </div>
      </div>

      <p class="sr-only" aria-live="polite">{{ liveMessage }}</p>
    </div>

    <template v-if="!isCreating" #footer>
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
              'is-warning': editorOpen || visibleCount === 0,
              'is-dragging': dragName !== null,
            }"
          >
            <template v-if="saving">正在保存分类设置…</template>
            <template v-else-if="editorOpen">
              请先应用或取消正在编辑的分类，再保存设置
            </template>
            <template v-else-if="dragName">
              正在拖动「{{ dragName }}」，松手放到新位置
            </template>
            <template v-else-if="visibleCount === 0">
              请至少保留一个显示的分类
            </template>
            <template v-else>
              共 {{ draft.length }} 个分类，显示 {{ visibleCount }} 个<span
                v-if="hiddenCount > 0"
              >，隐藏 {{ hiddenCount }} 个</span>
            </template>
          </p>
          <div class="dialog-footer category-settings-footer__actions">
            <el-button
              class="category-cancel-button"
              :disabled="saving"
              @click="closeDialog"
            >
              取消
            </el-button>
            <el-button
              class="category-save-button"
              type="primary"
              :loading="saving"
              :disabled="dragBusy || editorOpen"
              @click="submit"
            >
              保存设置
            </el-button>
          </div>
        </div>
      </div>
    </template>
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
  padding: 16px calc(20px + env(safe-area-inset-right, 0px)) 0
    calc(20px + env(safe-area-inset-left, 0px));
  overflow: hidden;
}

:global(.el-dialog.category-settings-dialog.is-creating .el-dialog__body) {
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

.category-settings-header.is-creating {
  max-width: none;
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

.category-settings-header__close:hover:not(:disabled),
.category-settings-header__add:hover:not(:disabled) {
  color: var(--hb-ink);
  border-color: var(--hb-line-strong);
}

.category-settings-header__close:disabled,
.category-settings-header__add:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

.category-settings-header__add {
  margin-left: auto;
  color: var(--hb-teal-dark);
  background: var(--hb-teal-faint);
  border-color: var(--hb-teal-soft);
}

.category-settings-header__add:hover:not(:disabled) {
  color: var(--hb-teal-dark);
  border-color: var(--hb-teal);
}

.category-settings-header__add:focus-visible {
  outline: 2px solid var(--hb-teal);
  outline-offset: 2px;
}


.category-settings {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  width: 100%;
  max-width: 1180px;
  min-height: 0;
  margin: 0 auto;
}

.category-settings.is-creating {
  max-width: none;
}


.category-workspace {
  display: grid;
  flex: 1 1 auto;
  grid-template-columns: minmax(0, 1fr);
  grid-template-rows: minmax(0, 1fr);
  gap: 14px;
  min-height: 0;
}

.category-workspace.has-editor {
  grid-template-rows: minmax(0, 42vh) minmax(0, 1fr);
}

.category-workspace.is-creating {
  grid-template-columns: minmax(0, 1fr);
  grid-template-rows: minmax(0, 1fr);
}

.category-editor-panel {
  min-width: 0;
  min-height: 0;
  padding: 14px;
  overflow-y: auto;
  overscroll-behavior: contain;
  background: var(--hb-surface-raised);
  border: 1px solid var(--hb-line);
  border-radius: 18px;
  scrollbar-gutter: stable;
}

.category-list-column {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
}





.category-sort-scroll {
  flex: 1 1 auto;
  min-height: 0;
  padding: 2px 4px 24px;
  overflow-y: auto;
  overscroll-behavior: contain;
  touch-action: pan-y;
  scrollbar-gutter: stable;
}

.category-sort-grid,
.category-hidden-grid {
  position: relative;
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(118px, 1fr));
  gap: 10px;
  padding: 2px;
}

.category-sort-tile {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
  min-height: 168px;
  padding: 10px 8px 8px;
  background: var(--hb-bg);
  border: 1px solid var(--hb-line);
  border-radius: 16px;
  transition:
    background-color 0.16s ease,
    border-color 0.16s ease,
    box-shadow 0.16s ease,
    scale 0.16s ease;
}

.category-sort-tile:hover {
  background: var(--hb-surface-raised);
  border-color: var(--hb-line-strong);
}

.category-sort-tile.is-hidden {
  background: var(--hb-surface);
  border-style: dashed;
}

.category-sort-tile.is-dragging {
  z-index: 1;
  background: var(--hb-teal-faint);
  border-color: var(--hb-teal);
  box-shadow: 0 12px 30px rgba(10, 93, 87, 0.18);
  scale: 1.03;
}

.category-sort-move {
  transition:
    transform 0.22s cubic-bezier(0.2, 0, 0, 1),
    background-color 0.16s ease,
    border-color 0.16s ease,
    box-shadow 0.16s ease,
    scale 0.16s ease;
}

.category-sort-tile__main {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  gap: 6px;
  align-items: center;
  justify-content: center;
  min-width: 0;
  min-height: 62px;
  padding: 16px 2px 6px;
  color: var(--hb-ink-soft);
  font: inherit;
  font-size: 12.5px;
  line-height: 1.2;
  cursor: grab;
  background: transparent;
  border: 0;
  border-radius: 10px;
  appearance: none;
  -webkit-tap-highlight-color: transparent;
}

.category-sort-tile.is-hidden .category-sort-tile__main {
  cursor: default;
}

.category-sort-tile__main:focus-visible {
  outline: 2px solid var(--hb-teal);
  outline-offset: 2px;
}

.category-sort-tile__main:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.category-sort-tile__icon {
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--hb-teal);
}

.category-sort-tile.is-hidden .category-sort-tile__icon {
  color: var(--hb-ink-faint);
}

.category-sort-tile__icon :deep(.el-icon) {
  font-size: 26px;
}

.category-sort-tile__name {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.category-sort-tile__toggle {
  display: inline-flex;
  gap: 5px;
  align-items: center;
  justify-content: center;
  min-height: 24px;
  color: var(--hb-ink-faint);
  font-size: 10.5px;
  line-height: 1;
  cursor: pointer;
  -webkit-user-select: none;
  user-select: none;
}

.category-sort-tile__checkbox {
  width: 16px;
  height: 16px;
  margin: 0;
  accent-color: var(--hb-teal);
  cursor: pointer;
}

.category-sort-tile__checkbox:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

.category-sort-tile:has(.category-sort-tile__checkbox:focus-visible) {
  outline: 2px solid var(--hb-teal);
  outline-offset: 2px;
}

.category-sort-tile__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 3px;
  justify-content: center;
  margin-top: auto;
}

.category-sort-tile__action {
  min-height: 24px;
  padding: 0 5px;
  color: var(--hb-ink-soft);
  font: inherit;
  font-size: 10.5px;
  line-height: 1;
  white-space: nowrap;
  cursor: pointer;
  background: var(--hb-surface);
  border: 1px solid var(--hb-line);
  border-radius: 7px;
  -webkit-tap-highlight-color: transparent;
}

.category-sort-tile__action:hover:not(:disabled) {
  color: var(--hb-teal-dark);
  border-color: var(--hb-teal);
}

.category-sort-tile__action:focus-visible {
  outline: 2px solid var(--hb-teal);
  outline-offset: 1px;
}

.category-sort-tile__action:disabled {
  cursor: not-allowed;
  opacity: 0.45;
}

.category-sort-tile__grip {
  position: absolute;
  top: 3px;
  right: 3px;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  padding: 0;
  color: var(--hb-ink-faint);
  font-size: 15px;
  cursor: grab;
  touch-action: none;
  -webkit-user-select: none;
  user-select: none;
  -webkit-touch-callout: none;
  background: transparent;
  border: 1px solid transparent;
  border-radius: 8px;
  -webkit-tap-highlight-color: transparent;
}

.category-sort-tile__grip:hover:not(:disabled) {
  color: var(--hb-ink-soft);
  background: var(--hb-bg-deep);
  border-color: var(--hb-line);
}

.category-sort-tile__grip:active:not(:disabled),
.category-sort-tile.is-dragging .category-sort-tile__grip {
  cursor: grabbing;
  color: var(--hb-teal);
}

.category-sort-tile__grip:disabled {
  cursor: not-allowed;
  opacity: 0.45;
}

.category-sort-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 160px;
  color: var(--hb-ink-faint);
  font-size: 13px;
  text-align: center;
}

.category-sort-empty p {
  max-width: 420px;
  margin: 0;
  line-height: 1.7;
}

.category-hidden-heading {
  margin: 16px 0 8px;
  padding-top: 12px;
  color: var(--hb-ink-soft);
  font-size: 11.5px;
  font-weight: 500;
  line-height: 1.6;
  border-top: 1px dashed var(--hb-line);
}

.category-hidden-hint {
  margin: 8px 0 0;
  color: var(--hb-ink-faint);
  font-size: 11px;
  line-height: 1.6;
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

.category-create-error {
  flex: 0 0 auto;
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
  line-height: 1.5;
}

.category-settings-footer__status.is-warning {
  color: var(--hb-danger);
}

.category-settings-footer__status.is-dragging {
  color: var(--hb-teal);
  font-weight: 600;
}

.category-settings-footer__actions {
  flex: 0 0 auto;
}

@media (min-width: 900px) {
  .category-workspace,
  .category-workspace.has-editor {
    grid-template-rows: minmax(0, 1fr);
  }

  .category-workspace {
    grid-template-columns: minmax(0, 1fr);
  }

  .category-workspace.has-editor {
    grid-template-columns: minmax(300px, 380px) minmax(0, 1fr);
  }
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

  :global(.el-dialog.category-settings-dialog.is-creating .el-dialog__body) {
    padding-bottom: calc(10px + env(safe-area-inset-bottom, 0px));
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


  .category-workspace {
    gap: 10px;
  }

  .category-workspace.has-editor {
    grid-template-rows: minmax(0, 44vh) minmax(0, 1fr);
  }

  .category-editor-panel {
    padding: 12px;
    border-radius: 14px;
  }

  .category-sort-grid,
  .category-hidden-grid {
    grid-template-columns: repeat(auto-fill, minmax(96px, 1fr));
    gap: 8px;
  }

  .category-sort-tile {
    gap: 5px;
    min-height: 156px;
    padding: 8px 5px 6px;
    border-radius: 14px;
  }

  .category-sort-tile__main {
    gap: 5px;
    min-height: 58px;
    padding: 14px 2px 4px;
    font-size: 11.5px;
  }

  .category-sort-tile__icon :deep(.el-icon) {
    font-size: 24px;
  }

  .category-sort-tile__toggle {
    font-size: 10px;
  }

  .category-sort-tile__checkbox {
    width: 15px;
    height: 15px;
  }

  .category-sort-tile__actions {
    gap: 2px;
  }

  .category-sort-tile__action {
    min-height: 23px;
    padding: 0 4px;
    font-size: 10px;
  }

  .category-sort-tile__grip {
    top: 1px;
    right: 1px;
    width: 28px;
    height: 28px;
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
}
</style>
