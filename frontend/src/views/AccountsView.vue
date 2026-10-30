<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  EditPen,
  Plus,
  Rank,
  RefreshRight,
  Wallet,
  WarningFilled,
} from '@element-plus/icons-vue'
import AccountDialog from '../components/AccountDialog.vue'
import EmptyState from '../components/EmptyState.vue'
import { listAccounts } from '../api/accounts'
import { getErrorMessage } from '../api/client'
import type { Account } from '../api/types'
import { authState } from '../stores/auth'
import {
  orderAccounts,
  readAccountOrder,
  saveAccountOrder,
} from '../stores/accountOrder'
import { dataVersion } from '../stores/data'
import { formatMoney } from '../utils/format'

const route = useRoute()
const router = useRouter()

const accounts = ref<Account[]>([])
const loading = ref(true)
const errorMessage = ref('')

const dialogVisible = ref(false)
const editingAccount = ref<Account | null>(null)

const totalBalance = computed(() =>
  accounts.value.reduce((sum, account) => sum + Number(account.balance || 0), 0),
)

type AccountGroupKey = 'personal' | 'public' | 'other'

interface AccountGroupDefinition {
  key: AccountGroupKey
  title: string
  hint: string
  emptyHint: string
}

interface AccountGroup extends AccountGroupDefinition {
  accounts: Account[]
}

const GROUP_DEFINITIONS: readonly AccountGroupDefinition[] = [
  {
    key: 'personal',
    title: '个人账户',
    hint: '当前登录账号名下',
    emptyHint: '暂无个人账户。',
  },
  {
    key: 'public',
    title: '公共账户',
    hint: '全家共用',
    emptyHint: '暂无公共账户。未归属到具体成员的账户属于公共账户。',
  },
  {
    key: 'other',
    title: '他人账户',
    hint: '其他成员或历史归属',
    emptyHint: '暂无他人账户。其他成员名下或历史归属的账户会显示在这里。',
  },
]

const orderIds = ref<number[]>([])
const orderError = ref('')
const liveMessage = ref('')

const currentUserId = computed<number | null>(() => authState.user?.id ?? null)
const orderScope = computed(() => {
  const userId = authState.user?.id
  const householdId = authState.household?.id
  return userId === undefined || householdId === undefined
    ? ''
    : `h${householdId}.u${userId}`
})

const orderedAccounts = computed(() =>
  orderAccounts(accounts.value, orderIds.value),
)

const accountGroups = computed<AccountGroup[]>(() => {
  const buckets: Record<AccountGroupKey, Account[]> = {
    personal: [],
    public: [],
    other: [],
  }
  for (const account of orderedAccounts.value) {
    buckets[classifyAccount(account)].push(account)
  }
  return GROUP_DEFINITIONS.map((definition) => ({
    ...definition,
    accounts: buckets[definition.key],
  }))
})

function classifyAccount(account: Account): AccountGroupKey {
  const userId = currentUserId.value
  if (userId !== null && account.owner === userId) return 'personal'
  if (account.owner === null) return 'public'
  return 'other'
}

function accountsInGroup(key: AccountGroupKey): Account[] {
  return accountGroups.value.find((group) => group.key === key)?.accounts ?? []
}

function groupTitle(key: AccountGroupKey): string {
  return (
    GROUP_DEFINITIONS.find((definition) => definition.key === key)?.title ??
    '账户'
  )
}

function syncOrderFromStorage(): void {
  try {
    orderIds.value = readAccountOrder()
  } catch {
    orderIds.value = []
  }
}

/**
 * Builds the next persisted ID order by moving one account inside its current
 * group. The flattened result always follows personal → public → other, and
 * saves every currently visible account so stale IDs are dropped.
 */
function nextOrderForGroupMove(
  key: AccountGroupKey,
  accountId: number,
  targetIndex: number,
): number[] | null {
  const groupsNow = accountGroups.value
  const group = groupsNow.find((item) => item.key === key)
  if (!group) return null
  const ids = group.accounts.map((account) => account.id)
  const fromIndex = ids.indexOf(accountId)
  if (fromIndex < 0) return null
  const toIndex = Math.max(0, Math.min(targetIndex, ids.length - 1))
  if (fromIndex === toIndex) return null
  const nextIds = [...ids]
  nextIds.splice(fromIndex, 1)
  nextIds.splice(toIndex, 0, accountId)
  return groupsNow.flatMap((item) =>
    item.key === key ? nextIds : item.accounts.map((account) => account.id),
  )
}

function sameOrder(left: readonly number[], right: readonly number[]): boolean {
  return (
    left.length === right.length &&
    left.every((value, index) => value === right[index])
  )
}

const DRAG_THRESHOLD = 6
const DRAG_EDGE_SIZE = 64
const DRAG_MAX_SCROLL_SPEED = 18
const KEY_DELTAS: Record<string, number> = {
  ArrowUp: -1,
  ArrowDown: 1,
  ArrowLeft: -1,
  ArrowRight: 1,
}

interface LayoutSlot {
  left: number
  top: number
  width: number
  height: number
}

interface AccountDragSession {
  pointerId: number
  group: AccountGroupKey
  accountId: number
  name: string
  scope: string
  startX: number
  startY: number
  lastX: number
  lastY: number
  active: boolean
  snapshot: number[]
  captureTarget: HTMLElement
  scroller: HTMLElement | null
  rafId: number | null
  slots: LayoutSlot[]
}

const dragId = ref<number | null>(null)
const dragBusy = ref(false)
const groupElements: Partial<Record<AccountGroupKey, HTMLElement>> = {}
let dragSession: AccountDragSession | null = null

function setGroupElement(key: AccountGroupKey, element: unknown): void {
  if (element instanceof HTMLElement) {
    groupElements[key] = element
  } else {
    delete groupElements[key]
  }
}

function persistOrder(
  next: readonly number[],
  rollback: readonly number[],
  successMessage: string,
): boolean {
  try {
    saveAccountOrder(next)
    orderError.value = ''
    liveMessage.value = successMessage
    return true
  } catch (error) {
    orderIds.value = [...rollback]
    const message =
      error instanceof Error && error.message
        ? error.message
        : '账户顺序无法保存到本机，请稍后重试'
    orderError.value = message
    liveMessage.value = message
    return false
  }
}

function gripLabel(
  group: AccountGroup,
  account: Account,
  index: number,
): string {
  return `${account.name}，${group.title}第 ${index + 1} 位，共 ${group.accounts.length} 个账户。按方向键调整顺序，或按住拖动`
}

function handleGripKeydown(
  event: KeyboardEvent,
  key: AccountGroupKey,
  account: Account,
  index: number,
): void {
  if (event.altKey || event.ctrlKey || event.metaKey) return
  const delta = KEY_DELTAS[event.key]
  if (delta === undefined || dragSession) return
  event.preventDefault()
  const groupAccounts = accountsInGroup(key)
  const targetIndex = index + delta
  if (targetIndex < 0 || targetIndex >= groupAccounts.length) {
    liveMessage.value = `${account.name}已经在${groupTitle(key)}第 ${index + 1} 位`
    return
  }
  const next = nextOrderForGroupMove(key, account.id, targetIndex)
  if (!next) return
  const rollback = [...orderIds.value]
  orderIds.value = next
  const newIndex = accountsInGroup(key).findIndex(
    (item) => item.id === account.id,
  )
  persistOrder(
    next,
    rollback,
    `已将${groupTitle(key)}「${account.name}」移动到第 ${newIndex + 1} 位`,
  )
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value))
}

function pointInRect(
  x: number,
  y: number,
  rect: Pick<DOMRect, 'left' | 'right' | 'top' | 'bottom'>,
): boolean {
  return x >= rect.left && x <= rect.right && y >= rect.top && y <= rect.bottom
}

function isPointInsideGroup(
  x: number,
  y: number,
  key: AccountGroupKey,
): boolean {
  const target = groupElements[key]
  if (!target) return false
  return pointInRect(x, y, target.getBoundingClientRect())
}

function captureLayoutSlots(session: AccountDragSession): LayoutSlot[] {
  const container = groupElements[session.group]
  if (!container) return []
  const measured = new Map<number, LayoutSlot>()
  const tiles = container.querySelectorAll<HTMLElement>(
    '[data-account-sort-id]',
  )
  for (const tile of tiles) {
    const id = Number(tile.dataset.accountSortId)
    if (!Number.isInteger(id)) continue
    // offset* ignore FLIP transforms, so live reordering cannot corrupt
    // the logical slot boxes captured before the gesture moved.
    measured.set(id, {
      left: tile.offsetLeft,
      top: tile.offsetTop,
      width: tile.offsetWidth,
      height: tile.offsetHeight,
    })
  }
  const slots: LayoutSlot[] = []
  for (const account of accountsInGroup(session.group)) {
    const slot = measured.get(account.id)
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
  const container = groupElements[session.group]
  if (!container) return
  const rect = container.getBoundingClientRect()
  // Hovering another group is deliberately inert: only a drop inside the
  // source group can reorder, and ownership never changes.
  if (!pointInRect(x, y, rect)) return
  const slotIndex = nearestSlotIndex(
    x - rect.left - container.clientLeft,
    y - rect.top - container.clientTop,
    session.slots,
  )
  if (slotIndex < 0) return
  const entries = accountsInGroup(session.group)
  const currentIndex = entries.findIndex(
    (account) => account.id === session.accountId,
  )
  if (slotIndex === currentIndex) return
  const next = nextOrderForGroupMove(
    session.group,
    session.accountId,
    slotIndex,
  )
  if (next) orderIds.value = next
}

function findScrollContainer(element: HTMLElement): HTMLElement | null {
  if (typeof getComputedStyle !== 'function') return null
  let current = element.parentElement
  while (current) {
    const overflowY = getComputedStyle(current).overflowY
    if (
      (overflowY === 'auto' || overflowY === 'scroll') &&
      current.scrollHeight > current.clientHeight
    ) {
      return current
    }
    current = current.parentElement
  }
  const scrollingElement = document.scrollingElement
  return scrollingElement instanceof HTMLElement ? scrollingElement : null
}

function autoScrollFrame(): void {
  const session = dragSession
  if (!session || !session.active) return
  session.rafId = null
  const scroller = session.scroller
  if (scroller && scroller.scrollHeight > scroller.clientHeight) {
    const rect = scroller.getBoundingClientRect()
    let speed = 0
    if (session.lastY < rect.top + DRAG_EDGE_SIZE) {
      const ratio = clamp(
        (rect.top + DRAG_EDGE_SIZE - session.lastY) / DRAG_EDGE_SIZE,
        0,
        1,
      )
      speed = -Math.ceil(ratio * DRAG_MAX_SCROLL_SPEED)
    } else if (session.lastY > rect.bottom - DRAG_EDGE_SIZE) {
      const ratio = clamp(
        (session.lastY - (rect.bottom - DRAG_EDGE_SIZE)) / DRAG_EDGE_SIZE,
        0,
        1,
      )
      speed = Math.ceil(ratio * DRAG_MAX_SCROLL_SPEED)
    }
    if (speed !== 0) {
      const maxScroll = scroller.scrollHeight - scroller.clientHeight
      const nextTop = Math.min(
        maxScroll,
        Math.max(0, scroller.scrollTop + speed),
      )
      if (nextTop !== scroller.scrollTop) {
        scroller.scrollTop = nextTop
        applyDropTarget(session.lastX, session.lastY)
      }
    }
  }
  if (dragSession === session && session.active && session.scroller) {
    session.rafId = requestAnimationFrame(autoScrollFrame)
  }
}

function startDragAutoScroll(session: AccountDragSession): void {
  if (session.rafId !== null || typeof requestAnimationFrame !== 'function') {
    return
  }
  session.rafId = requestAnimationFrame(autoScrollFrame)
}

function cleanupDragSession(session: AccountDragSession): void {
  if (typeof window !== 'undefined') {
    window.removeEventListener('pointermove', handlePointerMove)
    window.removeEventListener('pointerup', handlePointerUp)
    window.removeEventListener('pointercancel', handlePointerCancel)
    window.removeEventListener('blur', handleWindowBlur)
    window.removeEventListener('keydown', handleWindowKeydown)
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
    // The browser may already have released capture.
  }
  dragBusy.value = false
}

function cancelDrag(): void {
  const session = dragSession
  if (!session) {
    dragBusy.value = false
    return
  }
  dragSession = null
  cleanupDragSession(session)
  dragId.value = null
  if (session.active) {
    orderIds.value = [...session.snapshot]
    liveMessage.value = '已取消拖动，顺序保持原样'
  }
}

function finishDrag(commit: boolean): void {
  const session = dragSession
  if (!session) {
    dragBusy.value = false
    return
  }
  dragSession = null
  cleanupDragSession(session)
  dragId.value = null
  if (!session.active) return
  if (!commit) {
    orderIds.value = [...session.snapshot]
    liveMessage.value = '已取消拖动，顺序保持原样'
    return
  }
  if (session.scope !== orderScope.value) {
    orderIds.value = [...session.snapshot]
    liveMessage.value = '账号或家庭已变更，本次排序未保存'
    return
  }
  const next = [...orderIds.value]
  if (sameOrder(next, session.snapshot)) {
    liveMessage.value = '账户顺序未改变'
    return
  }
  const finalIndex = accountsInGroup(session.group).findIndex(
    (account) => account.id === session.accountId,
  )
  persistOrder(
    next,
    session.snapshot,
    `已将${groupTitle(session.group)}「${session.name}」移动到第 ${finalIndex + 1} 位`,
  )
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
    session.slots = captureLayoutSlots(session)
    session.active = true
    dragId.value = session.accountId
    liveMessage.value = `正在拖动${groupTitle(session.group)}「${session.name}」，松手放到组内新位置`
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
  const droppedInside = isPointInsideGroup(
    event.clientX,
    event.clientY,
    session.group,
  )
  finishDrag(session.active && droppedInside)
}

function handlePointerCancel(event: PointerEvent): void {
  const session = dragSession
  if (!session || event.pointerId !== session.pointerId) return
  finishDrag(false)
}

function handleWindowBlur(): void {
  finishDrag(false)
}

function handleWindowKeydown(event: KeyboardEvent): void {
  if (event.key !== 'Escape' || !dragSession) return
  event.preventDefault()
  finishDrag(false)
}

function suppressContextMenu(event: Event): void {
  event.preventDefault()
}

function beginDrag(
  event: PointerEvent,
  key: AccountGroupKey,
  account: Account,
): void {
  if (dragSession || dialogVisible.value) return
  if (event.pointerType === 'mouse' && event.button !== 0) return
  if (!accountsInGroup(key).some((item) => item.id === account.id)) return
  const captureTarget = groupElements[key]
  if (!captureTarget) return
  event.preventDefault()
  event.stopPropagation()
  dragSession = {
    pointerId: event.pointerId,
    group: key,
    accountId: account.id,
    name: account.name,
    scope: orderScope.value,
    startX: event.clientX,
    startY: event.clientY,
    lastX: event.clientX,
    lastY: event.clientY,
    active: false,
    snapshot: [...orderIds.value],
    captureTarget,
    scroller: findScrollContainer(captureTarget),
    rafId: null,
    slots: [],
  }
  dragBusy.value = true
  try {
    captureTarget.setPointerCapture(event.pointerId)
  } catch {
    // Window listeners keep the gesture usable without capture.
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
    window.addEventListener('keydown', handleWindowKeydown)
  }
}

function beginCardDrag(
  event: PointerEvent,
  key: AccountGroupKey,
  account: Account,
): void {
  // Mouse may drag from the card surface; touch and pen must use the grip so
  // the account list stays scrollable by finger.
  if (event.pointerType !== 'mouse') return
  const target = event.target
  if (
    target instanceof Element &&
    target.closest('button, a, input, textarea, select, [data-no-drag]')
  ) {
    return
  }
  beginDrag(event, key, account)
}

function beginGripDrag(
  event: PointerEvent,
  key: AccountGroupKey,
  account: Account,
): void {
  beginDrag(event, key, account)
}

let requestSequence = 0

async function load(): Promise<void> {
  if (dragSession) cancelDrag()
  const request = ++requestSequence
  loading.value = true
  errorMessage.value = ''
  try {
    const result = await listAccounts()
    if (request !== requestSequence) return
    accounts.value = result
  } catch (error) {
    if (request !== requestSequence) return
    accounts.value = []
    errorMessage.value = getErrorMessage(error)
  } finally {
    if (request === requestSequence) loading.value = false
  }
}

function openCreate(): void {
  if (dragBusy.value) return
  editingAccount.value = null
  dialogVisible.value = true
}

function openEdit(account: Account): void {
  if (dragBusy.value) return
  editingAccount.value = account
  dialogVisible.value = true
}

async function consumeNewQuery(): Promise<void> {
  if (route.query.new !== '1') return
  const query: Record<string, string | null | (string | null)[]> = {}
  for (const [key, value] of Object.entries(route.query)) {
    if (key === 'new' || value === undefined) continue
    query[key] = value
  }
  await router.replace({ query })
}

onMounted(() => {
  syncOrderFromStorage()
  void load()
  if (route.query.new === '1') {
    openCreate()
    void consumeNewQuery()
  }
})

watch(dataVersion, () => {
  void load()
})

watch(orderScope, () => {
  // Never let a gesture started under one account/household write the next
  // scope's preference; cancel first, then adopt that scope's saved order.
  cancelDrag()
  orderError.value = ''
  syncOrderFromStorage()
})

onBeforeUnmount(() => {
  cancelDrag()
})

</script>

<template>
  <div class="accounts-page">
    <div class="page-heading">
      <div class="page-heading__copy">
        <p class="page-eyebrow">资金账户</p>
      </div>
      <div class="heading-actions">
        <el-button
          type="primary"
          :icon="Plus"
          :disabled="dragBusy"
          @click="openCreate"
        >
          添加账户
        </el-button>
      </div>
    </div>

    <div v-if="loading" class="account-group__cards" aria-busy="true">
      <span class="sr-only">正在加载资金账户</span>
      <div v-for="item in 3" :key="item" class="surface-card account-skeleton">
        <el-skeleton animated>
          <template #template>
            <el-skeleton-item variant="text" style="width: 38%" />
            <el-skeleton-item
              variant="h3"
              style="width: 62%; margin-top: 16px"
            />
            <el-skeleton-item
              variant="h1"
              style="width: 48%; margin-top: 18px"
            />
          </template>
        </el-skeleton>
      </div>
    </div>

    <div v-else-if="errorMessage" class="surface-card accounts-error">
      <EmptyState
        title="暂时无法加载资金账户"
        :description="errorMessage"
        :icon="WarningFilled"
      >
        <template #actions>
          <el-button :icon="RefreshRight" @click="load">重新加载</el-button>
        </template>
      </EmptyState>
    </div>

    <div v-else-if="accounts.length === 0" class="surface-card accounts-empty">
      <EmptyState
        title="还没有资金账户"
        description="为家里的现金、银行卡或其他钱包各创建一个账户。添加后，收支与账户转账都会汇总到余额。"
        :icon="Wallet"
      >
        <template #actions>
          <el-button type="primary" :icon="Plus" @click="openCreate">
            添加第一个账户
          </el-button>
        </template>
      </EmptyState>
    </div>

    <template v-else>
      <div class="accounts-summary">
        <p>
          <strong>{{ accounts.length }}</strong>
          个资金账户
        </p>
        <p>
          合计余额
          <strong class="money-number">{{ formatMoney(totalBalance) }}</strong>
        </p>
      </div>

      <p v-if="orderError" class="accounts-order-error" role="alert">
        {{ orderError }}
      </p>

      <div class="account-groups">
        <section
          v-for="group in accountGroups"
          :key="group.key"
          class="account-group"
          role="group"
          :aria-labelledby="`account-group-title-${group.key}`"
          :data-account-group="group.key"
        >
          <div class="account-group__heading">
            <h3 :id="`account-group-title-${group.key}`">
              {{ group.title }}
              <span class="account-group__count">
                {{ group.accounts.length }}
              </span>
            </h3>
            <p class="account-group__hint">{{ group.hint }}</p>
          </div>

          <div
            :ref="(element) => setGroupElement(group.key, element)"
            class="account-group__grid"
          >
            <TransitionGroup
              v-if="group.accounts.length > 0"
              tag="div"
              name="account-sort"
              class="account-group__cards"
              role="list"
              :aria-label="`${group.title}，仅在组内拖动排序`"
            >
              <article
                v-for="(account, index) in group.accounts"
                :key="account.id"
                class="surface-card account-card"
                :class="{ 'is-dragging': dragId === account.id }"
                role="listitem"
                :data-account-sort-id="account.id"
                @pointerdown="beginCardDrag($event, group.key, account)"
              >
                <div class="account-card__main">
                  <button
                    type="button"
                    class="account-card__grip"
                    :disabled="dialogVisible"
                    :aria-label="gripLabel(group, account, index)"
                    title="按住拖动调整组内顺序；聚焦后可按方向键"
                    @pointerdown.stop="beginGripDrag($event, group.key, account)"
                    @keydown="
                      handleGripKeydown($event, group.key, account, index)
                    "
                    @contextmenu="suppressContextMenu"
                  >
                    <el-icon :size="14" aria-hidden="true"><Rank /></el-icon>
                  </button>

                  <span class="account-card__icon" aria-hidden="true">
                    <el-icon :size="17"><Wallet /></el-icon>
                  </span>

                  <div class="account-card__identity">
                    <h4 class="account-card__name" :title="account.name">
                      {{ account.name }}
                    </h4>
                    <p
                      v-if="group.key === 'other'"
                      class="account-card__owner"
                      :title="`归属成员：${account.owner_name || '未知成员'}`"
                    >
                      归属：{{ account.owner_name || '未知成员' }}
                    </p>
                    <p v-else class="account-card__owner">
                      {{ group.key === 'personal' ? '本人所有' : '全家共用' }}
                    </p>
                  </div>

                  <el-button
                    text
                    circle
                    :icon="EditPen"
                    title="编辑账户"
                    :disabled="dragBusy"
                    :aria-label="`编辑账户 ${account.name}`"
                    @click="openEdit(account)"
                  />
                </div>

                <div class="account-card__balance-row">
                  <span class="account-card__label">当前余额</span>
                  <p
                    class="account-card__balance money-number"
                    :class="{ 'is-negative': Number(account.balance) < 0 }"
                  >
                    {{ formatMoney(account.balance) }}
                  </p>
                </div>
              </article>
            </TransitionGroup>

            <p v-else class="account-group__empty">{{ group.emptyHint }}</p>
          </div>
        </section>
      </div>
    </template>

    <p class="sr-only" aria-live="polite">{{ liveMessage }}</p>

    <AccountDialog
      v-model="dialogVisible"
      :account="editingAccount"
    />
  </div>
</template>

<style scoped>
.accounts-page {
  min-width: 0;
}

.accounts-order-error {
  margin: 0 0 14px;
  padding: 9px 12px;
  color: var(--hb-danger);
  background: #fdf3f2;
  border: 1px solid #f0d1cc;
  border-radius: 10px;
  font-size: 12px;
  line-height: 1.6;
}

.account-groups {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.account-group {
  min-width: 0;
}

.account-group__heading {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 4px 10px;
  margin-bottom: 8px;
  padding: 0 2px 7px;
  border-bottom: 1px dashed var(--hb-line);
}

.account-group__heading h3 {
  display: flex;
  gap: 7px;
  align-items: center;
  margin: 0;
  font-size: 13.5px;
  font-weight: 600;
}

.account-group__count {
  min-width: 20px;
  padding: 1px 7px;
  color: var(--hb-teal);
  background: var(--hb-teal-faint);
  border: 1px solid #d8eae5;
  border-radius: 999px;
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  line-height: 1.6;
  text-align: center;
}

.account-group__hint {
  margin: 0;
  color: var(--hb-ink-faint);
  font-size: 11px;
}

.account-group__grid {
  position: relative;
}

.account-group__cards {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
}

.account-group__empty {
  margin: 0;
  padding: 9px 12px;
  color: var(--hb-ink-faint);
  background: rgba(255, 253, 248, 0.45);
  border: 1px dashed var(--hb-line);
  border-radius: 11px;
  font-size: 11.5px;
  line-height: 1.65;
}

.account-card {
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  gap: 9px;
  min-height: 132px;
  padding: 12px 13px;
  overflow: hidden;
  touch-action: pan-y;
  user-select: none;
  -webkit-touch-callout: none;
  transition:
    background-color 0.16s ease,
    border-color 0.16s ease,
    box-shadow 0.16s ease;
}

.account-card:hover {
  border-color: var(--hb-line-strong);
}

.account-card__main {
  display: flex;
  gap: 7px;
  align-items: center;
  min-width: 0;
}

.account-card__grip {
  display: grid;
  flex: 0 0 auto;
  place-items: center;
  width: 28px;
  height: 40px;
  margin-left: -6px;
  padding: 0;
  color: var(--hb-ink-faint);
  background: transparent;
  border: 0;
  border-radius: 8px;
  cursor: grab;
  touch-action: none;
}

.account-card__grip:hover:not(:disabled) {
  color: var(--hb-teal);
  background: var(--hb-teal-faint);
}

.account-card__grip:focus-visible {
  outline: 2px solid var(--hb-teal);
  outline-offset: 2px;
}

.account-card__grip:disabled {
  cursor: not-allowed;
  opacity: 0.45;
}

.account-card.is-dragging {
  z-index: 1;
  background: var(--hb-teal-faint);
  border-color: var(--hb-teal);
  box-shadow: 0 10px 26px rgba(10, 93, 87, 0.16);
}

.account-card.is-dragging .account-card__grip {
  color: var(--hb-teal);
  cursor: grabbing;
}

.account-sort-move {
  transition:
    transform 0.2s cubic-bezier(0.2, 0, 0, 1),
    background-color 0.16s ease,
    border-color 0.16s ease,
    box-shadow 0.16s ease;
}

.account-card__icon {
  display: grid;
  flex: 0 0 auto;
  place-items: center;
  width: 30px;
  height: 30px;
  color: var(--hb-teal);
  background: var(--hb-teal-faint);
  border: 1px solid #d8eae5;
  border-radius: 10px;
}

.account-card__identity {
  flex: 1 1 auto;
  min-width: 0;
}

.account-card__name {
  margin: 0;
  overflow: hidden;
  font-size: 13.5px;
  font-weight: 600;
  line-height: 1.35;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.account-card__owner {
  margin: 2px 0 0;
  overflow: hidden;
  color: var(--hb-ink-faint);
  font-size: 10.5px;
  line-height: 1.5;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.account-card__balance-row {
  display: flex;
  gap: 8px;
  align-items: baseline;
  justify-content: space-between;
  padding-top: 8px;
  border-top: 1px dashed var(--hb-line);
}

.account-card__label {
  color: var(--hb-ink-faint);
  font-size: 10px;
  white-space: nowrap;
}

.account-card__balance {
  margin: 0;
  overflow-wrap: anywhere;
  color: var(--hb-ink);
  font-size: 19px;
  font-weight: 600;
  letter-spacing: -0.02em;
  line-height: 1.2;
  text-align: right;
}

.account-card__balance.is-negative {
  color: var(--hb-danger);
}

.account-skeleton {
  min-height: 132px;
  padding: 14px;
}

.accounts-error,
.accounts-empty {
  padding: 16px;
}

@media (max-width: 1100px) {
  .account-group__cards {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 640px) {
  .account-groups {
    gap: 16px;
  }

  .account-group__cards {
    grid-template-columns: minmax(0, 1fr);
    gap: 8px;
  }

  .account-card {
    min-height: 122px;
    padding: 11px 12px;
  }

  .account-card__balance {
    font-size: 18px;
  }

  .account-skeleton {
    min-height: 122px;
  }

  .accounts-error,
  .accounts-empty {
    padding: 8px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .account-sort-move,
  .account-card {
    transition: none;
  }
}
</style>
