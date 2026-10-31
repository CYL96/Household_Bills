<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Delete, EditPen } from '@element-plus/icons-vue'
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
      <div v-loading="loading" class="mobile-list">
        <article
          v-for="transaction in transactions"
          :key="transaction.id"
          class="tx-card"
        >
          <div class="tx-card__top">
            <div class="tx-card__identity">
              <span
                class="tx-card__dot"
                :class="`tx-card__dot--${transaction.kind}`"
                aria-hidden="true"
              ></span>
              <div class="tx-card__heading">
                <h4>
                  <el-icon class="category-icon" aria-hidden="true">
                    <component :is="iconForCategory(transaction.category, transaction.kind)" />
                  </el-icon>
                  <span>{{ transaction.category }}</span>
                </h4>
                <p>
                  <span class="tx-card__time">{{ formatDateTimeLabel(transaction.date) }}</span>
                  <span aria-hidden="true"> · </span>
                  {{ displayAccount(transaction) }}
                </p>
              </div>
            </div>
            <span
              class="tx-card__amount money-number"
              :class="`amount--${transaction.kind}`"
            >
              {{ formatSignedAmount(transaction.amount, transaction.kind) }}
            </span>
          </div>

          <p v-if="transaction.note" class="tx-card__note">
            {{ transaction.note }}
          </p>

          <div class="tx-card__foot">
            <div class="tx-card__meta">
              <span class="kind-pill" :class="`kind-pill--${transaction.kind}`">
                {{ kindLabel(transaction.kind) }}
              </span>
              <span
                class="tx-card__owner"
                :title="transaction.owner_name || '未知成员'"
              >{{ transaction.owner_name || '未知成员' }}</span>
            </div>
            <div class="tx-card__actions">
              <el-button
                text
                size="small"
                :icon="EditPen"
                :disabled="editDisabled"
                :aria-label="`编辑${transaction.category}这笔${recordNoun(transaction)}`"
                @click="openEdit(transaction)"
              >
                编辑
              </el-button>
              <el-button
                text
                size="small"
                type="danger"
                :icon="Delete"
                :loading="deletingId === transaction.id"
                :aria-label="`删除${transaction.category}这笔${recordNoun(transaction)}`"
                @click="confirmDelete(transaction)"
              >
                删除
              </el-button>
            </div>
          </div>
        </article>

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

.tx-card {
  margin-bottom: 9px;
  padding: 14px 15px;
  background: #fbf7f0;
  border: 1px solid var(--hb-line);
  border-radius: 14px;
}

.tx-card:last-of-type {
  margin-bottom: 0;
}

.tx-card__top {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.tx-card__identity {
  display: flex;
  min-width: 0;
  gap: 9px;
}

.tx-card__dot {
  flex: 0 0 auto;
  width: 8px;
  height: 8px;
  margin-top: 6px;
  border-radius: 50%;
}

.tx-card__dot--income {
  background: var(--hb-teal);
}

.tx-card__dot--expense {
  background: var(--hb-terracotta);
}

.tx-card__dot--transfer {
  background: var(--hb-gold);
}

.tx-card__heading {
  min-width: 0;
}

.tx-card__heading h4 {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  overflow-wrap: anywhere;
  font-size: 14px;
  font-weight: 600;
}

.tx-card__heading h4 .category-icon {
  margin-top: 1px;
}

.tx-card__heading p {
  margin-top: 2px;
  color: var(--hb-ink-faint);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  line-height: 1.55;
  overflow-wrap: anywhere;
}

.tx-card__time {
  white-space: nowrap;
}

.tx-card__amount {
  flex: 0 0 auto;
  padding-top: 1px;
  font-size: 16px;
  font-weight: 600;
  white-space: nowrap;
}

.tx-card__note {
  margin-top: 9px;
  color: var(--hb-ink-soft);
  font-size: 12px;
  line-height: 1.65;
  overflow-wrap: anywhere;
}

.tx-card__foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-top: 10px;
  padding-top: 9px;
  border-top: 1px dashed var(--hb-line-strong);
}

.tx-card__meta {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  color: var(--hb-ink-faint);
  font-size: 10px;
}

.tx-card__owner {
  min-width: 0;
  max-width: 9rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tx-card__owner {
  color: var(--hb-ink-soft);
}

.tx-card__actions {
  display: flex;
  flex: 0 0 auto;
  gap: 0;
}

.tx-card__actions :deep(.el-button) {
  padding: 3px 5px;
}

@media (max-width: 520px) {
  .tx-card {
    padding: 13px;
  }

  .tx-card__amount {
    font-size: 15px;
  }

  .tx-card__meta {
    flex-wrap: wrap;
    gap: 5px;
  }
}
</style>
