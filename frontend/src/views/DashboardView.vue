<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import {
  ArrowRight,
  Plus,
  RefreshRight,
  TrendCharts,
  Wallet,
  WarningFilled,
} from '@element-plus/icons-vue'
import type { EChartsCoreOption } from 'echarts/core'
import BaseChart from '../components/BaseChart.vue'
import EmptyState from '../components/EmptyState.vue'
import StatCard from '../components/StatCard.vue'
import { listAccounts } from '../api/accounts'
import { getErrorMessage } from '../api/client'
import { getSummary } from '../api/summary'
import type { Account, Summary, TransactionKind } from '../api/types'
import { dataVersion } from '../stores/data'
import {
  currentMonth,
  formatAxisAmount,
  formatDayLabel,
  formatMoney,
  formatMonthLabel,
  toNumber,
} from '../utils/format'

const router = useRouter()

const month = ref(currentMonth())
const loading = ref(true)
const errorMessage = ref('')
const accounts = ref<Account[]>([])

function emptySummary(forMonth: string): Summary {
  return {
    month: forMonth,
    income: '0.00',
    expense: '0.00',
    net: '0.00',
    total_balance: '0.00',
    categories: [],
    daily: [],
  }
}

const summary = ref<Summary>(emptySummary(month.value))

const monthLabel = computed(() => formatMonthLabel(month.value))
const daily = computed(() => summary.value.daily)
const noAccounts = computed(() => accounts.value.length === 0)
const hasMonthEntries = computed(() => summary.value.daily.length > 0)

let loadSequence = 0

async function load(): Promise<void> {
  const sequence = ++loadSequence
  loading.value = true
  errorMessage.value = ''
  try {
    const [summaryData, accountList] = await Promise.all([
      getSummary(month.value),
      listAccounts(),
    ])
    if (sequence !== loadSequence) return
    summary.value = summaryData
    accounts.value = accountList
  } catch (error) {
    if (sequence !== loadSequence) return
    errorMessage.value = getErrorMessage(error)
  } finally {
    if (sequence === loadSequence) loading.value = false
  }
}

onMounted(() => {
  void load()
})

watch(month, () => {
  void load()
})

watch(dataVersion, () => {
  void load()
})

function goNewTransaction(kind: TransactionKind = 'expense'): void {
  void router.push({
    name: 'transactions',
    query: { new: '1', kind, month: month.value },
  })
}

function goNewAccount(): void {
  void router.push({ name: 'accounts', query: { new: '1' } })
}

function goTransactions(): void {
  void router.push({ name: 'transactions', query: { month: month.value } })
}

const trendOption = computed<EChartsCoreOption>(() => ({
  color: ['#0f766e', '#bd6a3c'],
  tooltip: {
    trigger: 'axis',
    confine: true,
    axisPointer: { type: 'shadow' },
    backgroundColor: 'rgba(38, 34, 28, 0.94)',
    borderWidth: 0,
    textStyle: { color: '#fffdf8', fontSize: 12 },
    valueFormatter: (value: unknown) => formatMoney(Number(value)),
  },
  legend: {
    data: ['收入', '支出'],
    bottom: 0,
    icon: 'roundRect',
    itemWidth: 10,
    itemHeight: 10,
    itemGap: 20,
    textStyle: { color: '#6d655a', fontSize: 12 },
  },
  grid: {
    left: 6,
    right: 14,
    top: 18,
    bottom: 44,
    containLabel: true,
  },
  xAxis: {
    type: 'category',
    data: daily.value.map((item) => formatDayLabel(item.date)),
    axisTick: { show: false },
    axisLine: { lineStyle: { color: '#e7dfd2' } },
    axisLabel: { color: '#7f7567', fontSize: 11 },
  },
  yAxis: {
    type: 'value',
    axisLine: { show: false },
    axisTick: { show: false },
    axisLabel: {
      color: '#7f7567',
      fontSize: 11,
      formatter: (value: unknown) => formatAxisAmount(Number(value)),
    },
    splitLine: { lineStyle: { color: '#efe8dd', type: 'dashed' } },
  },
  series: [
    {
      name: '收入',
      type: 'bar',
      data: daily.value.map((item) => toNumber(item.income)),
      barMaxWidth: 18,
      itemStyle: { color: '#0f766e', borderRadius: [4, 4, 0, 0] },
      emphasis: { focus: 'series' },
    },
    {
      name: '支出',
      type: 'bar',
      data: daily.value.map((item) => toNumber(item.expense)),
      barMaxWidth: 18,
      itemStyle: { color: '#bd6a3c', borderRadius: [4, 4, 0, 0] },
      emphasis: { focus: 'series' },
    },
  ],
}))

const trendChartLabel = computed(() => {
  if (daily.value.length === 0) {
    return `${monthLabel.value}暂无每日收支数据`
  }
  return `${monthLabel.value}每日收入与支出柱状图，共 ${daily.value.length} 天有记录`
})
</script>

<template>
  <div class="dashboard has-mobile-fab">
    <div class="page-heading">
      <div class="page-heading__copy">
        <h2>{{ monthLabel }} · 收支概览</h2>
      </div>

      <div class="heading-actions">
        <el-date-picker
          v-model="month"
          type="month"
          value-format="YYYY-MM"
          format="YYYY年MM月"
          placeholder="选择月份"
          :clearable="false"
          class="month-picker"
          aria-label="选择汇总月份"
        />
        <el-button
          type="primary"
          :icon="Plus"
          class="mobile-entry-fab"
          aria-label="记一笔"
          title="记一笔"
          @click="goNewTransaction('expense')"
        >
          记一笔
        </el-button>
      </div>
    </div>

    <div
      v-if="loading"
      class="overview-loading"
      aria-busy="true"
      aria-live="polite"
    >
      <span class="sr-only">正在加载家庭收支总览</span>
      <div class="stats-grid">
        <div v-for="item in 4" :key="item" class="surface-card skeleton-stat">
          <el-skeleton animated>
            <template #template>
              <el-skeleton-item variant="text" style="width: 36%" />
              <el-skeleton-item
                variant="h1"
                style="width: 72%; margin-top: 22px"
              />
              <el-skeleton-item
                variant="text"
                style="width: 52%; margin-top: 20px"
              />
            </template>
          </el-skeleton>
        </div>
      </div>
      <div class="surface-card skeleton-chart">
        <el-skeleton animated :rows="7" />
      </div>
    </div>

    <div v-else-if="errorMessage" class="surface-card overview-error">
      <EmptyState
        title="暂时无法加载总览"
        :description="errorMessage"
        :icon="WarningFilled"
      >
        <template #actions>
          <el-button :icon="RefreshRight" @click="load">重新加载</el-button>
        </template>
      </EmptyState>
    </div>

    <div v-else class="overview">
      <section v-if="noAccounts" class="surface-card onboarding">
        <div class="onboarding__icon" aria-hidden="true">
          <el-icon :size="24"><Wallet /></el-icon>
        </div>
        <div class="onboarding__copy">
          <p class="page-eyebrow">开始使用</p>
          <h3>先为家庭添加一个资金账户</h3>
          <p>
            资金账户用于归集收入、支出与账户转账。添加账户后，余额会随每一笔记录自动更新。
          </p>
        </div>
        <div class="onboarding__side">
          <div class="onboarding__steps" aria-label="使用步骤">
            <span class="is-current">1. 添加账户</span>
            <span>2. 记录收支与转账</span>
          </div>
          <el-button type="primary" :icon="Plus" @click="goNewAccount">
            添加资金账户
          </el-button>
        </div>
      </section>

      <div class="stats-grid">
        <StatCard
          label="收入"
          :amount="formatMoney(summary.income)"
          hint="所选月份家庭全部收入"
          tone="teal"
        />
        <StatCard
          label="支出"
          :amount="formatMoney(summary.expense)"
          hint="所选月份家庭全部支出"
          tone="terracotta"
        />
        <StatCard
          label="结余"
          :amount="formatMoney(summary.net)"
          hint="收入减去支出"
          tone="ink"
        />
        <StatCard
          label="家庭总余额"
          :amount="formatMoney(summary.total_balance)"
          hint="各账户当前余额合计；转账在账户间抵消"
          tone="balance"
        />
      </div>

      <section class="surface-card chart-card">
        <header class="section-head">
          <div>
            <h3>每日收支走势</h3>
            <p>{{ monthLabel }}有记录日期的收入与支出</p>
          </div>
          <span v-if="hasMonthEntries" class="chart-caption">
            按日汇总
          </span>
        </header>

        <EmptyState
          v-if="daily.length === 0"
          title="暂时没有收支记录"
          description="所选月份还没有任何收支。从第一笔记录开始，趋势会在这里呈现；转账不计入收支走势。"
          :icon="TrendCharts"
        >
          <template #actions>
            <el-button
              type="primary"
              plain
              :icon="Plus"
              @click="goNewTransaction('expense')"
            >
              记一笔支出
            </el-button>
          </template>
        </EmptyState>
        <BaseChart
          v-else
          :option="trendOption"
          height="330px"
          :aria-label="trendChartLabel"
        />
      </section>

      <div class="overview-foot">
        <button type="button" class="text-link" @click="goTransactions">
          查看{{ monthLabel }}流水明细
          <el-icon :size="14"><ArrowRight /></el-icon>
        </button>
        <p>家庭总余额按全部账户计算，不受月份筛选影响；账户转账在内部抵消。</p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.dashboard {
  min-width: 0;
}

.month-picker {
  width: 152px;
}

.stats-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 14px;
}

.overview {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.onboarding {
  display: flex;
  align-items: center;
  gap: 18px;
  padding: 22px 24px;
  background:
    radial-gradient(420px 180px at 100% 0%, rgba(15, 118, 110, 0.09), transparent 72%),
    var(--hb-surface);
}

.onboarding__icon {
  display: grid;
  flex: 0 0 auto;
  width: 52px;
  height: 52px;
  place-items: center;
  color: var(--hb-teal);
  background: var(--hb-teal-faint);
  border: 1px solid #d4e8e3;
  border-radius: 16px;
}

.onboarding__copy {
  flex: 1;
  min-width: 0;
}

.onboarding__copy .page-eyebrow {
  margin-bottom: 4px;
  font-size: 10px;
}

.onboarding__copy h3 {
  font-size: 18px;
}

.onboarding__copy > p:last-child {
  max-width: 540px;
  margin-top: 5px;
  color: var(--hb-ink-soft);
  font-size: 13px;
}

.onboarding__side {
  display: flex;
  flex: 0 0 auto;
  flex-direction: column;
  align-items: flex-end;
  gap: 10px;
}

.onboarding__steps {
  display: flex;
  gap: 12px;
  color: var(--hb-ink-faint);
  font-size: 11px;
}

.onboarding__steps .is-current {
  color: var(--hb-teal);
  font-weight: 600;
}

.chart-card {
  min-width: 0;
  padding: 20px 22px 16px;
}

.chart-card .section-head {
  margin-bottom: 8px;
}

.chart-caption {
  padding-top: 5px;
  color: var(--hb-ink-faint);
  font-size: 11px;
}

.overview-foot {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 2px 2px 0;
}

.overview-foot p {
  color: var(--hb-ink-faint);
  font-size: 11px;
}

.text-link {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 0;
  color: var(--hb-teal);
  background: none;
  border: 0;
  font-family: inherit;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
}

.text-link:hover {
  color: var(--hb-teal-dark);
  text-decoration: underline;
}

.overview-loading {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.skeleton-stat {
  min-height: 156px;
  padding: 22px;
}

.skeleton-chart {
  min-height: 390px;
  padding: 24px;
}

.overview-error {
  padding: 26px;
}

@media (max-width: 1180px) {
  .stats-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 760px) {
  .onboarding {
    align-items: flex-start;
    flex-wrap: wrap;
  }

  .onboarding__side {
    align-items: flex-start;
    width: 100%;
    padding-left: 70px;
  }
}

@media (max-width: 640px) {
  .stats-grid {
    grid-template-columns: minmax(0, 1fr);
    gap: 10px;
  }

  .chart-card {
    padding: 18px 16px 12px;
  }

  .month-picker {
    width: 144px;
  }

  .heading-actions {
    justify-content: space-between;
  }

  .onboarding {
    padding: 18px;
    gap: 14px;
  }

  .onboarding__icon {
    width: 42px;
    height: 42px;
    border-radius: 13px;
  }

  .onboarding__copy h3 {
    font-size: 16px;
  }

  .onboarding__side {
    padding-left: 0;
    align-items: flex-start;
  }

  .onboarding__side .el-button {
    width: 100%;
  }

  .skeleton-stat {
    min-height: 136px;
    padding: 18px;
  }

  .skeleton-chart {
    min-height: 330px;
    padding: 18px;
  }
}

@media (max-width: 420px) {
  .heading-actions {
    display: grid;
    grid-template-columns: 1fr auto;
  }

  .month-picker {
    width: 100%;
  }
}
</style>
