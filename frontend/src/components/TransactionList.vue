<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Delete, EditPen, Loading } from '@element-plus/icons-vue'
import { getErrorMessage } from '../api/client'
import { deleteTransaction } from '../api/transactions'
import type { Account, Transaction, TransactionKind } from '../api/types'
import { useMediaQuery } from '../composables/useMediaQuery'
import { markDataChanged } from '../stores/data'
import { readCategoryEntries } from '../stores/categories'
import { categoryIcon } from '../utils/categoryIcons'
import { entryMatchesName } from '../utils/categoryPreferences'
import { formatDateTimeLabel, formatSignedAmount } from '../utils/format'
import EmptyState from './EmptyState.vue'
import TransactionDialog from './TransactionDialog.vue'

const props = withDefaults(
  defineProps<{
    transactions: Transaction[]
    accounts: Account[]
    loading?: boolean
    editingDisabled?: boolean
  }>(),
  {
    loading: false,
    editingDisabled: false,
  },
)

const emit = defineEmits<{
  (
    event: 'view-bills',
    payload: { kind: 'expense' | 'income'; categories: string[] },
  ): void
}>()

const KIND_LABELS: Record<TransactionKind, string> = {
  expense: '支出',
  income: '收入',
  transfer: '转账',
}

const isMobile = useMediaQuery('(max-width: 760px)')

const dialogVisible = ref(false)
const editingTransaction = ref<Transaction | null>(null)
const initialKind = ref<TransactionKind>('expense')
const deletingId = ref<number | null>(null)

/**
 * Editing needs at least one account to save against. Deletion stays
 * available even when editing is disabled or no account can be loaded.
 */
const editDisabled = computed(
  () => props.editingDisabled || props.accounts.length === 0,
)

/** Must stay in sync with `.tx-card__actions` width in the style block. */
const SWIPE_REVEAL_WIDTH = 152
const SWIPE_START_PX = 8
const SWIPE_SETTLE_RATIO = 0.5

interface SwipeSession {
  pointerId: number
  id: number
  surface: HTMLElement
  startX: number
  startY: number
  baseOffset: number
  revealWidth: number
  offset: number
  active: boolean
}

const listRoot = ref<HTMLElement | null>(null)
const openRowId = ref<number | null>(null)
const draggingRowId = ref<number | null>(null)
const dragOffset = ref(0)
let swipeSession: SwipeSession | null = null
let suppressClickUntil = 0

function actionPanelId(transactionId: number): string {
  return `tx-actions-${transactionId}`
}

function surfaceStyle(transactionId: number) {
  if (draggingRowId.value !== transactionId) return undefined
  return { transform: `translateX(-${dragOffset.value}px)` }
}

function endSwipe(): void {
  const session = swipeSession
  swipeSession = null
  draggingRowId.value = null
  dragOffset.value = 0
  if (!session) return
  try {
    if (session.surface.hasPointerCapture(session.pointerId)) {
      session.surface.releasePointerCapture(session.pointerId)
    }
  } catch {
    // The browser may already have released capture.
  }
}

function closeSwipe(options: { restoreFocus?: boolean } = {}): void {
  const id = openRowId.value
  openRowId.value = null
  endSwipe()
  if (id === null || !options.restoreFocus) return
  const card = listRoot.value?.querySelector<HTMLElement>(
    `[data-tx-id="${id}"]`,
  )
  card?.focus({ preventScroll: true })
}

function openRow(transactionId: number): void {
  if (openRowId.value === transactionId) return
  endSwipe()
  openRowId.value = transactionId
}

function handleDocumentPointerDown(event: PointerEvent): void {
  if (openRowId.value === null) return
  if (!(event.target instanceof Element)) return
  const card = event.target.closest<HTMLElement>('[data-tx-id]')
  if (card && Number(card.dataset.txId) === openRowId.value) return
  closeSwipe()
}

function handleDocumentKeydown(event: KeyboardEvent): void {
  if (event.key !== 'Escape' || openRowId.value === null) return
  event.preventDefault()
  closeSwipe({ restoreFocus: true })
}

onMounted(() => {
  document.addEventListener('pointerdown', handleDocumentPointerDown, true)
  document.addEventListener('keydown', handleDocumentKeydown)
})

onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', handleDocumentPointerDown, true)
  document.removeEventListener('keydown', handleDocumentKeydown)
  closeSwipe()
})

watch(isMobile, () => closeSwipe())
watch(
  () => props.transactions,
  () => closeSwipe(),
)

function handleSurfacePointerDown(
  event: PointerEvent,
  transactionId: number,
): void {
  if (event.pointerType === 'mouse' && event.button !== 0) return
  if (
    event.target instanceof Element &&
    event.target.closest('button, a, input, textarea, select, [role="button"]')
  ) {
    return
  }
  endSwipe()
  if (openRowId.value !== null && openRowId.value !== transactionId) {
    openRowId.value = null
  }
  const surface = event.currentTarget
  if (!(surface instanceof HTMLElement)) return
  const actions =
    surface.parentElement?.querySelector<HTMLElement>('.tx-card__actions')
  const revealWidth =
    actions?.getBoundingClientRect().width || SWIPE_REVEAL_WIDTH
  swipeSession = {
    pointerId: event.pointerId,
    id: transactionId,
    surface,
    startX: event.clientX,
    startY: event.clientY,
    baseOffset: openRowId.value === transactionId ? revealWidth : 0,
    revealWidth,
    offset: openRowId.value === transactionId ? revealWidth : 0,
    active: false,
  }
  try {
    surface.setPointerCapture(event.pointerId)
  } catch {
    // Pointer capture is an enhancement; the gesture still tracks moves.
  }
}

function handleSurfacePointerMove(event: PointerEvent): void {
  const session = swipeSession
  if (!session || event.pointerId !== session.pointerId) return
  const dx = event.clientX - session.startX
  const dy = event.clientY - session.startY
  if (!session.active) {
    if (Math.abs(dx) < SWIPE_START_PX && Math.abs(dy) < SWIPE_START_PX) return
    // Vertical intent belongs to the page scroll; horizontal rightward
    // movement on a closed card has nothing to reveal.
    if (Math.abs(dy) >= Math.abs(dx)) {
      endSwipe()
      return
    }
    if (session.baseOffset === 0 && dx > 0) {
      endSwipe()
      return
    }
    session.active = true
    draggingRowId.value = session.id
  }
  if (event.cancelable) event.preventDefault()
  const next = Math.min(
    Math.max(session.baseOffset - dx, 0),
    session.revealWidth,
  )
  if (next === session.offset) return
  session.offset = next
  dragOffset.value = next
}

function handleSurfacePointerUp(event: PointerEvent): void {
  const session = swipeSession
  if (!session || event.pointerId !== session.pointerId) return
  const { id, active, offset, revealWidth } = session
  endSwipe()
  if (!active) return
  suppressClickUntil = Date.now() + 400
  openRowId.value =
    offset > revealWidth * SWIPE_SETTLE_RATIO ? id : null
}

function handleSurfacePointerCancel(event: PointerEvent): void {
  const session = swipeSession
  if (!session || event.pointerId !== session.pointerId) return
  endSwipe()
}

function handleSurfaceClick(transactionId: number): void {
  if (Date.now() < suppressClickUntil) return
  if (openRowId.value === transactionId) closeSwipe()
}

function handleCardKeydown(event: KeyboardEvent, transactionId: number): void {
  if (event.target !== event.currentTarget) return
  if (event.altKey || event.ctrlKey || event.metaKey) return
  if (
    event.key === 'Enter' ||
    event.key === ' ' ||
    event.key === 'Spacebar'
  ) {
    event.preventDefault()
    openRow(transactionId)
    return
  }
  if (event.key === 'ArrowLeft') {
    event.preventDefault()
    openRow(transactionId)
    return
  }
  if (event.key === 'ArrowRight' && openRowId.value === transactionId) {
    event.preventDefault()
    closeSwipe({ restoreFocus: true })
  }
}

function handleEditAction(transaction: Transaction): void {
  closeSwipe()
  openEdit(transaction)
}

function handleDeleteAction(transaction: Transaction): void {
  closeSwipe()
  void confirmDelete(transaction)
}

function iconForCategory(name: string, kind: TransactionKind) {
  if (kind === 'transfer') return categoryIcon(name)
  const entry = readCategoryEntries(kind).find((candidate) =>
    entryMatchesName(candidate, name),
  )
  return categoryIcon(entry?.name ?? name, entry?.icon)
}

function kindLabel(kind: TransactionKind): string {
  return KIND_LABELS[kind]
}

/**
 * Transfers read as source → destination; other kinds show one account.
 * Element Plus table slots hand over its untyped DefaultRow, so validate at
 * this boundary instead of casting the row to Transaction.
 */
function displayAccount(row: unknown): string {
  if (!isTransactionRow(row)) return ''
  if (row.kind === 'transfer' && row.transfer_account_name) {
    return `${row.account_name} → ${row.transfer_account_name}`
  }
  return row.account_name
}

interface TransactionDay {
  key: string
  label: string
  transactions: Transaction[]
}

/** `date` is local civil YYYY-MM-DDTHH:mm:ss, so its prefix is the local day. */
function dayKey(date: string): string {
  const match = /^(\d{4}-\d{2}-\d{2})/.exec(date)
  return match ? match[1] : date
}

function dayLabel(key: string): string {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(key)
  if (!match) return key
  return `${match[1]}年${Number(match[2])}月${Number(match[3])}日`
}

/** Mobile rows show only the local time of day; dates live in group labels. */
function timeOfDay(date: string): string {
  const match = /^\d{4}-\d{2}-\d{2}T(\d{2}:\d{2})/.exec(date)
  return match ? match[1] : formatDateTimeLabel(date)
}

/**
 * Groups adjacent rows by local day without reordering: the API already
 * returns newest first, and a day reappearing later stays a second group.
 */
const transactionDays = computed<TransactionDay[]>(() => {
  const days: TransactionDay[] = []
  for (const transaction of props.transactions) {
    const key = dayKey(transaction.date)
    const latest = days[days.length - 1]
    if (latest && latest.key === key) {
      latest.transactions.push(transaction)
    } else {
      days.push({ key, label: dayLabel(key), transactions: [transaction] })
    }
  }
  return days
})

function recordNoun(row: unknown): string {
  if (!isTransactionRow(row)) return '记录'
  return row.kind === 'transfer' ? '转账' : '收支'
}

/**
 * Element Plus table slots expose rows as its generic DefaultRow rather than
 * the array item type, so narrow at the boundary instead of casting to any.
 */
function isTransactionRow(row: unknown): row is Transaction {
  if (typeof row !== 'object' || row === null) return false
  const candidate = row as Record<string, unknown>
  return (
    typeof candidate.id === 'number' &&
    typeof candidate.account === 'number' &&
    (candidate.kind === 'income' ||
      candidate.kind === 'expense' ||
      candidate.kind === 'transfer') &&
    typeof candidate.amount === 'string' &&
    typeof candidate.category === 'string' &&
    typeof candidate.date === 'string' &&
    typeof candidate.note === 'string' &&
    typeof candidate.owner === 'number' &&
    typeof candidate.owner_name === 'string' &&
    (candidate.transfer_account === null ||
      typeof candidate.transfer_account === 'number') &&
    typeof candidate.transfer_account_name === 'string'
  )
}

function openEditRow(row: unknown): void {
  if (isTransactionRow(row)) openEdit(row)
}

function openEdit(transaction: Transaction): void {
  if (editDisabled.value) return
  initialKind.value = transaction.kind
  editingTransaction.value = transaction
  dialogVisible.value = true
}

async function confirmDeleteRow(row: unknown): Promise<void> {
  if (!isTransactionRow(row)) return
  await confirmDelete(row)
}

async function confirmDelete(transaction: Transaction): Promise<void> {
  const isTransfer = transaction.kind === 'transfer'
  const amount = formatSignedAmount(transaction.amount, transaction.kind)
  const detail = isTransfer
    ? `从「${transaction.account_name}」转到「${transaction.transfer_account_name}」，${amount}`
    : amount
  const description = isTransfer
    ? `将删除记账时间为 ${formatDateTimeLabel(transaction.date)} 的账户转账记录（${detail}）。删除后两个账户的余额都会重新计算，且无法恢复。`
    : `将删除记账时间为 ${formatDateTimeLabel(transaction.date)} 的「${
        transaction.category
      }」记录（${detail}）。删除后账户余额会重新计算，且无法恢复。`

  try {
    await ElMessageBox.confirm(
      description,
      isTransfer ? '删除这笔转账？' : '删除这笔收支？',
      {
        confirmButtonText: '删除',
        cancelButtonText: '取消',
        type: 'warning',
        confirmButtonClass: 'el-button--danger',
      },
    )
  } catch {
    return
  }

  if (deletingId.value !== null) return
  deletingId.value = transaction.id
  try {
    await deleteTransaction(transaction.id)
    ElMessage.success(isTransfer ? '已删除这笔转账' : '已删除这笔收支')
    markDataChanged()
  } catch (error) {
    ElMessage.error(getErrorMessage(error))
  } finally {
    deletingId.value = null
  }
}

/**
 * The edit dialog may offer to filter bills for a category selection. Close
 * the dialog first, then let the owning view apply its existing filter flow.
 */
function handleViewBills(selection: {
  kind: 'expense' | 'income'
  categories: string[]
}): void {
  dialogVisible.value = false
  emit('view-bills', selection)
}
</script>

<template>
  <div class="transaction-list">
    <template v-if="isMobile">
      <div ref="listRoot" v-loading="loading" class="mobile-list">
        <section
          v-for="(day, index) in transactionDays"
          :key="`${day.key}-${index}`"
          class="tx-day-group"
          :aria-labelledby="`tx-day-${index}`"
        >
          <h3 :id="`tx-day-${index}`" class="tx-day-group__label">
            {{ day.label }}
          </h3>
          <div class="tx-day-group__rows">
            <article
              v-for="transaction in day.transactions"
              :key="transaction.id"
              class="tx-card"
              :class="{
                'tx-card--open': openRowId === transaction.id,
                'tx-card--dragging': draggingRowId === transaction.id,
              }"
              :data-tx-id="transaction.id"
              tabindex="0"
              :aria-expanded="openRowId === transaction.id"
              :aria-controls="actionPanelId(transaction.id)"
              @keydown="handleCardKeydown($event, transaction.id)"
            >
              <div
                :id="actionPanelId(transaction.id)"
                class="tx-card__actions"
                :inert="openRowId === transaction.id ? undefined : true"
                :aria-hidden="openRowId !== transaction.id"
              >
                <button
                  type="button"
                  class="tx-card__action"
                  :disabled="editDisabled"
                  :aria-label="`编辑${transaction.category}这笔${recordNoun(transaction)}`"
                  @click="handleEditAction(transaction)"
                >
                  <el-icon aria-hidden="true"><EditPen /></el-icon>
                  <span>编辑</span>
                </button>
                <button
                  type="button"
                  class="tx-card__action tx-card__action--danger"
                  :disabled="deletingId === transaction.id"
                  :aria-label="`删除${transaction.category}这笔${recordNoun(transaction)}`"
                  @click="handleDeleteAction(transaction)"
                >
                  <el-icon
                    v-if="deletingId === transaction.id"
                    class="is-loading"
                    aria-hidden="true"
                  ><Loading /></el-icon>
                  <el-icon v-else aria-hidden="true"><Delete /></el-icon>
                  <span>删除</span>
                </button>
              </div>

              <div
                class="tx-card__surface"
                :style="surfaceStyle(transaction.id)"
                @pointerdown="handleSurfacePointerDown($event, transaction.id)"
                @pointermove="handleSurfacePointerMove"
                @pointerup="handleSurfacePointerUp"
                @pointercancel="handleSurfacePointerCancel"
                @lostpointercapture="handleSurfacePointerCancel"
                @click="handleSurfaceClick(transaction.id)"
              >
                <div class="tx-card__top">
                  <div class="tx-card__identity">
                    <el-icon class="category-icon" aria-hidden="true">
                      <component :is="iconForCategory(transaction.category, transaction.kind)" />
                    </el-icon>
                    <h4 :title="transaction.category">{{ transaction.category }}</h4>
                  </div>
                  <span
                    class="tx-card__amount money-number"
                    :class="`amount--${transaction.kind}`"
                    :aria-label="`${kindLabel(transaction.kind)} ${formatSignedAmount(transaction.amount, transaction.kind)}`"
                  >
                    {{ formatSignedAmount(transaction.amount, transaction.kind) }}
                  </span>
                </div>

                <p class="tx-card__meta">
                  <time
                    class="tx-card__time"
                    :datetime="transaction.date"
                  >
                    <span class="sr-only">{{ formatDateTimeLabel(transaction.date) }}</span>
                    <span aria-hidden="true">{{ timeOfDay(transaction.date) }}</span>
                  </time>
                  <template v-if="transaction.kind === 'transfer'">
                    <span class="tx-card__sep" aria-hidden="true">·</span>
                    <span class="tx-card__flag">转账</span>
                  </template>
                  <span class="tx-card__sep" aria-hidden="true">·</span>
                  <span
                    class="tx-card__account"
                    :title="displayAccount(transaction)"
                  >{{ displayAccount(transaction) }}</span>
                  <span class="tx-card__sep" aria-hidden="true">·</span>
                  <span
                    class="tx-card__owner"
                    :title="transaction.owner_name || '未知成员'"
                  >{{ transaction.owner_name || '未知成员' }}</span>
                </p>

                <p
                  v-if="transaction.note"
                  class="tx-card__note"
                  :title="transaction.note"
                >
                  {{ transaction.note }}
                </p>
              </div>
            </article>
          </div>
        </section>

        <slot v-if="!loading && transactions.length === 0" name="empty">
          <EmptyState
            title="暂无记录"
            description="当前没有可显示的收支流水。"
          />
        </slot>
      </div>
    </template>

    <el-table
      v-else
      v-loading="loading"
      :data="transactions"
      row-key="id"
      class="ledger-table"
    >
      <el-table-column label="记账时间" width="160">
        <template #default="{ row }">
          <span class="date-cell">{{ formatDateTimeLabel(row.date) }}</span>
        </template>
      </el-table-column>

      <el-table-column label="分类" min-width="118">
        <template #default="{ row }">
          <span class="category-cell">
            <el-icon class="category-icon" aria-hidden="true">
              <component :is="iconForCategory(row.category, row.kind)" />
            </el-icon>
            <span>{{ row.category }}</span>
          </span>
        </template>
      </el-table-column>

      <el-table-column label="类型" width="88">
        <template #default="{ row }">
          <span class="kind-pill" :class="`kind-pill--${row.kind}`">
            {{ kindLabel(row.kind) }}
          </span>
        </template>
      </el-table-column>

      <el-table-column label="金额" width="138" align="right">
        <template #default="{ row }">
          <span
            class="amount money-number"
            :class="`amount--${row.kind}`"
          >
            {{ formatSignedAmount(row.amount, row.kind) }}
          </span>
        </template>
      </el-table-column>

      <el-table-column label="账户" min-width="112">
        <template #default="{ row }">
          <span class="account-cell" :title="displayAccount(row)">
            {{ displayAccount(row) }}
          </span>
        </template>
      </el-table-column>

      <el-table-column
        prop="note"
        label="备注"
        min-width="148"
        show-overflow-tooltip
      >
        <template #default="{ row }">
          <span v-if="row.note" class="note-cell">{{ row.note }}</span>
          <span v-else class="cell-muted">—</span>
        </template>
      </el-table-column>

      <el-table-column label="成员" width="96">
        <template #default="{ row }">
          <span
            class="owner-cell"
            :title="row.owner_name || '未知成员'"
          >
            {{ row.owner_name || '未知成员' }}
          </span>
        </template>
      </el-table-column>

      <el-table-column label="操作" width="104" align="right">
        <template #default="{ row }">
          <div class="row-actions">
            <el-button
              text
              circle
              :icon="EditPen"
              title="编辑"
              :disabled="editDisabled"
              :aria-label="`编辑${row.category}这笔${recordNoun(row)}`"
              @click="openEditRow(row)"
            />
            <el-button
              text
              circle
              type="danger"
              :icon="Delete"
              title="删除"
              :loading="deletingId === row.id"
              :aria-label="`删除${row.category}这笔${recordNoun(row)}`"
              @click="confirmDeleteRow(row)"
            />
          </div>
        </template>
      </el-table-column>

      <template #empty>
        <slot name="empty">
          <EmptyState
            title="暂无记录"
            description="当前没有可显示的收支流水。"
          />
        </slot>
      </template>
    </el-table>

    <TransactionDialog
      v-model="dialogVisible"
      :transaction="editingTransaction"
      :accounts="accounts"
      :initial-kind="initialKind"
      @view-bills="handleViewBills"
    />
  </div>
</template>

<style scoped>
.ledger-table {
  --el-table-border-color: var(--hb-line);
  --el-table-header-bg-color: #faf6ef;
  --el-table-row-hover-bg-color: #fbf8f2;
  --el-table-tr-bg-color: transparent;
  --el-table-bg-color: transparent;
  --el-table-text-color: var(--hb-ink);
  --el-table-header-text-color: var(--hb-ink-faint);
  width: 100%;
  font-size: 13px;
}

.ledger-table :deep(.el-table__inner-wrapper::before) {
  display: none;
}

.ledger-table :deep(th.el-table__cell) {
  height: 40px;
  padding: 0;
  font-size: 11px;
  font-weight: 500;
  letter-spacing: 0.03em;
}

.ledger-table :deep(td.el-table__cell) {
  padding: 12px 0;
}

.ledger-table :deep(.cell) {
  padding: 0 10px;
}

.ledger-table :deep(.el-table__empty-block) {
  min-height: 220px;
}

.date-cell {
  color: var(--hb-ink-soft);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.category-cell {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-weight: 500;
}

.category-icon {
  flex: 0 0 auto;
  color: var(--hb-ink-soft);
  font-size: 14px;
}

.account-cell {
  display: block;
  overflow: hidden;
  color: var(--hb-ink-soft);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.note-cell {
  color: var(--hb-ink-soft);
  font-size: 12px;
}

.owner-cell {
  display: block;
  overflow: hidden;
  color: var(--hb-ink-soft);
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cell-muted {
  color: var(--hb-ink-faint);
  font-size: 11px;
}

.amount {
  font-weight: 600;
  white-space: nowrap;
}

.amount--income {
  color: var(--hb-teal-dark);
}

.amount--expense {
  color: #9a5530;
}

.amount--transfer {
  color: #8a6a2f;
}

.kind-pill {
  display: inline-flex;
  align-items: center;
  padding: 1px 8px;
  border-radius: 999px;
  font-size: 10px;
  font-weight: 500;
  line-height: 1.7;
  white-space: nowrap;
}

.kind-pill--income {
  color: var(--hb-teal-dark);
  background: var(--hb-teal-soft);
}

.kind-pill--expense {
  color: #96502b;
  background: var(--hb-terracotta-soft);
}

.kind-pill--transfer {
  color: #7d5f25;
  background: #f6ecd7;
}

.row-actions {
  display: flex;
  justify-content: flex-end;
  gap: 0;
}

.row-actions :deep(.el-button + .el-button) {
  margin-left: 2px;
}

.mobile-list {
  min-height: 180px;
  padding-bottom: 6px;
}

.tx-day-group {
  margin-bottom: 10px;
}

.tx-day-group:last-of-type {
  margin-bottom: 0;
}

.tx-day-group__label {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 3px 5px;
  color: var(--hb-ink-faint);
  font-size: 11px;
  font-weight: 500;
  letter-spacing: 0.02em;
}

.tx-day-group__label::after {
  content: '';
  flex: 1 1 auto;
  height: 1px;
  background: var(--hb-line);
}

.tx-day-group__rows {
  display: grid;
  gap: 6px;
}

.tx-card {
  position: relative;
  overflow: hidden;
  background: var(--hb-surface-raised);
  border: 1px solid var(--hb-line);
  border-radius: 10px;
  touch-action: pan-y;
}

.tx-card__surface {
  position: relative;
  z-index: 1;
  padding: 8px 12px 9px;
  background: #fbf7f0;
  touch-action: pan-y;
  transition: transform 0.22s cubic-bezier(0.22, 0.61, 0.36, 1);
}

.tx-card--open .tx-card__surface {
  transform: translateX(-152px);
}

.tx-card--dragging {
  user-select: none;
}

.tx-card--dragging .tx-card__surface {
  transition: none;
}

.tx-card__actions {
  position: absolute;
  inset: 0 0 0 auto;
  z-index: 0;
  display: flex;
  width: 152px;
}

.tx-card__action {
  display: flex;
  flex: 1 1 0;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 3px;
  min-width: 44px;
  min-height: 44px;
  padding: 6px;
  color: var(--hb-teal);
  font: inherit;
  font-size: 12px;
  background: var(--hb-surface-raised);
  border: 0;
  border-radius: 0;
  cursor: pointer;
  appearance: none;
  transition: background-color 0.16s ease, color 0.16s ease;
}

.tx-card__action + .tx-card__action {
  border-left: 1px solid var(--hb-line);
}

.tx-card__action--danger {
  color: var(--hb-danger);
}

.tx-card__action:disabled {
  color: var(--hb-ink-faint);
  cursor: not-allowed;
  opacity: 0.55;
}

.tx-card__action:focus-visible {
  outline-offset: -2px;
}

@media (hover: hover) {
  .tx-card__action:hover:not(:disabled) {
    background: var(--hb-teal-faint);
  }

  .tx-card__action--danger:hover:not(:disabled) {
    background: var(--hb-danger-soft);
  }
}

.tx-card__top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.tx-card__identity {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.tx-card__identity h4 {
  min-width: 0;
  overflow: hidden;
  font-size: 13.5px;
  font-weight: 600;
  line-height: 1.4;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tx-card__identity .category-icon {
  margin-top: 0;
}

.tx-card__meta {
  display: flex;
  align-items: baseline;
  margin-top: 1px;
  color: var(--hb-ink-faint);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  line-height: 1.45;
  white-space: nowrap;
}

.tx-card__sep {
  flex: 0 0 auto;
  margin: 0 5px;
  color: var(--hb-ink-faint);
}

.tx-card__time {
  flex: 0 0 auto;
  white-space: nowrap;
}

.tx-card__flag {
  flex: 0 0 auto;
  color: #7d5f25;
}

.tx-card__account,
.tx-card__owner {
  overflow: hidden;
  color: var(--hb-ink-soft);
  text-overflow: ellipsis;
}

.tx-card__account {
  flex: 1 1 auto;
  min-width: 2.5em;
}

.tx-card__owner {
  flex: 0 1 auto;
  min-width: 2.5em;
  max-width: 45%;
}

.tx-card__amount {
  flex: 0 0 auto;
  font-size: 15px;
  font-weight: 600;
  white-space: nowrap;
}

.tx-card__note {
  margin-top: 1px;
  overflow: hidden;
  color: var(--hb-ink-soft);
  font-size: 11px;
  line-height: 1.5;
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (max-width: 520px) {
  .tx-card__surface {
    padding: 7px 10px 8px;
  }

  .tx-card__top {
    gap: 8px;
  }

  .tx-card__amount {
    font-size: 14.5px;
  }

  .tx-card__meta {
    font-size: 10.5px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .tx-card__surface,
  .tx-card__action {
    transition: none;
  }
}
</style>
