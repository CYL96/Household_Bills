<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import {
  ArrowLeft,
  ArrowRight,
  Calendar,
  RefreshRight,
} from '@element-plus/icons-vue'
import EmptyState from '../components/EmptyState.vue'
import TransactionList from '../components/TransactionList.vue'
import { listAccounts } from '../api/accounts'
import { getErrorMessage } from '../api/client'
import { getSummary } from '../api/summary'
import { listTransactions } from '../api/transactions'
import type {
  Account,
  Summary,
  SummaryDay,
  Transaction,
} from '../api/types'
import { dataVersion } from '../stores/data'
import {
  formatDateLabel,
  formatMoney,
  formatMonthLabel,
  localDayString,
  monthEndDate,
  pad2,
  toNumber,
} from '../utils/format'

/** Monday-first weekday headings for the month grid. */
const WEEKDAYS = ['一', '二', '三', '四', '五', '六', '日'] as const
const WEEKDAY_NAMES = [
  '星期日',
  '星期一',
  '星期二',
  '星期三',
  '星期四',
  '星期五',
  '星期六',
] as const


interface CalendarCell {
  key: string
  blank: boolean
  date: string
  dayNumber: number
  income: string
  expense: string
}

const today = ref(localDayString(new Date()))
const month = ref(today.value.slice(0, 7))
const selectedDate = ref(today.value)

const summary = ref<Summary | null>(null)
const summaryLoading = ref(true)
const summaryError = ref('')

const dayTransactions = ref<Transaction[]>([])
const loadedDayDate = ref('')
const dayLoading = ref(true)
const dayError = ref('')

const accounts = ref<Account[]>([])
const accountsLoading = ref(true)
const accountsError = ref('')

let summarySequence = 0
let daySequence = 0
let accountsSequence = 0

const monthLabel = computed(() => formatMonthLabel(month.value))
const selectedDateLabel = computed(() => formatDateLabel(selectedDate.value))

const monthModel = computed<string>({
  get: () => month.value,
  set: (value) => {
    if (typeof value === 'string') setMonth(value)
  },
})

/**
 * The selected date belongs to `month`, but its transaction list is only
 * exposed once the response for that exact date arrives. This keeps an
 * in-flight previous day from ever rendering under the new date.
 */
const visibleDayTransactions = computed<Transaction[]>(() =>
  loadedDayDate.value === selectedDate.value ? dayTransactions.value : [],
)

const dailyByDate = computed(() => {
  const map = new Map<string, SummaryDay>()
  const current = summary.value
  if (!current || current.month !== month.value) return map
  for (const item of current.daily) map.set(item.date, item)
  return map
})

const summaryReady = computed(
  () => summary.value?.month === month.value,
)

const selectedDayTotals = computed<SummaryDay | null>(() => {
  if (!summaryReady.value) return null
  return dailyByDate.value.get(selectedDate.value) ?? null
})

const calendarCells = computed<CalendarCell[]>(() => {
  const parsed = parseMonth(month.value)
  if (!parsed) return []

  const daysInMonth = Number(monthEndDate(month.value).slice(8, 10))
  const firstWeekday = new Date(
    parsed.year,
    parsed.monthIndex - 1,
    1,
  ).getDay()
  const leading = (firstWeekday + 6) % 7
  const cellCount = Math.ceil((leading + daysInMonth) / 7) * 7
  const cells: CalendarCell[] = []

  for (let index = 0; index < cellCount; index += 1) {
    const dayNumber = index - leading + 1
    if (dayNumber < 1 || dayNumber > daysInMonth) {
      cells.push({
        key: `blank-${index}`,
        blank: true,
        date: '',
        dayNumber: 0,
        income: '',
        expense: '',
      })
      continue
    }
    const date = `${month.value}-${pad2(dayNumber)}`
    const totals = dailyByDate.value.get(date)
    cells.push({
      key: date,
      blank: false,
      date,
      dayNumber,
      income: totals?.income ?? '0.00',
      expense: totals?.expense ?? '0.00',
    })
  }
  return cells
})

const isViewingToday = computed(
  () =>
    selectedDate.value === today.value &&
    month.value === today.value.slice(0, 7),
)

const dayHasTransfer = computed(() =>
  visibleDayTransactions.value.some(
    (transaction) => transaction.kind === 'transfer',
  ),
)

const editingDisabled = computed(
  () =>
    accountsLoading.value ||
    accountsError.value !== '' ||
    accounts.value.length === 0,
)

function parseMonth(
  value: string,
): { year: number; monthIndex: number } | null {
  const match = /^(\d{4})-(\d{2})$/.exec(value)
  if (!match) return null
  const year = Number(match[1])
  const monthIndex = Number(match[2])
  if (monthIndex < 1 || monthIndex > 12) return null
  return { year, monthIndex }
}

function weekdayLabel(date: string): string {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date)
  if (!match) return ''
  const weekday = new Date(
    Number(match[1]),
    Number(match[2]) - 1,
    Number(match[3]),
  ).getDay()
  return WEEKDAY_NAMES[weekday] ?? ''
}


function daySummaryText(cell: CalendarCell): string {
  return `收入 ${formatMoney(cell.income)}，支出 ${formatMoney(cell.expense)}`
}

function dayAriaLabel(cell: CalendarCell): string {
  const parts = [formatDateLabel(cell.date), weekdayLabel(cell.date)]
  if (cell.date === today.value) parts.push('今天')
  if (summaryReady.value) parts.push(daySummaryText(cell))
  if (cell.date === selectedDate.value) parts.push('当前选中')
  return parts.filter((part) => part !== '').join('，')
}

function dayTitle(cell: CalendarCell): string {
  const label = `${formatDateLabel(cell.date)} ${weekdayLabel(cell.date)}`
  if (!summaryReady.value) return label
  return `${label} · ${daySummaryText(cell)}`
}

function setMonth(next: string): void {
  if (!parseMonth(next)) return
  if (next === month.value) return
  const todayDate = localDayString(new Date())
  today.value = todayDate
  month.value = next
  selectedDate.value =
    next === todayDate.slice(0, 7) ? todayDate : `${next}-01`
}

function shiftMonth(delta: number): void {
  const parsed = parseMonth(month.value)
  if (!parsed) return
  const total = parsed.year * 12 + (parsed.monthIndex - 1) + delta
  const year = Math.floor(total / 12)
  const monthIndex = ((total % 12) + 12) % 12
  setMonth(`${String(year).padStart(4, '0')}-${pad2(monthIndex + 1)}`)
}

function goToday(): void {
  const now = new Date()
  const todayDate = localDayString(now)
  today.value = todayDate
  month.value = todayDate.slice(0, 7)
  selectedDate.value = todayDate
}

function selectDate(date: string): void {
  if (date === selectedDate.value) return
  selectedDate.value = date
}

async function loadSummary(quiet = false): Promise<void> {
  const sequence = ++summarySequence
  const requestedMonth = month.value
  summaryLoading.value = true
  summaryError.value = ''
  if (!quiet) summary.value = null
  try {
    const result = await getSummary(requestedMonth)
    if (sequence !== summarySequence || requestedMonth !== month.value) return
    summary.value = result
  } catch (error) {
    if (sequence !== summarySequence || requestedMonth !== month.value) return
    summary.value = null
    summaryError.value = getErrorMessage(error)
  } finally {
    if (sequence === summarySequence) summaryLoading.value = false
  }
}

async function loadDay(quiet = false): Promise<void> {
  const sequence = ++daySequence
  const requestedDate = selectedDate.value
  dayLoading.value = true
  dayError.value = ''
  if (!quiet) dayTransactions.value = []
  try {
    const result = await listTransactions({
      start: requestedDate,
      end: requestedDate,
    })
    if (sequence !== daySequence || requestedDate !== selectedDate.value) return
    dayTransactions.value = result
    loadedDayDate.value = requestedDate
  } catch (error) {
    if (sequence !== daySequence || requestedDate !== selectedDate.value) return
    dayTransactions.value = []
    dayError.value = getErrorMessage(error)
  } finally {
    if (sequence === daySequence) dayLoading.value = false
  }
}

/**
 * Editing writes back an account, so it stays unavailable until the account
 * snapshot is present. Deletion never depends on it.
 */
async function loadAccounts(): Promise<void> {
  const sequence = ++accountsSequence
  accountsLoading.value = true
  accountsError.value = ''
  try {
    const result = await listAccounts()
    if (sequence !== accountsSequence) return
    accounts.value = result
  } catch (error) {
    if (sequence !== accountsSequence) return
    accountsError.value = getErrorMessage(error)
  } finally {
    if (sequence === accountsSequence) accountsLoading.value = false
  }
}

function retrySummary(): void {
  void loadSummary()
}

function retryDay(): void {
  void loadDay()
}

onMounted(() => {
  void loadSummary()
  void loadDay()
  void loadAccounts()
})

watch(month, () => {
  void loadSummary()
})

watch(selectedDate, () => {
  void loadDay()
})

watch(dataVersion, () => {
  void loadSummary(true)
  void loadDay(true)
  void loadAccounts()
})
</script>

<template>
  <div class="calendar-page">
    <section
      class="surface-card calendar-card"
      aria-labelledby="calendar-title"
    >
      <header class="calendar-toolbar">
        <div class="calendar-title">
          <el-button
            class="calendar-nav"
            circle
            :icon="ArrowLeft"
            title="上个月"
            aria-label="上个月"
            @click="shiftMonth(-1)"
          />
          <h3 id="calendar-title" class="calendar-title__label">
            {{ monthLabel }}
          </h3>
          <el-button
            class="calendar-nav"
            circle
            :icon="ArrowRight"
            title="下个月"
            aria-label="下个月"
            @click="shiftMonth(1)"
          />
        </div>

        <div class="calendar-actions">
          <el-date-picker
            v-model="monthModel"
            type="month"
            value-format="YYYY-MM"
            format="YYYY年MM月"
            placeholder="选择月份"
            :clearable="false"
            :editable="false"
            class="month-picker"
            aria-label="选择日历月份"
          />
          <el-button
            :disabled="isViewingToday"
            @click="goToday"
          >
            回到今天
          </el-button>
        </div>
      </header>

      <div
        v-if="summaryError"
        class="inline-warning"
        role="alert"
      >
        <span>月历数据加载失败：{{ summaryError }}</span>
        <el-button text :icon="RefreshRight" @click="retrySummary">
          重试
        </el-button>
      </div>

      <div
        class="calendar-body"
        :aria-busy="summaryLoading"
      >
        <div class="calendar-weekdays" aria-hidden="true">
          <span
            v-for="weekday in WEEKDAYS"
            :key="weekday"
            class="calendar-weekday"
          >
            {{ weekday }}
          </span>
        </div>

        <div
          class="calendar-grid"
          role="group"
          :aria-label="`${monthLabel}日期列表`"
        >
          <template v-for="cell in calendarCells" :key="cell.key">
            <span
              v-if="cell.blank"
              class="day-cell day-cell--blank"
              aria-hidden="true"
            />
            <button
              v-else
              type="button"
              class="day-cell"
              :class="{
                'is-today': cell.date === today,
                'is-selected': cell.date === selectedDate,
              }"
              :aria-pressed="cell.date === selectedDate"
              :aria-current="cell.date === today ? 'date' : undefined"
              :aria-label="dayAriaLabel(cell)"
              :title="dayTitle(cell)"
              @click="selectDate(cell.date)"
            >
              <span class="day-cell__number">{{ cell.dayNumber }}</span>
              <span
                v-if="toNumber(cell.income) > 0"
                class="day-cell__amount day-cell__amount--income"
              >
                +{{ formatMoney(cell.income) }}
              </span>
              <span
                v-if="toNumber(cell.expense) > 0"
                class="day-cell__amount day-cell__amount--expense"
              >
                −{{ formatMoney(cell.expense) }}
              </span>
            </button>
          </template>
        </div>

        <p v-if="summaryLoading" class="calendar-status" role="status">
          正在加载月历数据…
        </p>
        <p
          v-else-if="!summaryError && summary && summary.daily.length === 0"
          class="calendar-status"
          role="status"
        >
          本月暂无收支记录。
        </p>
      </div>
    </section>

    <section
      class="surface-card day-panel"
      :aria-label="`${selectedDateLabel}流水明细`"
    >
      <div
        v-if="selectedDayTotals && (
          toNumber(selectedDayTotals.income) > 0 ||
          toNumber(selectedDayTotals.expense) > 0
        )"
        class="day-totals"
        aria-label="当日收支合计"
      >
        <span
          v-if="toNumber(selectedDayTotals.income) > 0"
          class="day-total day-total--income"
        >
          收入
          <strong class="money-number">
            {{ formatMoney(selectedDayTotals.income) }}
          </strong>
        </span>
        <span
          v-if="toNumber(selectedDayTotals.expense) > 0"
          class="day-total day-total--expense"
        >
          支出
          <strong class="money-number">
            {{ formatMoney(selectedDayTotals.expense) }}
          </strong>
        </span>
      </div>

      <p
        v-if="!dayLoading && !dayError && visibleDayTransactions.length > 0"
        class="day-panel__count"
      >
        共 {{ visibleDayTransactions.length }} 笔{{ dayHasTransfer ? '，含转账' : '' }}
      </p>

      <div
        v-if="accountsError"
        class="inline-warning"
        role="status"
      >
        <span>账户列表加载失败，暂时无法编辑明细：{{ accountsError }}</span>
        <el-button text :icon="RefreshRight" @click="loadAccounts">
          重试
        </el-button>
      </div>
      <div
        v-else-if="!accountsLoading && accounts.length === 0"
        class="day-panel__note"
        role="status"
      >
        还没有可用的资金账户，暂时无法编辑明细；删除记录仍可使用。
      </div>

      <div
        v-if="dayError"
        class="inline-warning"
        role="alert"
      >
        <span>当天流水加载失败：{{ dayError }}</span>
        <el-button text :icon="RefreshRight" @click="retryDay">
          重试
        </el-button>
      </div>

      <TransactionList
        v-if="!dayError"
        :transactions="visibleDayTransactions"
        :accounts="accounts"
        :loading="dayLoading"
        :editing-disabled="editingDisabled"
      >
        <template #empty>
          <EmptyState
            title="当天暂无流水"
            description="这一天还没有记录，切换日期看看其他日子。"
            :icon="Calendar"
          />
        </template>
      </TransactionList>
    </section>
  </div>
</template>

<style scoped>
.calendar-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.calendar-card {
  padding: 18px 22px 16px;
}

.calendar-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 14px;
}

.calendar-title {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.calendar-title__label {
  color: var(--hb-ink);
  font-size: 20px;
  white-space: nowrap;
}

.calendar-nav {
  flex: 0 0 auto;
  width: 32px;
  height: 32px;
  padding: 0;
}

.calendar-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.calendar-actions :deep(.month-picker) {
  width: 150px;
}


.month-picker :deep(.el-input__inner) {
  min-width: 0;
}

.calendar-body {
  min-width: 0;
}

.calendar-weekdays,
.calendar-grid {
  display: grid;
  grid-template-columns: repeat(7, minmax(0, 1fr));
  gap: 6px;
}

.calendar-weekdays {
  margin-bottom: 6px;
}

.calendar-weekday {
  color: var(--hb-ink-faint);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-align: center;
}

.day-cell {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 1px;
  min-width: 0;
  min-height: 54px;
  padding: 6px 2px 5px;
  color: var(--hb-ink);
  font: inherit;
  line-height: 1.25;
  background: #fbf8f2;
  border: 1px solid transparent;
  border-radius: var(--hb-radius-sm);
  cursor: pointer;
  transition:
    background 0.15s ease,
    border-color 0.15s ease;
}

.day-cell:hover {
  background: var(--hb-teal-faint);
  border-color: #d7e9e4;
}

.day-cell--blank {
  background: transparent;
  border-color: transparent;
  cursor: default;
}

.day-cell--blank:hover {
  background: transparent;
  border-color: transparent;
}

.day-cell__number {
  display: grid;
  width: 22px;
  height: 22px;
  place-items: center;
  color: inherit;
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  border-radius: 999px;
}

.day-cell.is-today .day-cell__number {
  color: var(--hb-teal-dark);
  font-weight: 700;
  background: var(--hb-teal-soft);
}

.day-cell.is-selected,
.day-cell.is-selected:hover {
  color: var(--hb-surface-raised);
  background: var(--hb-teal);
  border-color: var(--hb-teal-dark);
  box-shadow: 0 6px 16px rgba(15, 118, 110, 0.24);
}

.day-cell.is-selected .day-cell__number {
  color: var(--hb-surface-raised);
  background: transparent;
}

.day-cell__amount {
  max-width: 100%;
  overflow: hidden;
  font-size: 10px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  line-height: 1.3;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.day-cell__amount--income {
  color: var(--hb-teal);
}

.day-cell__amount--expense {
  color: var(--hb-terracotta);
}

.day-cell.is-selected .day-cell__amount--income {
  color: #d8f3ec;
}

.day-cell.is-selected .day-cell__amount--expense {
  color: #ffe2d2;
}

.calendar-status {
  margin-top: 10px;
  color: var(--hb-ink-soft);
  font-size: 12px;
}

.day-panel {
  padding: 18px 20px 10px;
}

.day-totals {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 14px;
}

.day-total {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 5px 10px;
  color: var(--hb-ink-soft);
  font-size: 12px;
  background: #faf6ef;
  border: 1px solid var(--hb-line);
  border-radius: 999px;
}

.day-total strong {
  color: var(--hb-ink);
  font-size: 13px;
  font-weight: 600;
}

.day-total--income {
  color: var(--hb-teal-dark);
  background: var(--hb-teal-faint);
  border-color: #d7e9e4;
}

.day-total--income strong {
  color: var(--hb-teal-dark);
}

.day-total--expense {
  color: var(--hb-terracotta);
  background: var(--hb-terracotta-soft);
  border-color: #ecd3c2;
}

.day-total--expense strong {
  color: var(--hb-terracotta);
}

.day-panel__count {
  margin: 0 0 12px;
  color: var(--hb-ink-faint);
  font-size: 12px;
}

.day-panel__note {
  padding: 10px 14px;
  margin-bottom: 12px;
  color: var(--hb-ink-soft);
  font-size: 12px;
  background: #faf6ef;
  border: 1px solid var(--hb-line);
  border-radius: 12px;
}

.inline-warning {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 12px 16px;
  margin-bottom: 12px;
  color: var(--hb-terracotta);
  font-size: 12px;
  line-height: 1.6;
  background: var(--hb-terracotta-soft);
  border: 1px solid #ecd3c2;
  border-radius: var(--hb-radius-md);
}

@media (max-width: 640px) {
  .calendar-card {
    padding: 14px 12px 12px;
  }

  .calendar-toolbar {
    margin-bottom: 12px;
  }

  .calendar-title {
    gap: 6px;
  }

  .calendar-title__label {
    font-size: 18px;
  }

  .calendar-actions {
    width: 100%;
  }

  .calendar-actions :deep(.month-picker) {
    flex: 1 1 140px;
    width: auto;
    min-width: 0;
  }

  .day-panel {
    padding: 16px 14px 6px;
  }

  .day-totals {
    margin-bottom: 12px;
  }
}

@media (max-width: 420px) {
  .calendar-card {
    padding: 12px 8px 10px;
  }

  .calendar-weekdays,
  .calendar-grid {
    gap: 3px;
  }

  .calendar-weekday {
    font-size: 10px;
  }

  .day-cell {
    min-height: 48px;
    padding: 4px 1px;
    border-radius: 8px;
  }

  .day-cell__number {
    width: 18px;
    height: 18px;
    font-size: 11px;
  }

  .day-cell__amount {
    font-size: 9px;
    letter-spacing: -0.02em;
  }

  .day-panel {
    padding: 14px 12px 4px;
  }

  .day-total {
    padding: 4px 8px;
  }
}
</style>
