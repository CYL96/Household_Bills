<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  ArrowDown,
  ArrowUp,
  Plus,
  RefreshLeft,
  Search,
  Wallet,
  WarningFilled,
} from '@element-plus/icons-vue'
import EmptyState from '../components/EmptyState.vue'
import TransactionDialog from '../components/TransactionDialog.vue'
import TransactionList from '../components/TransactionList.vue'
import { listAccounts } from '../api/accounts'
import { getErrorMessage } from '../api/client'
import { listTransactions } from '../api/transactions'
import type { Account, Transaction, TransactionKind } from '../api/types'
import { dataVersion } from '../stores/data'
import { currentMonth, formatMonthLabel } from '../utils/format'

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

function isTransactionKind(value: unknown): value is TransactionKind {
  return (
    typeof value === 'string' &&
    (TRANSACTION_KINDS as readonly string[]).includes(value)
  )
}

const transactions = ref<Transaction[]>([])
const accounts = ref<Account[]>([])
const loading = ref(true)
const accountsLoading = ref(true)
const accountsError = ref('')
const listError = ref('')

const dialogVisible = ref(false)
const initialKind = ref<TransactionKind>('expense')

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
  initialKind.value = kind
  dialogVisible.value = true
}

function viewCategoryBills(selection: { kind: 'expense' | 'income'; categories: string[] }): void {
  monthFilter.value = null
  accountFilter.value = ''
  kindFilter.value = selection.kind
  searchQuery.value = ''
  categoryFilter.value = [...selection.categories]
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
          aria-label="记一笔"
          title="记一笔"
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

      <TransactionList
        :transactions="transactions"
        :accounts="accounts"
        :loading="loading"
        @view-bills="viewCategoryBills"
      >
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
      </TransactionList>
    </section>

    <TransactionDialog
      v-model="dialogVisible"
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
</style>
