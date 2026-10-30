<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  ArrowDown,
  ArrowUp,
  Delete,
  EditPen,
  Plus,
  RefreshLeft,
  Search,
  Wallet,
  WarningFilled,
} from '@element-plus/icons-vue'
import EmptyState from '../components/EmptyState.vue'
import TransactionDialog from '../components/TransactionDialog.vue'
import { listAccounts } from '../api/accounts'
import { getErrorMessage } from '../api/client'
import { deleteTransaction, listTransactions } from '../api/transactions'
import type { Account, Transaction, TransactionKind } from '../api/types'
import { useMediaQuery } from '../composables/useMediaQuery'
import { dataVersion, markDataChanged } from '../stores/data'
import {
  currentMonth,
  formatDateTimeLabel,
  formatMonthLabel,
  formatSignedAmount,
} from '../utils/format'

const route = useRoute()
const router = useRouter()

function routeCategories(): string[] {
  const value = route.query.category
  const values = Array.isArray(value) ? value : [value]
  return [...new Set(values.filter((name): name is string =>
    typeof name === 'string' && name.trim() !== '',
  ))]
}

function initialMonth(): string | null {
  const queryMonth = route.query.month
  if (typeof queryMonth === 'string' && /^\d{4}-(0[1-9]|1[0-2])$/.test(queryMonth)) {
    return queryMonth
  }
  if (routeCategories().length > 0) return null
  return currentMonth()
}

const monthFilter = ref<string | null>(initialMonth())
const accountFilter = ref<number | ''>('')
const kindFilter = ref<TransactionKind | ''>('')
const searchQuery = ref('')
const categoryFilter = ref<string[]>(routeCategories())

const filtersOpen = ref(false)

const activeExtraFilterCount = computed(() => {
  let count = 0
  if (typeof accountFilter.value === 'number') count += 1
  if (kindFilter.value !== '') count += 1
  if (searchQuery.value.trim() !== '') count += 1
  if (categoryFilter.value.length > 0) count += 1
  return count
})

const TRANSACTION_KINDS: readonly TransactionKind[] = [
  'expense',
  'income',
  'transfer',
]

const KIND_LABELS: Record<TransactionKind, string> = {
  expense: '支出',
  income: '收入',
  transfer: '转账',
}

function isTransactionKind(value: unknown): value is TransactionKind {
  return (
    typeof value === 'string' &&
    (TRANSACTION_KINDS as readonly string[]).includes(value)
  )
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

const transactions = ref<Transaction[]>([])
const accounts = ref<Account[]>([])
const loading = ref(true)
const accountsLoading = ref(true)
const accountsError = ref('')
const listError = ref('')

const dialogVisible = ref(false)
const editingTransaction = ref<Transaction | null>(null)
const initialKind = ref<TransactionKind>('expense')
const deletingId = ref<number | null>(null)

const isMobile = useMediaQuery('(max-width: 760px)')

let searchTimer: ReturnType<typeof setTimeout> | null = null
let transactionsRequest = 0
let accountsRequest = 0

const resultsTitle = computed(() =>
  monthFilter.value
    ? `${formatMonthLabel(monthFilter.value)}流水`
    : '全部月份流水',
)

const hasNoAccounts = computed(
  () => !accountsLoading.value && !accountsError.value && accounts.value.length === 0,
)

async function loadAccounts(): Promise<void> {
  const request = ++accountsRequest
  accountsLoading.value = true
  accountsError.value = ''
  try {
    const result = await listAccounts()
    if (request !== accountsRequest) return
    accounts.value = result
  } catch (error) {
    if (request !== accountsRequest) return
    accountsError.value = getErrorMessage(error)
  } finally {
    if (request === accountsRequest) accountsLoading.value = false
  }
}

async function loadTransactions(): Promise<void> {
  const request = ++transactionsRequest
  loading.value = true
  listError.value = ''
  try {
    const result = await listTransactions({
      month: monthFilter.value ?? undefined,
      account:
        typeof accountFilter.value === 'number' ? accountFilter.value : undefined,
      kind: kindFilter.value || undefined,
      q: searchQuery.value.trim() || undefined,
      category: categoryFilter.value,
    })
    if (request !== transactionsRequest) return
    transactions.value = result
  } catch (error) {
    if (request !== transactionsRequest) return
    transactions.value = []
    listError.value = getErrorMessage(error)
  } finally {
    if (request === transactionsRequest) loading.value = false
  }
}

function openCreate(kind: TransactionKind = 'expense'): void {
  editingTransaction.value = null
  initialKind.value = kind
  dialogVisible.value = true
}

function viewCategoryBills(selection: { kind: 'expense' | 'income'; categories: string[] }): void {
  dialogVisible.value = false
  monthFilter.value = null
  accountFilter.value = ''
  kindFilter.value = selection.kind
  searchQuery.value = ''
  categoryFilter.value = [...selection.categories]
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
  initialKind.value = transaction.kind
  editingTransaction.value = transaction
  dialogVisible.value = true
}

async function confirmDeleteRow(row: unknown): Promise<void> {
  if (!isTransactionRow(row)) return
  await confirmDelete(row)
}

function goCreateAccount(): void {
  void router.push({ name: 'accounts', query: { new: '1' } })
}

function resetFilters(): void {
  monthFilter.value = currentMonth()
  accountFilter.value = ''
  kindFilter.value = ''
  searchQuery.value = ''
  clearCategoryFilter()
}

function clearCategoryFilter(): void {
  categoryFilter.value = []
  if (route.query.category === undefined) return
  const query = { ...route.query }
  delete query.category
  void router.replace({ query })
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

async function consumeNewQuery(): Promise<void> {
  if (route.query.new !== '1') return
  const query: Record<string, string | null | (string | null)[]> = {}
  for (const [key, value] of Object.entries(route.query)) {
    if (key === 'new' || key === 'kind' || value === undefined) continue
    query[key] = value
  }
  await router.replace({ query })
}

watch(
  () => [route.query.category, route.query.kind, route.query.month],
  () => {
    categoryFilter.value = routeCategories()
    if (categoryFilter.value.length === 0) return
    kindFilter.value = isTransactionKind(route.query.kind) ? route.query.kind : ''
    monthFilter.value = initialMonth()
    accountFilter.value = ''
    searchQuery.value = ''
  },
  { immediate: true },
)

onMounted(async () => {
  const queryKind = route.query.kind
  const requestedKind = isTransactionKind(queryKind) ? queryKind : 'expense'
  await Promise.all([loadAccounts(), loadTransactions()])
  if (route.query.new === '1') {
    openCreate(requestedKind)
    await consumeNewQuery()
  }
})

watch([monthFilter, accountFilter, kindFilter, categoryFilter], () => {
  void loadTransactions()
})

watch(searchQuery, () => {
  // Invalidate any in-flight request immediately so old query results cannot
  // briefly replace the list while the next search waits for the debounce.
  transactionsRequest += 1
  if (searchTimer !== null) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    searchTimer = null
    void loadTransactions()
  }, 320)
})

watch(dataVersion, () => {
  void loadAccounts()
  void loadTransactions()
})

onBeforeUnmount(() => {
  if (searchTimer !== null) {
    clearTimeout(searchTimer)
    searchTimer = null
  }
})
</script>

<template>
  <div class="transactions-page has-mobile-fab">
    <div class="page-heading">
      <div class="heading-actions heading-actions--fab">
        <el-button
          type="primary"
          :icon="Plus"
          class="mobile-entry-fab"
          @click="openCreate('expense')"
        >
          记一笔
        </el-button>
      </div>
    </div>

    <div v-if="accountsError" class="inline-note inline-note--error" role="alert">
      <span>账户列表加载失败：{{ accountsError }}</span>
      <el-button text type="primary" @click="loadAccounts">重新加载</el-button>
    </div>
    <div v-else-if="hasNoAccounts" class="inline-note">
      <el-icon :size="17" aria-hidden="true"><Wallet /></el-icon>
      <span>还没有资金账户，先创建一个账户才能开始记账。</span>
      <el-button text type="primary" @click="goCreateAccount">
        创建资金账户
      </el-button>
    </div>

    <section class="surface-card filter-panel" aria-label="流水筛选">
      <div class="filter-summary">
        <div class="filter-month">
          <el-date-picker
            v-model="monthFilter"
            type="month"
            value-format="YYYY-MM"
            format="YYYY年MM月"
            placeholder="全部月份"
            clearable
            aria-label="按月份筛选"
          />
        </div>

        <el-button
          class="filter-toggle"
          :class="{ 'is-active': activeExtraFilterCount > 0 }"
          :aria-expanded="filtersOpen"
          aria-controls="transaction-filter-more"
          @click="filtersOpen = !filtersOpen"
        >
          <span>筛选</span>
          <span v-if="activeExtraFilterCount > 0" class="filter-toggle__count">
            {{ activeExtraFilterCount }}
          </span>
          <el-icon aria-hidden="true">
            <ArrowUp v-if="filtersOpen" />
            <ArrowDown v-else />
          </el-icon>
        </el-button>

        <el-button
          class="filter-reset"
          :icon="RefreshLeft"
          title="重置筛选"
          aria-label="重置筛选"
          @click="resetFilters"
        />
      </div>

      <div v-if="categoryFilter.length > 0" class="category-filter">
        <span>分类：{{ categoryFilter.join('、') }}</span>
        <el-button text size="small" @click="clearCategoryFilter">清除分类筛选</el-button>
      </div>

      <div v-show="filtersOpen" id="transaction-filter-more" class="filter-more">
        <div class="filter-grid">
          <div class="filter-field">
            <span class="filter-label">资金账户</span>
            <el-select
              v-model="accountFilter"
              placeholder="全部账户"
              clearable
              :loading="accountsLoading"
              aria-label="按资金账户筛选"
            >
              <el-option
                v-for="account in accounts"
                :key="account.id"
                :label="account.name"
                :value="account.id"
              />
            </el-select>
          </div>

          <div class="filter-field">
            <span class="filter-label">类型</span>
            <el-select
              v-model="kindFilter"
              placeholder="全部类型"
              clearable
              aria-label="按类型筛选"
            >
              <el-option label="收入" value="income" />
              <el-option label="支出" value="expense" />
              <el-option label="转账" value="transfer" />
            </el-select>
          </div>

          <div class="filter-field filter-field--search">
            <span class="filter-label">搜索</span>
            <el-input
              v-model="searchQuery"
              clearable
              :prefix-icon="Search"
              placeholder="搜索分类或备注"
              aria-label="搜索分类或备注"
            />
          </div>
        </div>
      </div>
    </section>

    <div v-if="listError" class="surface-card list-error">
      <EmptyState
        title="暂时无法加载流水明细"
        :description="listError"
        :icon="WarningFilled"
      >
        <template #actions>
          <el-button :icon="RefreshLeft" @click="loadTransactions">
            重新加载
          </el-button>
        </template>
      </EmptyState>
    </div>

    <section v-else class="surface-card ledger-panel">
      <div class="results-head">
        <div>
          <h3>{{ resultsTitle }}</h3>
          <p>
            <span v-if="loading">正在加载记录…</span>
            <span v-else>共 {{ transactions.length }} 笔记录</span>
          </p>
        </div>
        <span class="results-mark" aria-hidden="true">账</span>
      </div>

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
                  <h4>{{ transaction.category }}</h4>
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

          <EmptyState
            v-if="!loading && transactions.length === 0"
            title="暂无符合条件的记录"
            description="调整筛选条件，或记下这个月的第一笔家庭流水。"
          >
            <template #actions>
              <el-button :icon="RefreshLeft" @click="resetFilters">
                重置筛选
              </el-button>
              <el-button
                type="primary"
                :icon="Plus"
                @click="openCreate('expense')"
              >
                记一笔
              </el-button>
            </template>
          </EmptyState>
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
            <span class="category-cell">{{ row.category }}</span>
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
          <EmptyState
            title="暂无符合条件的记录"
            description="调整筛选条件，或记下这个月的第一笔家庭流水。"
          >
            <template #actions>
              <el-button :icon="RefreshLeft" @click="resetFilters">
                重置筛选
              </el-button>
              <el-button
                type="primary"
                :icon="Plus"
                @click="openCreate('expense')"
              >
                记一笔
              </el-button>
            </template>
          </EmptyState>
        </template>
      </el-table>
    </section>

    <TransactionDialog
      v-model="dialogVisible"
      :transaction="editingTransaction"
      :accounts="accounts"
      :initial-kind="initialKind"
      @view-bills="viewCategoryBills"
    />
  </div>
</template>

<style scoped>
.transactions-page {
  min-width: 0;
}

.page-heading {
  justify-content: flex-end;
}

.inline-note {
  display: flex;
  align-items: center;
  gap: 9px;
  margin-bottom: 14px;
  padding: 11px 15px;
  color: #745b33;
  background: #fcf6eb;
  border: 1px solid #ecdfc8;
  border-radius: 13px;
  font-size: 12px;
}

.inline-note > span {
  flex: 1;
  min-width: 0;
}

.inline-note--error {
  color: #8a4136;
  background: var(--hb-danger-soft);
  border-color: #f0d4cd;
}

.filter-panel {
  padding: 14px 16px;
  margin-bottom: 15px;
}

.category-filter {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 10px;
  font-size: 12px;
  color: var(--hb-ink-soft);
}

.filter-summary {
  display: flex;
  align-items: center;
  gap: 8px;
}

.filter-month {
  flex: 1 1 auto;
  min-width: 0;
  max-width: 280px;
}

.filter-month :deep(.el-date-editor.el-input) {
  width: 100%;
}

.filter-month :deep(.el-input__inner) {
  min-width: 0;
}

.filter-toggle {
  flex: 0 0 auto;
  margin-left: auto;
  padding: 0 12px;
}

.filter-toggle :deep(span) {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.filter-toggle.is-active {
  color: var(--hb-teal-dark);
  background: var(--hb-teal-faint);
  border-color: var(--hb-teal);
}

.filter-toggle__count {
  justify-content: center;
  min-width: 18px;
  height: 18px;
  padding: 0 5px;
  border-radius: 999px;
  background: var(--hb-teal);
  color: var(--hb-surface-raised);
  font-size: 11px;
  font-weight: 600;
  line-height: 18px;
}

.filter-summary .filter-reset {
  flex: 0 0 auto;
  width: 32px;
  height: 32px;
  margin-left: 0;
  padding: 0;
}

.filter-more {
  padding-top: 12px;
}

.filter-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 13px;
}

.filter-field {
  display: flex;
  flex-direction: column;
  min-width: 0;
  gap: 6px;
}

.filter-label {
  color: var(--hb-ink-faint);
  font-size: 11px;
  font-weight: 500;
  letter-spacing: 0.04em;
}

.filter-field :deep(.el-select),
.filter-field :deep(.el-input) {
  width: 100%;
}

.ledger-panel {
  padding: 18px 20px 10px;
  min-width: 0;
}

.results-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 0 2px 14px;
}

.results-head h3 {
  font-size: 17px;
}

.results-head p {
  margin-top: 3px;
  color: var(--hb-ink-faint);
  font-size: 11px;
}

.results-mark {
  display: grid;
  width: 32px;
  height: 32px;
  place-items: center;
  color: var(--hb-teal);
  background: var(--hb-teal-faint);
  border: 1px solid #d9eae5;
  border-radius: 10px;
  font-family: var(--hb-display);
  font-size: 14px;
}

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
  font-weight: 500;
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
  overflow-wrap: anywhere;
  font-size: 14px;
  font-weight: 600;
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

.list-error {
  padding: 12px;
}

@media (max-width: 760px) {
  .page-heading {
    display: contents;
  }

  .filter-panel {
    padding: 12px;
  }

  .filter-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 11px;
  }

  .filter-field--search {
    grid-column: 1 / -1;
  }

  .filter-toggle {
    padding: 0 10px;
  }

  .ledger-panel {
    padding: 15px 14px 8px;
  }

  .results-head {
    padding-bottom: 11px;
  }

  .inline-note {
    align-items: flex-start;
    flex-wrap: wrap;
  }

  .inline-note .el-button {
    padding-left: 0;
  }
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
