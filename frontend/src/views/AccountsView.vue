<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  EditPen,
  Plus,
  RefreshRight,
  User,
  Wallet,
  WarningFilled,
} from '@element-plus/icons-vue'
import AccountDialog from '../components/AccountDialog.vue'
import EmptyState from '../components/EmptyState.vue'
import { listAccounts } from '../api/accounts'
import { getErrorMessage } from '../api/client'
import type { Account } from '../api/types'
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

let requestSequence = 0

async function load(): Promise<void> {
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
  editingAccount.value = null
  dialogVisible.value = true
}

function openEdit(account: Account): void {
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
  void load()
  if (route.query.new === '1') {
    openCreate()
    void consumeNewQuery()
  }
})

watch(dataVersion, () => {
  void load()
})
</script>

<template>
  <div class="accounts-page">
    <div class="page-heading">
      <div class="page-heading__copy">
        <p class="page-eyebrow">资金账户</p>
        <h2>家里的钱，分别放在哪里</h2>
        <p class="page-heading__subtitle">
          当前余额 = 期初余额 + 全部收入 − 全部支出 + 转入 − 转出，由收支与转账记录自动计算。
        </p>
      </div>
      <div class="heading-actions">
        <el-button type="primary" :icon="Plus" @click="openCreate">
          添加账户
        </el-button>
      </div>
    </div>

    <div v-if="loading" class="account-grid" aria-busy="true">
      <span class="sr-only">正在加载资金账户</span>
      <div v-for="item in 3" :key="item" class="surface-card account-skeleton">
        <el-skeleton animated>
          <template #template>
            <el-skeleton-item variant="text" style="width: 28%" />
            <el-skeleton-item
              variant="h3"
              style="width: 56%; margin-top: 20px"
            />
            <el-skeleton-item
              variant="h1"
              style="width: 70%; margin-top: 26px"
            />
            <el-skeleton-item
              variant="text"
              style="width: 44%; margin-top: 26px"
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

      <div class="account-grid">
        <article
          v-for="account in accounts"
          :key="account.id"
          class="surface-card account-card"
        >
          <div class="account-card__top">
            <div class="account-card__identity">
              <span class="account-card__icon" aria-hidden="true">
                <el-icon :size="19"><Wallet /></el-icon>
              </span>
              <span
                class="account-card__owner"
                :class="{ 'is-public': !account.owner }"
                :title="account.owner ? `归属成员：${account.owner_name}` : '公共账户，全家共用'"
              >
                <el-icon :size="12" aria-hidden="true"><User /></el-icon>
                <span class="account-card__owner-name">
                  {{ account.owner ? `归属：${account.owner_name}` : '公共账户' }}
                </span>
              </span>
            </div>
            <el-button
              text
              circle
              :icon="EditPen"
              title="编辑账户"
              :aria-label="`编辑账户 ${account.name}`"
              @click="openEdit(account)"
            />
          </div>

          <h3 class="account-card__name">{{ account.name }}</h3>

          <p class="account-card__label">当前余额</p>
          <p
            class="account-card__balance money-number"
            :class="{ 'is-negative': Number(account.balance) < 0 }"
          >
            {{ formatMoney(account.balance) }}
          </p>

          <div class="account-card__foot">
            <span>期初余额</span>
            <span class="money-number">{{ formatMoney(account.opening_balance) }}</span>
          </div>
        </article>

        <button type="button" class="account-add-tile" @click="openCreate">
          <span class="account-add-tile__icon" aria-hidden="true">
            <el-icon :size="20"><Plus /></el-icon>
          </span>
          <strong>添加资金账户</strong>
          <small>现金、银行卡或其他钱包</small>
        </button>
      </div>

      <p class="accounts-footnote">
        家庭成员的每一笔收支和账户转账都会同步到对应账户余额，期初余额可随时在编辑中修正。
      </p>
    </template>

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

.account-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 14px;
}

.accounts-summary {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 14px;
  padding: 0 2px;
}

.accounts-summary p {
  color: var(--hb-ink-faint);
  font-size: 12px;
}

.accounts-summary strong {
  margin: 0 3px;
  color: var(--hb-ink);
  font-size: 14px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.account-card {
  position: relative;
  display: flex;
  flex-direction: column;
  min-height: 210px;
  padding: 18px 19px 15px;
  overflow: hidden;
}

.account-card::before {
  content: "";
  position: absolute;
  top: 0;
  left: 22px;
  right: 22px;
  height: 2px;
  border-radius: 0 0 999px 999px;
  background: linear-gradient(90deg, rgba(15, 118, 110, 0.65), rgba(15, 118, 110, 0));
}

.account-card__top {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.account-card__identity {
  display: flex;
  align-items: center;
  min-width: 0;
  gap: 8px;
}

.account-card__owner {
  display: inline-flex;
  align-items: center;
  min-width: 0;
  gap: 4px;
  max-width: 170px;
  padding: 3px 9px;
  color: var(--hb-ink-soft);
  background: #faf6ef;
  border: 1px solid var(--hb-line);
  border-radius: 999px;
  font-size: 11px;
  line-height: 1.5;
  white-space: nowrap;
}

.account-card__owner.is-public {
  color: var(--hb-ink-faint);
  border-style: dashed;
}

.account-card__owner-name {
  overflow: hidden;
  text-overflow: ellipsis;
}

.account-card__icon {
  display: grid;
  width: 36px;
  height: 36px;
  place-items: center;
  color: var(--hb-teal);
  background: var(--hb-teal-faint);
  border: 1px solid #d8eae5;
  border-radius: 11px;
}

.account-card__name {
  margin-top: 14px;
  overflow-wrap: anywhere;
  font-size: 16px;
  font-weight: 600;
}

.account-card__label {
  margin-top: 15px;
  color: var(--hb-ink-faint);
  font-size: 10px;
  letter-spacing: 0.06em;
}

.account-card__balance {
  margin-top: 2px;
  color: var(--hb-ink);
  font-size: 25px;
  font-weight: 600;
  letter-spacing: -0.03em;
  line-height: 1.2;
  overflow-wrap: anywhere;
}

.account-card__balance.is-negative {
  color: var(--hb-danger);
}

.account-card__foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-top: auto;
  padding-top: 13px;
  color: var(--hb-ink-faint);
  border-top: 1px dashed var(--hb-line-strong);
  font-size: 11px;
}

.account-card__foot span:last-child {
  color: var(--hb-ink-soft);
  font-variant-numeric: tabular-nums;
}

.account-add-tile {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  justify-content: center;
  min-height: 210px;
  padding: 20px;
  color: var(--hb-ink-soft);
  background: rgba(255, 253, 248, 0.48);
  border: 1px dashed var(--hb-line-strong);
  border-radius: var(--hb-radius-lg);
  text-align: left;
  cursor: pointer;
  transition: border-color 0.18s ease, background-color 0.18s ease;
}

.account-add-tile:hover {
  background: rgba(255, 253, 248, 0.82);
  border-color: #9fc7c0;
}

.account-add-tile__icon {
  display: grid;
  width: 38px;
  height: 38px;
  margin-bottom: 13px;
  place-items: center;
  color: var(--hb-teal);
  background: var(--hb-teal-faint);
  border: 1px solid #d8eae5;
  border-radius: 12px;
}

.account-add-tile strong {
  color: var(--hb-ink);
  font-size: 14px;
  font-weight: 600;
}

.account-add-tile small {
  margin-top: 4px;
  color: var(--hb-ink-faint);
  font-size: 11px;
}

.accounts-footnote {
  margin-top: 16px;
  padding: 0 2px;
  color: var(--hb-ink-faint);
  font-size: 11px;
}

.account-skeleton {
  min-height: 210px;
  padding: 20px;
}

.accounts-error,
.accounts-empty {
  padding: 16px;
}

@media (max-width: 1100px) {
  .account-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 640px) {
  .account-grid {
    grid-template-columns: minmax(0, 1fr);
    gap: 10px;
  }

  .account-card {
    min-height: 190px;
    padding: 16px;
  }

  .account-add-tile {
    min-height: 130px;
    padding: 16px;
  }

  .account-add-tile__icon {
    margin-bottom: 9px;
  }

  .accounts-error,
  .accounts-empty {
    padding: 8px;
  }
}
</style>
