<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { EChartsCoreOption } from 'echarts/core'
import {
  ArrowLeft,
  ArrowRight,
  PieChart,
  RefreshRight,
  Switch,
  WarningFilled,
} from '@element-plus/icons-vue'
import BaseChart from '../components/BaseChart.vue'
import { useMediaQuery } from '../composables/useMediaQuery'
import EmptyState from '../components/EmptyState.vue'
import TransactionList from '../components/TransactionList.vue'
import { listAccounts } from '../api/accounts'
import { getErrorMessage } from '../api/client'
import { listTransactions } from '../api/transactions'
import type { Account, Transaction } from '../api/types'
import { authState } from '../stores/auth'
import {
  categoryState,
  ensureCategories,
  readCategoryEntries,
} from '../stores/categories'
import { dataVersion } from '../stores/data'
import {
  buildCategoryReport,
  sumReportAmounts,
  type CategoryReport,
  type ReportCategory,
} from '../utils/reportStatistics'
import {
  currentMonth,
  formatMonthLabel,
  formatMoney,
  toNumber,
} from '../utils/format'

type ReportKind = 'expense' | 'income'
type CategoryMode = 'major' | 'minor'

const KIND_OPTIONS: ReadonlyArray<{ value: ReportKind; label: string }> = [
  { value: 'expense', label: '支出' },
  { value: 'income', label: '收入' },
]

const month = ref(currentMonth())
const kind = ref<ReportKind>('expense')
const categoryMode = ref<CategoryMode>('major')
const isMobile = useMediaQuery('(max-width: 760px)')
const isCompact = useMediaQuery('(max-width: 430px)')
const isNarrow = useMediaQuery('(max-width: 360px)')

const transactions = ref<Transaction[]>([])
const loaded = ref(false)
const loading = ref(true)
const loadError = ref('')

const accounts = ref<Account[]>([])
const accountsLoading = ref(true)
const accountsError = ref('')

const monthTotals = computed(() => sumReportAmounts(transactions.value))

/**
 * Editing writes back an account, so it stays unavailable until the account
 * snapshot is present. Deletion never depends on it.
 */
const editingDisabled = computed(
  () =>
    accountsLoading.value ||
    accountsError.value !== '' ||
    accounts.value.length === 0,
)

const selectedParentKey = ref<string | null>(null)
const selectedChildKey = ref<string | null>(null)

let loadSequence = 0
let accountsSequence = 0

const monthLabel = computed(() => formatMonthLabel(month.value))
const kindLabel = computed(() =>
  kind.value === 'expense' ? '支出' : '收入',
)

/**
 * The report only consumes the household snapshot that belongs to the current
 * identity. An unloaded or failed snapshot never falls back to guessed names.
 */
const categoryReady = computed(
  () =>
    categoryState.loaded &&
    categoryState.userId === (authState.user?.id ?? null) &&
    categoryState.householdId === (authState.household?.id ?? null),
)

const categoryEntries = computed(() =>
  categoryReady.value ? readCategoryEntries(kind.value) : [],
)

/**
 * The small-category view reuses the report builder with every entry treated
 * as top-level, so child categories become flat rows with their own amounts.
 */
const minorEntries = computed(() =>
  categoryEntries.value.map((entry) => {
    const copy = { ...entry }
    delete copy.parentName
    return copy
  }),
)

const report = computed<CategoryReport | null>(() => {
  if (!loaded.value || !categoryReady.value) return null
  return buildCategoryReport(
    transactions.value,
    categoryEntries.value,
    kind.value,
  )
})

const minorReport = computed<CategoryReport | null>(() => {
  if (!loaded.value || !categoryReady.value) return null
  return buildCategoryReport(
    transactions.value,
    minorEntries.value,
    kind.value,
  )
})

const rows = computed<ReportCategory[]>(() => report.value?.rows ?? [])

const minorRows = computed<ReportCategory[]>(
  () => minorReport.value?.rows ?? [],
)

/**
 * A flat report is only useful when it differs from the top-level one, i.e.
 * when some transaction is actually recorded in a child category. Comparing
 * the built rows keeps unknown historical categories and direct parent
 * entries out of the decision.
 */
const minorAvailable = computed(() => {
  if (minorRows.value.length === 0) return false
  if (minorRows.value.length !== rows.value.length) return true
  return minorRows.value.some((row, index) => {
    const major = rows.value[index]
    return (
      major === undefined ||
      major.key !== row.key ||
      major.amount !== row.amount ||
      major.count !== row.count
    )
  })
})

const effectiveMode = computed<CategoryMode>(() =>
  categoryMode.value === 'minor' && minorAvailable.value ? 'minor' : 'major',
)

const rootRows = computed<ReportCategory[]>(() =>
  effectiveMode.value === 'minor' ? minorRows.value : rows.value,
)

const selectedParent = computed<ReportCategory | null>(
  () => rows.value.find((row) => row.key === selectedParentKey.value) ?? null,
)

const children = computed<ReportCategory[]>(
  () => selectedParent.value?.children ?? [],
)

const selectedChild = computed<ReportCategory | null>(() => {
  if (selectedChildKey.value === null) return null
  return (
    children.value.find((row) => row.key === selectedChildKey.value) ?? null
  )
})

type ReportLevel = 'root' | 'children' | 'detail'

const level = computed<ReportLevel>(() => {
  if (selectedChild.value) return 'detail'
  const parent = selectedParent.value
  if (!parent) return 'root'
  // A selected child that disappeared with a refresh falls back to its
  // surviving parent list; a parent without children opens its detail.
  return parent.children.length > 0 ? 'children' : 'detail'
})

const levelRows = computed<ReportCategory[]>(() =>
  level.value === 'children' ? children.value : rootRows.value,
)

const activeCategory = computed<ReportCategory | null>(
  () => selectedChild.value ?? selectedParent.value,
)

const backLabel = computed(() => {
  if (level.value === 'detail' && selectedChild.value) return '返回二级分类'
  return '返回全部分类'
})

const chartRows = computed<ReportCategory[]>(() =>
  level.value === 'children' ? children.value : rootRows.value,
)

const chartLevelLabel = computed(() => {
  if (level.value === 'children') return '二级分类'
  return effectiveMode.value === 'minor' ? '小类' : '大类'
})

const chartAriaLabel = computed(() => {
  if (chartRows.value.length === 0) {
    return `${monthLabel.value}暂无可展示的${kindLabel.value}分类`
  }
  const names = chartRows.value
    .map((row) => `${row.name} ${percentageLabel(row.percentage)} ${formatMoney(row.amount)}`)
    .join('、')
  return `${monthLabel.value}${kindLabel.value}${chartLevelLabel.value}环形图，共 ${chartRows.value.length} 个分类：${names}`
})

/**
 * Picks the ring start angle whose slice midpoints split labels most evenly
 * between the left and right label columns, preferring angles near 12 o'clock
 * on ties. The same pass exposes the heavier side for the adaptive height.
 */
const chartLayout = computed(() => {
  const values = chartRows.value.map((row) =>
    Math.max(0, toNumber(row.amount)),
  )
  const total = values.reduce((sum, value) => sum + value, 0)
  if (values.length <= 1 || total <= 0) {
    return { startAngle: 90, maxSide: values.length }
  }

  let bestAngle = 90
  let bestMaxSide = values.length
  let bestDistance = Number.POSITIVE_INFINITY

  for (let candidate = 0; candidate < 360; candidate += 1) {
    let left = 0
    let right = 0
    let cumulative = 0
    for (const value of values) {
      const angle = (value / total) * 360
      const mid = -candidate + cumulative + angle / 2
      if (Math.cos((mid * Math.PI) / 180) >= 0) {
        right += 1
      } else {
        left += 1
      }
      cumulative += angle
    }
    const maxSide = Math.max(left, right)
    const distance = Math.abs(candidate - 90)
    if (
      maxSide < bestMaxSide ||
      (maxSide === bestMaxSide && distance < bestDistance)
    ) {
      bestAngle = candidate
      bestMaxSide = maxSide
      bestDistance = distance
    }
  }

  return { startAngle: bestAngle, maxSide: bestMaxSide }
})

/**
 * Pie labels are two text lines (name + percentage). The native pie label
 * pass stacks one block per label on each side and only starts squeezing
 * (overlapping) them once the chart cannot fit them, so the adaptive height
 * must reserve the real block height, not a rough per-label guess.
 */
const chartLabelMetrics = computed(() => {
  const mobile = isMobile.value
  const lineHeight = mobile ? 14 : 17
  const minMargin = mobile ? 4 : 6
  return {
    mobile,
    lineHeight,
    minMargin,
    // Two lines + the gap ECharts keeps between neighbouring label rects.
    slot: lineHeight * 2 + minMargin + 4,
  }
})

const chartHeight = computed(() => {
  const minimum = isMobile.value ? 320 : 400
  return `${Math.max(minimum, chartLayout.value.maxSide * chartLabelMetrics.value.slot + 24)}px`
})

/**
 * The outer radius is reused by the label clamp below, so the ring sizes live
 * in one place. Outside labels need horizontal room on both sides of the
 * ring; the pie radius is a share of the smaller chart dimension and tall
 * multi-label charts are width-limited, so the ring shrinks on narrow screens
 * to keep a full "100.00%" line between the label line and the chart edge.
 */
const chartRadius = computed<[string, string]>(() =>
  isNarrow.value
    ? ['33%', '45%']
    : isCompact.value
      ? ['40%', '52%']
      : isMobile.value
        ? ['40%', '60%']
        : ['42%', '60%'],
)

const chartCanvasEl = ref<HTMLDivElement | null>(null)
const chartWidth = ref(0)
let chartCanvasObserver: ResizeObserver | null = null

watch(
  chartCanvasEl,
  (el) => {
    chartCanvasObserver?.disconnect()
    chartCanvasObserver = null
    chartWidth.value = el?.clientWidth ?? 0
    if (!el || typeof ResizeObserver === 'undefined') return
    chartCanvasObserver = new ResizeObserver((entries) => {
      const width = entries[0]?.contentRect.width
      if (width) chartWidth.value = width
    })
    chartCanvasObserver.observe(el)
  },
  { flush: 'post' },
)

onBeforeUnmount(() => {
  chartCanvasObserver?.disconnect()
})

/**
 * ECharts re-anchors outside pie labels ("alignTo: none") on an ellipse
 * around the ring. That formula assumes the stacked labels stay near the
 * ring: once a side holds many labels, the stack is taller than the ring and
 * the labels pushed past the ring radius get an X offset of
 * sqrt(dy² - r²), which runs off the canvas and shrinks the label width until
 * even the percentage is truncated. Knowing the real ring geometry lets the
 * labelLayout hook clamp only those runaway labels back to the near-ring
 * column, while the remaining labels keep their native hugging positions.
 */
const chartGeometry = computed(() => {
  const width = chartWidth.value
  const height = Number.parseFloat(chartHeight.value)
  if (!(width > 0) || !(height > 0)) return null
  const radius =
    (Math.min(width, height) / 2) *
    (Number.parseFloat(chartRadius.value[1]) / 100)
  return { width, cx: width / 2, radius }
})

const CATEGORY_COLORS = [
  '#2f6f66',
  '#8fb3a5',
  '#3f7f86',
  '#a8bda9',
  '#5d8a7a',
  '#6f9fa3',
  '#4d7f5f',
  '#9bb3ae',
  '#356b70',
  '#7d9f83',
  '#73a0a0',
  '#a9c0b4',
]

const chartOption = computed<EChartsCoreOption>(() => {
  const { mobile, lineHeight, minMargin } = chartLabelMetrics.value
  const geometry = chartGeometry.value
  const radius = chartRadius.value
  const edgeMargin = mobile ? 8 : 12
  // Native leader geometry: the ellipse formula re-anchors text at
  // ring + length + length2, so the same offset is the safe column for labels
  // the ellipse would otherwise overshoot.
  const labelLineLength = mobile ? 15 : 19
  const labelLineLength2 = mobile ? (isNarrow.value ? 4 : 6) : 8
  const distanceToLabelLine = mobile ? (isNarrow.value ? 4 : 5) : 7
  const labelGap = labelLineLength + labelLineLength2
  return {
    color: CATEGORY_COLORS,
    tooltip: {
      trigger: 'item',
      confine: true,
      backgroundColor: 'rgba(38, 34, 28, 0.94)',
      borderWidth: 0,
      textStyle: { color: '#fffdf8', fontSize: 12 },
      valueFormatter: (value: unknown) => formatMoney(Number(value)),
    },
    series: [
      {
        name: `${kindLabel.value}分类`,
        type: 'pie',
        radius,
        center: ['50%', '50%'],
        startAngle: chartLayout.value.startAngle,
        clockwise: true,
        avoidLabelOverlap: true,
        labelLayout: (params: {
          align?: string
          labelRect: { x: number; width: number }
        }) => {
          if (!geometry) return {}
          if (params.align !== 'left' && params.align !== 'right') return {}
          if (params.align === 'left') {
            // Right half of the pie: text grows rightwards from label.x.
            const column = geometry.cx + geometry.radius + labelGap
            if (params.labelRect.x > column) {
              return {
                x: column,
                width: Math.max(0, geometry.width - edgeMargin - column),
              }
            }
            return {}
          }
          // Left half: text ends at label.x (align right).
          const column = geometry.cx - geometry.radius - labelGap
          if (params.labelRect.x + params.labelRect.width < column) {
            return { x: column, width: Math.max(0, column - edgeMargin) }
          }
          return {}
        },
        itemStyle: {
          borderColor: '#fffdf8',
          borderWidth: mobile ? 2 : 3,
          borderRadius: 4,
        },
        label: {
          show: true,
          // alignTo only takes effect for position 'outer'.
          position: 'outer',
          formatter: (params: { dataIndex: number }) => {
            const row = chartRows.value[params.dataIndex]!
            return `${row.name}\n${percentageLabel(row.percentage)}`
          },
          // Labels stay attached to their sectors. There is deliberately no
          // global labelLayout moveOverlap: pie series do not refresh their
          // label lines, so a later vertical shift detaches text from its line
          // and can drag it over the ring. The pie's own avoidLabelOverlap
          // stacks labels with `minMargin` between them; the labelLayout above
          // only clamps runaway X positions and refits their width.
          alignTo: 'none',
          distanceToLabelLine,
          bleedMargin: edgeMargin,
          minMargin,
          overflow: 'truncate',
          ellipsis: '…',
          color: '#5f6f6a',
          fontSize: mobile ? 10 : 12,
          lineHeight,
        },
        labelLine: {
          show: true,
          length: labelLineLength,
          length2: labelLineLength2,
          smooth: false,
          lineStyle: { width: 1, opacity: 0.6 },
        },
        emphasis: { scale: true, scaleSize: 5 },
        data: chartRows.value.map((row) => ({
          name: row.name,
          value: toNumber(row.amount),
        })),
      },
    ],
  }
})

/**
 * The report builder already orders a category's transactions by date and id,
 * newest first; the shared list renders them in the given order.
 */
const detailTransactions = computed<Transaction[]>(
  () => activeCategory.value?.transactions ?? [],
)

function resetDrilldown(): void {
  selectedParentKey.value = null
  selectedChildKey.value = null
}

function backToMajorCategories(): void {
  categoryMode.value = 'major'
  resetDrilldown()
}

function goBack(): void {
  if (level.value === 'detail' && selectedChild.value) {
    selectedChildKey.value = null
    return
  }
  resetDrilldown()
}

function openMinorRow(row: ReportCategory): void {
  const transactionId = row.transactions[0]?.id
  if (transactionId === undefined) return

  for (const parent of rows.value) {
    if (
      !parent.transactions.some(
        (transaction) => transaction.id === transactionId,
      )
    ) {
      continue
    }
    selectedParentKey.value = parent.key
    const child = parent.children.find((item) =>
      item.transactions.some(
        (transaction) => transaction.id === transactionId,
      ),
    )
    selectedChildKey.value = child?.key ?? null
    return
  }
}

function openRow(row: ReportCategory): void {
  if (level.value === 'root') {
    if (effectiveMode.value === 'minor') {
      openMinorRow(row)
      return
    }
    selectedParentKey.value = row.key
    selectedChildKey.value = null
    return
  }
  selectedChildKey.value = row.key
}

function toggleCategoryMode(): void {
  if (!minorAvailable.value) return
  categoryMode.value =
    effectiveMode.value === 'minor' ? 'major' : 'minor'
}

function percentageLabel(value: number): string {
  return `${value.toFixed(2)}%`
}

function barWidth(row: ReportCategory): string {
  return `${Math.min(100, Math.max(0, row.percentage))}%`
}

async function load(quiet = false): Promise<void> {
  const sequence = ++loadSequence
  if (!quiet) {
    loading.value = true
    loaded.value = false
  }
  loadError.value = ''
  try {
    const result = await listTransactions({ month: month.value })
    if (sequence !== loadSequence) return
    transactions.value = result
    loaded.value = true
  } catch (error) {
    if (sequence !== loadSequence) return
    loadError.value = getErrorMessage(error)
    if (!loaded.value) transactions.value = []
  } finally {
    if (sequence === loadSequence) loading.value = false
  }
}

/**
 * Editing needs an account snapshot. A failed refresh keeps the last known
 * accounts visible but stays flagged, so editing is disabled instead of
 * silently saving against stale options.
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

function retryCategories(): void {
  void ensureCategories(true).catch(() => undefined)
}

onMounted(() => {
  void ensureCategories().catch(() => undefined)
  void load()
  void loadAccounts()
  const canvas = chartCanvasEl.value
  if (canvas && !chartWidth.value) chartWidth.value = canvas.clientWidth
})

watch(month, () => {
  resetDrilldown()
  void load()
})

watch(kind, () => {
  resetDrilldown()
})

watch(dataVersion, () => {
  void load(true)
  void loadAccounts()
})

// A background refresh may remove the selected category. Keep the deepest
// still-existing level instead of showing an empty drilldown.
watch(rows, () => {
  if (selectedParentKey.value === null) {
    selectedChildKey.value = null
    return
  }
  const parent = rows.value.find(
    (row) => row.key === selectedParentKey.value,
  )
  if (!parent) {
    resetDrilldown()
    return
  }
  if (selectedChildKey.value === null) return
  if (
    !parent.children.some((child) => child.key === selectedChildKey.value)
  ) {
    selectedChildKey.value = null
  }
})
</script>

<template>
  <div class="reports-page">
    <div class="page-heading">
      <div class="heading-actions">
        <div class="kind-toggle" role="group" aria-label="选择报表类型">
          <button
            v-for="option in KIND_OPTIONS"
            :key="option.value"
            type="button"
            class="kind-toggle__button"
            :class="{ 'is-active': kind === option.value }"
            :aria-pressed="kind === option.value"
            @click="kind = option.value"
          >
            <span>{{ option.label }}</span>
            <span class="kind-toggle__amount money-number">
              {{ loaded ? formatMoney(monthTotals[option.value]) : '—' }}
            </span>
          </button>
        </div>
        <el-date-picker
          v-model="month"
          type="month"
          value-format="YYYY-MM"
          format="YYYY年MM月"
          placeholder="选择月份"
          :clearable="false"
          class="month-picker"
          aria-label="选择报表月份"
        />
      </div>
    </div>

    <div v-if="loading" class="report-skeleton" aria-busy="true" aria-live="polite">
      <span class="sr-only">正在加载{{ monthLabel }}{{ kindLabel }}报表</span>
      <div class="surface-card skeleton-block skeleton-block--chart">
        <el-skeleton animated :rows="7" />
      </div>
      <div class="surface-card skeleton-block">
        <el-skeleton animated :rows="7" />
      </div>
    </div>

    <template v-else>
      <div
        v-if="loadError && loaded"
        class="surface-card inline-warning"
        role="status"
      >
        <span>刷新流水失败，当前仍显示上次加载的数据：{{ loadError }}</span>
        <el-button text :icon="RefreshRight" @click="load(true)">
          重试
        </el-button>
      </div>

      <div
        v-if="categoryReady && categoryState.error"
        class="surface-card inline-warning"
        role="status"
      >
        <span>分类设置刷新失败，正在使用上次加载的分类：{{ categoryState.error }}</span>
        <el-button text :icon="RefreshRight" @click="retryCategories">
          重试
        </el-button>
      </div>

      <div
        v-if="loadError && !loaded"
        class="surface-card report-error"
      >
        <EmptyState
          title="暂时无法加载报表"
          :description="loadError"
          :icon="WarningFilled"
        >
          <template #actions>
            <el-button :icon="RefreshRight" @click="load()">重新加载</el-button>
          </template>
        </EmptyState>
      </div>

      <div
        v-else-if="!categoryReady"
        class="surface-card report-gate"
      >
        <div
          v-if="
            categoryState.loading ||
            (!categoryState.error && !categoryState.loaded)
          "
          class="report-gate__loading"
          role="status"
          aria-live="polite"
        >
          <p class="report-gate__loading-text">正在加载家庭分类设置…</p>
          <el-skeleton animated :rows="3" />
        </div>
        <EmptyState
          v-else
          title="暂时无法读取家庭分类"
          :description="
            categoryState.error ??
            '报表需要家庭分类设置才能把流水归入正确的一级、二级分类。'
          "
          :icon="WarningFilled"
        >
          <template #actions>
            <el-button :icon="RefreshRight" @click="retryCategories">
              重新加载分类
            </el-button>
          </template>
        </EmptyState>
      </div>

      <div
        v-else-if="rows.length === 0"
        class="surface-card report-empty"
      >
        <EmptyState
          :title="`${monthLabel}暂无${kindLabel}记录`"
          :description="`所选月份还没有${kindLabel}流水。账户转账不计入报表；记录一笔${kindLabel}后，分类统计会显示在这里。`"
          :icon="PieChart"
        >
          <template #actions>
            <el-button :icon="RefreshRight" @click="load()">刷新数据</el-button>
          </template>
        </EmptyState>
      </div>

      <div v-else class="report-body">
        <div v-if="level === 'detail'" class="report-toolbar">
          <el-button
            text
            size="small"
            :icon="ArrowLeft"
            @click="goBack"
          >
            {{ backLabel }}
          </el-button>
        </div>

        <div v-if="level === 'detail'" class="surface-card report-detail">
          <header class="report-detail__head">
            <div>
              <p class="page-eyebrow">
                {{ kind === 'expense' ? '支出明细' : '收入明细' }}
              </p>
              <h3>{{ activeCategory?.name }}</h3>
              <p class="report-detail__subtitle">
                {{ monthLabel }} · {{ activeCategory?.count ?? 0 }} 笔 · 合计
                <span class="money-number">
                  {{ formatMoney(activeCategory?.amount ?? '0.00') }}
                </span>
              </p>
            </div>
          </header>

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
            class="report-detail__note"
            role="status"
          >
            还没有可用的资金账户，暂时无法编辑明细；删除记录仍可使用。
          </div>

          <TransactionList
            :transactions="detailTransactions"
            :accounts="accounts"
            :loading="loading"
            :editing-disabled="editingDisabled"
          >
            <template #empty>
              <EmptyState
                title="该分类暂无明细"
                description="数据刷新后这笔分类的流水可能已被移动或删除，请返回上级分类重新查看。"
                :icon="PieChart"
              >
                <template #actions>
                  <el-button :icon="RefreshRight" @click="load(true)">
                    刷新数据
                  </el-button>
                </template>
              </EmptyState>
            </template>
          </TransactionList>
        </div>

        <div v-else class="report-grid">
          <section class="surface-card report-chart" aria-label="分类占比环形图">
            <div ref="chartCanvasEl" class="report-chart__canvas">
              <BaseChart
                :option="chartOption"
                :height="chartHeight"
                :aria-label="chartAriaLabel"
              />
              <div class="report-center">
                <template v-if="level === 'children'">
                  <p class="report-center__eyebrow">二级分类</p>
                  <h3
                    class="report-center__title"
                    :title="selectedParent?.name"
                  >
                    {{ selectedParent?.name }}
                  </h3>
                  <button
                    type="button"
                    class="report-center__button"
                    @click="backToMajorCategories"
                  >
                    <el-icon :size="12" aria-hidden="true">
                      <ArrowLeft />
                    </el-icon>
                    <span>返回大类</span>
                  </button>
                </template>
                <template v-else>
                  <h3
                    class="report-center__title"
                    :title="`${kindLabel}${effectiveMode === 'minor' ? '小类' : '大类'}`"
                  >
                    {{ kindLabel }}{{ effectiveMode === 'minor' ? '小类' : '大类' }}
                  </h3>
                  <button
                    v-if="minorAvailable"
                    type="button"
                    class="report-center__button"
                    @click="toggleCategoryMode"
                  >
                    <el-icon :size="12" aria-hidden="true">
                      <Switch />
                    </el-icon>
                    <span>
                      {{ effectiveMode === 'minor' ? '切换大类' : '切换小类' }}
                    </span>
                  </button>
                </template>
              </div>
            </div>
          </section>

          <section class="surface-card report-list" aria-label="分类统计">
            <ul class="report-rows" role="list">
              <li v-for="row in levelRows" :key="row.key">
                <button
                  type="button"
                  class="report-row"
                  @click="openRow(row)"
                >
                  <span class="report-row__main">
                    <span class="report-row__name">{{ row.name }}</span>
                  </span>
                  <span class="report-row__amount money-number">
                    {{ formatMoney(row.amount) }}
                  </span>
                  <span class="report-row__meta">{{ row.count }} 笔</span>
                  <span class="report-row__progress">
                    <span class="report-row__track" aria-hidden="true">
                      <span
                        class="report-row__fill"
                        :style="{ width: barWidth(row) }"
                      ></span>
                    </span>
                    <span class="report-row__percent">
                      {{ percentageLabel(row.percentage) }}
                    </span>
                  </span>
                  <el-icon
                    class="report-row__arrow"
                    :size="14"
                    aria-hidden="true"
                  >
                    <ArrowRight />
                  </el-icon>
                </button>
              </li>
            </ul>

            <p class="report-list__hint">
              金额按分精确聚合，占比四舍五入到两位小数；转账不计入{{ kindLabel }}。
            </p>
          </section>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.reports-page {
  min-width: 0;
}

.month-picker {
  width: 152px;
}

.kind-toggle {
  display: inline-flex;
  gap: 4px;
  padding: 3px;
  background: var(--hb-bg);
  border: 1px solid var(--hb-line);
  border-radius: 999px;
}

.kind-toggle__button {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 3px;
  min-width: 120px;
  min-height: 54px;
  padding: 6px 16px;
  color: var(--hb-ink-soft);
  font: inherit;
  font-size: 12px;
  font-weight: 600;
  background: transparent;
  border: 0;
  border-radius: 999px;
  cursor: pointer;
  appearance: none;
  transition:
    color 0.16s ease,
    background-color 0.16s ease,
    box-shadow 0.16s ease;
}

.kind-toggle__button:hover {
  color: var(--hb-ink);
}

.kind-toggle__button.is-active {
  color: var(--hb-teal-dark);
  background: var(--hb-surface);
  box-shadow: var(--hb-shadow-sm);
}

.kind-toggle__amount {
  font-size: 14px;
  white-space: nowrap;
}

.report-skeleton {
  display: grid;
  grid-template-columns: minmax(0, 0.92fr) minmax(0, 1.08fr);
  gap: 14px;
}

.skeleton-block {
  min-height: 400px;
  padding: 24px;
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
  border-color: #ecd3c2;
}

.report-error,
.report-gate,
.report-empty {
  padding: 18px;
}

.report-gate__loading {
  padding: 10px 4px;
}

.report-gate__loading-text {
  margin-bottom: 14px;
  color: var(--hb-ink-soft);
  font-size: 13px;
}

.report-body {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.report-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 0 4px;
}

.report-grid {
  display: grid;
  grid-template-columns: minmax(0, 0.92fr) minmax(0, 1.08fr);
  align-items: start;
  gap: 14px;
}

.report-chart {
  min-width: 0;
  padding: 6px 8px 2px;
}

.report-chart__canvas {
  position: relative;
  min-width: 0;
}

.report-center {
  position: absolute;
  top: 50%;
  left: 50%;
  z-index: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  width: 180px;
  text-align: center;
  transform: translate(-50%, -50%);
  pointer-events: none;
}

.report-center__eyebrow {
  margin: 0;
  color: var(--hb-ink-faint);
  font-size: 10px;
  letter-spacing: 0.12em;
}

.report-center__title {
  max-width: 100%;
  margin: 0;
  overflow: hidden;
  color: var(--hb-ink);
  font-size: 22px;
  line-height: 1.25;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.report-center__button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  padding: 5px 11px;
  color: var(--hb-teal-dark);
  font: inherit;
  font-size: 11px;
  font-weight: 600;
  white-space: nowrap;
  background: var(--hb-surface);
  border: 1px solid var(--hb-line);
  border-radius: 999px;
  cursor: pointer;
  pointer-events: auto;
  appearance: none;
  transition:
    color 0.16s ease,
    background-color 0.16s ease,
    border-color 0.16s ease;
}

.report-center__button:hover {
  color: var(--hb-teal-dark);
  background: var(--hb-teal-faint);
  border-color: #cfe3de;
}

.report-center__button:focus-visible {
  outline: 2px solid var(--hb-teal);
  outline-offset: 2px;
}

.report-list {
  min-width: 0;
  padding: 20px 22px 16px;
}

.report-rows {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 0;
  margin: 0;
  list-style: none;
}

.report-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto 14px;
  grid-template-areas:
    'main amount arrow'
    'progress meta arrow';
  align-items: center;
  gap: 6px 12px;
  width: 100%;
  padding: 10px 8px;
  color: inherit;
  font: inherit;
  text-align: left;
  background: transparent;
  border: 0;
  border-radius: 10px;
  cursor: pointer;
  appearance: none;
  transition: background-color 0.16s ease;
}

.report-row:hover {
  background: var(--hb-teal-faint);
}

.report-row:focus-visible {
  outline: 2px solid var(--hb-teal);
  outline-offset: -2px;
}

.report-row__main {
  grid-area: main;
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 0;
}

.report-row__name {
  overflow: hidden;
  color: var(--hb-ink);
  font-size: 13px;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.report-row__meta {
  grid-area: meta;
  text-align: right;
  white-space: nowrap;
  color: var(--hb-ink-faint);
  font-size: 11px;
}

.report-row__progress {
  display: grid;
  grid-area: progress;
  grid-template-columns: minmax(0, 1fr) 58px;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.report-row__track {
  height: 6px;
  overflow: hidden;
  background: var(--hb-bg);
  border-radius: 999px;
}

.report-row__fill {
  display: block;
  height: 100%;
  background: var(--hb-teal);
  border-radius: 999px;
}

.report-row__amount {
  grid-area: amount;
  color: var(--hb-ink);
  font-size: 13px;
  font-weight: 600;
  text-align: right;
  white-space: nowrap;
}

.report-row__percent {
  color: var(--hb-ink-soft);
  font-size: 11px;
  text-align: right;
  white-space: nowrap;
}

.report-row__arrow {
  grid-area: arrow;
  color: var(--hb-ink-faint);
}

.report-list__hint {
  margin-top: 12px;
  padding-top: 12px;
  color: var(--hb-ink-faint);
  font-size: 11px;
  line-height: 1.6;
  border-top: 1px dashed var(--hb-line);
}

.report-detail {
  min-width: 0;
  padding: 20px 22px 18px;
}

.report-detail__head {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 16px;
}

.report-detail__head .page-eyebrow {
  margin-bottom: 4px;
  font-size: 10px;
}

.report-detail__head h3 {
  color: var(--hb-ink);
  font-size: 19px;
}

.report-detail__subtitle {
  margin-top: 5px;
  color: var(--hb-ink-soft);
  font-size: 13px;
}

.report-detail__note {
  padding: 12px 16px;
  margin-bottom: 12px;
  color: var(--hb-ink-soft);
  font-size: 12px;
  line-height: 1.6;
  background: var(--hb-bg);
  border: 1px solid var(--hb-line);
  border-radius: 10px;
}

@media (max-width: 1180px) {
  .report-skeleton,
  .report-grid {
    grid-template-columns: minmax(0, 1fr);
  }

  .report-chart {
    order: 1;
  }

  .report-list {
    order: 2;
  }
}

@media (max-width: 760px) {
  .report-chart {
    padding: 2px 2px 0;
  }

  .report-center {
    gap: 4px;
    width: 92px;
  }

  .report-center__eyebrow {
    font-size: 9px;
  }

  .report-center__title {
    font-size: 16px;
    line-height: 1.2;
  }

  .report-center__button {
    gap: 3px;
    padding: 3px 7px;
    font-size: 10px;
  }

  .report-list {
    padding: 18px 14px 14px;
  }

  .report-row {
    grid-template-columns: minmax(0, 1fr) auto 14px;
    gap: 6px 10px;
    padding: 11px 8px;
  }

  .report-detail {
    padding: 18px 14px 14px;
  }
}

@media (max-width: 640px) {
  .month-picker {
    width: 100%;
  }

  .kind-toggle {
    flex: 1;
  }

  .kind-toggle__button {
    flex: 1;
  }

  .report-detail__head h3 {
    font-size: 17px;
  }

  .skeleton-block {
    min-height: 320px;
    padding: 18px;
  }
}
</style>
