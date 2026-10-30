<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  ref,
  watch,
  type ComponentPublicInstance,
} from 'vue'
import { Close, Lock, Rank, Sort, View } from '@element-plus/icons-vue'
import { authState } from '../stores/auth'
import {
  readCategoryEntries,
  saveCategoryEntries,
  type CategoryEntry,
  type CategoryKind,
} from '../stores/categories'
import { categoryIcon } from '../utils/categoryIcons'

const props = defineProps<{
  modelValue: boolean
  kind: CategoryKind
  currentCategory?: string
}>()

const emit = defineEmits<{
  (event: 'update:modelValue', value: boolean): void
  (event: 'saved', selectedCategory?: string): void
}>()

const MAX_CATEGORY_LENGTH = 64
const DRAG_THRESHOLD = 6
const DRAG_EDGE_SIZE = 64
const DRAG_MAX_SCROLL_SPEED = 18
const SORT_HINT_ID = 'category-sort-hint'
const KEY_DELTAS: Record<string, number> = {
  ArrowUp: -1,
  ArrowDown: 1,
  ArrowLeft: -1,
  ArrowRight: 1,
}

type SettingsMode = 'sort' | 'manage'

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
const newCategory = ref('')
const addedNames = ref<string[]>([])
const addError = ref('')
const errorMessage = ref('')
const saving = ref(false)
const mode = ref<SettingsMode>('sort')
const dragName = ref<string | null>(null)
const liveMessage = ref('')
const gridScrollRef = ref<HTMLElement | null>(null)
const gridGroupRef = ref<ComponentPublicInstance | null>(null)
const gridRef = computed<HTMLElement | null>(() => {
  const element = gridGroupRef.value?.$el
  return element instanceof HTMLElement ? element : null
})
const manageListRef = ref<HTMLElement | null>(null)

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
const modeToggleText = computed(() =>
  mode.value === 'sort' ? '显示/隐藏' : '返回排序',
)
const modeToggleAria = computed(() =>
  mode.value === 'sort'
    ? '进入显示和隐藏分类面板'
    : '返回图标排序面板',
)

function currentScope(): string | null {
  const userId = authState.user?.id
  const householdId = authState.household?.id
  if (userId === undefined || householdId === undefined) return null
  return `${householdId}:${userId}`
}

function copyEntries(entries: CategoryEntry[]): CategoryEntry[] {
  return entries.map((entry) => ({ ...entry }))
}

function loadDraft(): void {
  cancelDrag(false)
  openedScope = currentScope()
  draft.value = readCategoryEntries(props.kind, props.currentCategory)
  newCategory.value = ''
  addError.value = ''
  errorMessage.value = ''
  addedNames.value = []
  mode.value = 'sort'
  liveMessage.value = ''
}

function clearTransientState(): void {
  cancelDrag(false)
  newCategory.value = ''
  addError.value = ''
  errorMessage.value = ''
  addedNames.value = []
  draft.value = []
  mode.value = 'sort'
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
    cancelDrag(false)
    emit('update:modelValue', false)
  }
})

onBeforeUnmount(() => {
  cancelDrag(false)
})

function closeDialog(): void {
  cancelDrag(false)
  emit('update:modelValue', false)
}

function handleDialogClosed(): void {
  cancelDrag(false)
}

function toggleMode(): void {
  if (saving.value) return
  cancelDrag(true)
  mode.value = mode.value === 'sort' ? 'manage' : 'sort'
  liveMessage.value =
    mode.value === 'manage'
      ? '已打开显示和隐藏分类面板'
      : '已返回图标排序面板'
}

function clearErrors(): void {
  addError.value = ''
  errorMessage.value = ''
}

function addCategory(): void {
  addError.value = ''
  const name = newCategory.value.trim()
  if (!name) {
    addError.value = '请输入分类名称'
    return
  }
  if (name.includes('\u0000')) {
    addError.value = '分类名称不能包含空字符'
    return
  }
  if (Array.from(name).length > MAX_CATEGORY_LENGTH) {
    addError.value = `分类名称最多 ${MAX_CATEGORY_LENGTH} 个字符`
    return
  }
  if (draft.value.some((entry) => entry.name === name)) {
    addError.value = '该分类已存在，可在面板中调整其显示状态'
    return
  }

  draft.value = [...draft.value, { name, visible: true }]
  addedNames.value = [...addedNames.value, name]
  newCategory.value = ''
  errorMessage.value = ''
  liveMessage.value = `已添加分类${name}，当前为显示状态`
  void nextTick(() => {
    const list = manageListRef.value
    if (list) list.scrollTop = list.scrollHeight
  })
}

function handleAddKeydown(event: Event | KeyboardEvent): void {
  if (!('isComposing' in event) || event.isComposing) return
  event.preventDefault()
  event.stopPropagation()
  addCategory()
}

function handleVisibilityChange(entry: CategoryEntry): void {
  addError.value = ''
  errorMessage.value = ''
  liveMessage.value = entry.visible
    ? `已显示分类${entry.name}`
    : `已隐藏分类${entry.name}，历史账目不会删除`
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

function handleTileKeydown(event: KeyboardEvent, name: string): void {
  if (saving.value || dragSession || mode.value !== 'sort') return
  if (event.altKey || event.ctrlKey || event.metaKey) return
  const delta = KEY_DELTAS[event.key]
  if (delta === undefined) return
  event.preventDefault()
  const entries = visibleEntries.value
  const index = entries.findIndex((entry) => entry.name === name)
  if (index < 0) return
  const target = entries[index + delta]
  if (!target) return
  if (!moveEntryToName(name, target.name)) return
  const nextIndex = visibleEntries.value.findIndex(
    (entry) => entry.name === name,
  )
  liveMessage.value = `已将分类${name}移动到显示顺序第 ${nextIndex + 1} 位`
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
}

function cancelDrag(restore = true): void {
  const session = dragSession
  if (!session) {
    dragName.value = null
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
  // Mouse can drag from the icon surface; touch uses the dedicated grip so
  // the grid itself stays scrollable by finger.
  if (event.pointerType !== 'mouse') return
  beginDrag(event, name)
}

function beginDrag(event: PointerEvent, name: string): void {
  if (saving.value || mode.value !== 'sort') return
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

function submit(): void {
  if (saving.value) return
  if (dragSession) cancelDrag(true)
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
      draft.value.map((entry) => ({ ...entry })),
    )
    const selected = [...addedNames.value]
      .reverse()
      .find((name) =>
        draft.value.some((entry) => entry.name === name && entry.visible),
      )
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
    fullscreen
    append-to-body
    :close-on-click-modal="!saving"
    :close-on-press-escape="!saving"
    :show-close="false"
    @closed="handleDialogClosed"
  >
    <template #header>
      <div class="category-settings-header">
        <button
          type="button"
          class="category-settings-header__close"
          aria-label="关闭分类设置"
          title="关闭"
          :disabled="saving"
          @click="closeDialog"
        >
          <el-icon><Close /></el-icon>
        </button>
        <div class="category-settings-header__heading">
          <span class="category-settings-header__title">{{ dialogTitle }}</span>
          <span class="category-settings-header__subtitle">
            {{ mode === 'sort' ? '拖动图标调整顺序' : '管理分类的显示与隐藏' }}
          </span>
        </div>
        <button
          type="button"
          class="category-settings-header__mode category-settings-mode-toggle"
          :aria-label="modeToggleAria"
          :title="modeToggleText"
          :disabled="saving"
          @click="toggleMode"
        >
          <el-icon>
            <component :is="mode === 'sort' ? View : Sort" />
          </el-icon>
          <span>{{ modeToggleText }}</span>
        </button>
      </div>
    </template>

    <div class="category-settings" :class="`category-settings--${mode}`">
      <p class="category-settings__lead">
        拖动分类图标可调整{{ kindLabel }}记账时的候选顺序。主网格只显示当前显示的分类。
      </p>

      <div class="category-settings__notice" role="note">
        <el-icon class="category-settings__notice-icon"><Lock /></el-icon>
        <span>
          设置仅保存在本机当前家庭账号下，不会同步到其他设备。隐藏分类只会从候选里移出，不会删除历史账目，也不会修改历史分类。
        </span>
      </div>

      <template v-if="mode === 'sort'">
        <div class="category-sort-toolbar">
          <span class="category-visible-count">
            显示 {{ visibleCount }} 个<span v-if="hiddenCount > 0">
              · 隐藏 {{ hiddenCount }} 个
            </span>
          </span>
          <p :id="SORT_HINT_ID" class="category-sort-hint">
            按住图标右上角的手柄拖动排序；键盘聚焦分类后可用方向键调整顺序。
          </p>
        </div>

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
              @pointerdown="beginTileDrag($event, entry.name)"
            >
              <button
                type="button"
                class="category-sort-tile__main"
                :disabled="saving"
                :aria-label="`${entry.name}，显示顺序第 ${index + 1} 位。使用方向键调整顺序`"
                :aria-describedby="SORT_HINT_ID"
                @keydown="handleTileKeydown($event, entry.name)"
              >
                <span class="category-sort-tile__icon" aria-hidden="true">
                  <el-icon><component :is="categoryIcon(entry.name)" /></el-icon>
                </span>
                <span class="category-sort-tile__name" :title="entry.name">
                  {{ entry.name }}
                </span>
              </button>
              <button
                type="button"
                class="category-sort-tile__grip"
                :aria-label="`拖动分类${entry.name}排序`"
                title="按住拖动排序"
                tabindex="-1"
                :disabled="saving"
                :data-category-sort-handle="entry.name"
                @pointerdown="beginDrag($event, entry.name)"
                @contextmenu="suppressContextMenu"
              >
                <el-icon><Rank /></el-icon>
              </button>
            </div>
          </TransitionGroup>

          <div v-else class="category-sort-empty" role="status">
            <p>没有显示中的分类。</p>
            <el-button
              class="category-sort-empty-action"
              type="primary"
              plain
              :disabled="saving"
              @click="toggleMode"
            >
              去显示/隐藏中开启
            </el-button>
          </div>
        </div>
      </template>

      <template v-else>
        <section class="category-add-block" aria-labelledby="category-add-title">
          <div class="category-add-head">
            <span id="category-add-title">新增自定义分类</span>
            <span>最多 {{ MAX_CATEGORY_LENGTH }} 个字符</span>
          </div>
          <div class="category-add">
            <el-input
              v-model="newCategory"
              class="category-name-input"
              size="large"
              clearable
              placeholder="输入新的分类名称"
              aria-label="新分类名称"
              @input="clearErrors"
              @keydown.enter="handleAddKeydown"
            />
            <el-button
              class="category-add-button"
              type="primary"
              plain
              size="large"
              :disabled="saving"
              @click="addCategory"
            >
              添加
            </el-button>
          </div>
          <p v-if="addError" class="category-add-error" role="alert">
            {{ addError }}
          </p>
        </section>

        <div class="category-manage-head">
          <span class="category-visible-count">
            全部 {{ draft.length }} 个分类，显示 {{ visibleCount }} 个
          </span>
          <span>隐藏分类不会删除历史账目</span>
        </div>

        <ul
          ref="manageListRef"
          class="category-manage-list"
          aria-label="全部已添加分类的显示设置"
        >
          <li
            v-for="entry in draft"
            :key="entry.name"
            class="category-manage-row"
            :class="{ 'is-hidden': !entry.visible }"
            :data-category-manage-name="entry.name"
          >
            <span class="category-manage-row__icon" aria-hidden="true">
              <el-icon><component :is="categoryIcon(entry.name)" /></el-icon>
            </span>
            <span class="category-manage-row__copy">
              <span class="category-manage-row__name" :title="entry.name">
                {{ entry.name }}
              </span>
              <span class="category-manage-row__state">
                {{ entry.visible ? '显示在记账页' : '已隐藏，历史账目不受影响' }}
              </span>
            </span>
            <el-switch
              v-model="entry.visible"
              class="category-visibility-switch"
              size="large"
              inline-prompt
              active-text="显示"
              inactive-text="隐藏"
              :disabled="saving"
              :aria-label="`${entry.name}显示状态`"
              @change="handleVisibilityChange(entry)"
            />
          </li>
        </ul>
      </template>

      <p class="sr-only" aria-live="polite">{{ liveMessage }}</p>
    </div>

    <template #footer>
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
              'is-warning': visibleCount === 0,
              'is-dragging': dragName !== null,
            }"
          >
            <template v-if="dragName">
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
              :disabled="dragName !== null"
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
  max-width: 980px;
  margin: 0 auto;
}

.category-settings-header__close {
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

.category-settings-header__close:hover:not(:disabled) {
  color: var(--hb-ink);
  border-color: var(--hb-line-strong);
}

.category-settings-header__close:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

.category-settings-header__heading {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.category-settings-header__title {
  overflow: hidden;
  color: var(--hb-ink);
  font-size: 19px;
  font-weight: 600;
  line-height: 1.25;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.category-settings-header__subtitle {
  overflow: hidden;
  color: var(--hb-ink-faint);
  font-size: 11.5px;
  line-height: 1.4;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.category-settings-header__mode {
  display: inline-flex;
  flex: 0 0 auto;
  gap: 7px;
  align-items: center;
  justify-content: center;
  min-height: 40px;
  padding: 0 14px;
  color: var(--hb-teal);
  font: inherit;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  background: var(--hb-teal-faint);
  border: 1px solid var(--hb-teal-soft);
  border-radius: 999px;
  -webkit-tap-highlight-color: transparent;
  transition:
    color 0.16s ease,
    background-color 0.16s ease,
    border-color 0.16s ease;
}

.category-settings-header__mode:hover:not(:disabled) {
  color: #fff;
  background: var(--hb-teal);
  border-color: var(--hb-teal);
}

.category-settings-header__mode:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.category-settings {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  width: 100%;
  max-width: 980px;
  min-height: 0;
  margin: 0 auto;
}

.category-settings__lead {
  flex: 0 0 auto;
  margin: 0 0 12px;
  color: var(--hb-ink-soft);
  font-size: 12.5px;
  line-height: 1.7;
}

.category-settings__notice {
  display: flex;
  flex: 0 0 auto;
  gap: 8px;
  align-items: flex-start;
  margin-bottom: 14px;
  padding: 10px 12px;
  color: var(--hb-ink-faint);
  font-size: 11.5px;
  line-height: 1.7;
  background: var(--hb-teal-faint);
  border: 1px solid var(--hb-teal-soft);
  border-radius: var(--hb-radius-sm);
}

.category-settings__notice-icon {
  flex: 0 0 auto;
  margin-top: 2px;
  color: var(--hb-teal);
}

.category-sort-toolbar {
  display: flex;
  flex: 0 0 auto;
  flex-wrap: wrap;
  gap: 4px 16px;
  align-items: baseline;
  justify-content: space-between;
  margin-bottom: 8px;
  color: var(--hb-ink-faint);
  font-size: 11.5px;
}

.category-visible-count {
  color: var(--hb-ink-soft);
  font-weight: 500;
}

.category-sort-hint {
  max-width: 560px;
  margin: 0;
  line-height: 1.6;
  text-align: right;
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

.category-sort-grid {
  position: relative;
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(100px, 1fr));
  gap: 12px;
  padding: 2px;
}

.category-sort-tile {
  position: relative;
  display: flex;
  min-width: 0;
  min-height: 104px;
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

.category-sort-tile.is-dragging .category-sort-tile__main {
  cursor: grabbing;
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
  gap: 8px;
  align-items: center;
  justify-content: center;
  min-width: 0;
  padding: 22px 8px 12px;
  color: var(--hb-ink-soft);
  font: inherit;
  font-size: 12.5px;
  line-height: 1.2;
  cursor: grab;
  background: transparent;
  border: 0;
  border-radius: inherit;
  appearance: none;
  -webkit-tap-highlight-color: transparent;
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

.category-sort-tile__icon :deep(.el-icon) {
  font-size: 28px;
}

.category-sort-tile__name {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.category-sort-tile__grip {
  position: absolute;
  top: 6px;
  right: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 34px;
  height: 34px;
  padding: 0;
  color: var(--hb-ink-faint);
  font-size: 16px;
  cursor: grab;
  touch-action: none;
  -webkit-user-select: none;
  user-select: none;
  -webkit-touch-callout: none;
  background: transparent;
  border: 1px solid transparent;
  border-radius: 9px;
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
  flex-direction: column;
  gap: 12px;
  align-items: center;
  justify-content: center;
  min-height: 220px;
  color: var(--hb-ink-faint);
  font-size: 13px;
  text-align: center;
}

.category-sort-empty p {
  margin: 0;
}

.category-settings--manage {
  min-height: 0;
}

.category-add-block {
  flex: 0 0 auto;
  margin-bottom: 4px;
}

.category-add-head {
  display: flex;
  gap: 12px;
  align-items: baseline;
  justify-content: space-between;
  margin-bottom: 6px;
  color: var(--hb-ink-soft);
  font-size: 12px;
  font-weight: 500;
}

.category-add-head span:last-child {
  flex: 0 0 auto;
  color: var(--hb-ink-faint);
  font-size: 11px;
  font-weight: 400;
}

.category-add {
  display: flex;
  gap: 8px;
}

.category-add .el-input {
  flex: 1;
  min-width: 0;
}

.category-add .el-button {
  flex: 0 0 auto;
  min-height: 40px;
}

.category-add-error {
  margin-top: 6px;
  color: var(--hb-danger);
  font-size: 12px;
}

.category-manage-head {
  display: flex;
  flex: 0 0 auto;
  flex-wrap: wrap;
  gap: 4px 12px;
  align-items: baseline;
  justify-content: space-between;
  margin: 14px 0 8px;
  color: var(--hb-ink-faint);
  font-size: 11.5px;
}

.category-manage-list {
  flex: 1 1 auto;
  min-height: 0;
  margin: 0;
  padding: 0 2px 18px;
  overflow-y: auto;
  overscroll-behavior: contain;
  list-style: none;
}

.category-manage-row {
  display: flex;
  gap: 12px;
  align-items: center;
  min-height: 62px;
  margin-bottom: 8px;
  padding: 10px 12px;
  background: var(--hb-surface-raised);
  border: 1px solid var(--hb-line);
  border-radius: 14px;
  transition:
    opacity 0.16s ease,
    background-color 0.16s ease;
}

.category-manage-row:last-child {
  margin-bottom: 0;
}

.category-manage-row.is-hidden {
  background: var(--hb-bg);
  opacity: 0.78;
}

.category-manage-row__icon {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  width: 40px;
  height: 40px;
  color: var(--hb-teal);
  background: var(--hb-bg);
  border: 1px solid var(--hb-line);
  border-radius: 11px;
}

.category-manage-row__icon :deep(.el-icon) {
  font-size: 20px;
}

.category-manage-row.is-hidden .category-manage-row__icon {
  color: var(--hb-ink-faint);
}

.category-manage-row__copy {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.category-manage-row__name {
  overflow: hidden;
  color: var(--hb-ink);
  font-size: 13.5px;
  font-weight: 500;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.category-manage-row__state {
  overflow: hidden;
  color: var(--hb-ink-faint);
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.category-manage-row :deep(.el-switch) {
  flex: 0 0 auto;
}

.category-manage-row :deep(.el-switch__core) {
  min-width: 58px;
  height: 26px;
  border-radius: 13px;
}

.category-manage-row :deep(.el-switch__core .el-switch__inner) {
  height: 22px;
  padding: 0 6px 0 22px;
}

.category-manage-row :deep(.el-switch__action) {
  width: 22px;
  height: 22px;
}

.category-settings-footer {
  width: 100%;
  max-width: 980px;
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

  .category-settings-header {
    gap: 8px;
  }

  .category-settings-header__close {
    width: 38px;
    height: 38px;
  }

  .category-settings-header__mode {
    min-height: 38px;
    padding: 0 11px;
    font-size: 12px;
  }

  .category-settings__lead {
    margin-bottom: 8px;
    font-size: 12px;
    line-height: 1.6;
  }

  .category-settings__notice {
    margin-bottom: 10px;
    padding: 8px 10px;
    font-size: 11px;
    line-height: 1.6;
  }

  .category-sort-hint {
    max-width: none;
    text-align: left;
  }

  .category-sort-grid {
    grid-template-columns: repeat(auto-fill, minmax(82px, 1fr));
    gap: 8px;
  }

  .category-sort-tile {
    min-height: 92px;
    border-radius: 14px;
  }

  .category-sort-tile__main {
    gap: 6px;
    padding: 18px 4px 9px;
    font-size: 11.5px;
  }

  .category-sort-tile__icon :deep(.el-icon) {
    font-size: 24px;
  }

  .category-sort-tile__grip {
    top: 2px;
    right: 2px;
    width: 32px;
    height: 32px;
  }

  .category-manage-row {
    gap: 9px;
    min-height: 58px;
    padding: 8px 10px;
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
