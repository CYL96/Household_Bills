<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue'
import type { Component } from 'vue'
import { useRoute } from 'vue-router'
import {
  ArrowDown,
  Calendar,
  DataAnalysis,
  Odometer,
  PieChart,
  Tickets,
  UserFilled,
  Wallet,
} from '@element-plus/icons-vue'
import { authState, familyMembers } from '../stores/auth'

interface NavItem {
  to: string
  label: string
  icon: Component
  /** 总览与流水明细在手机底部导航中提供，手机抽屉不重复展示。 */
  mobilePrimary?: boolean
}

const baseNavItems: NavItem[] = [
  { to: '/', label: '总览', icon: Odometer, mobilePrimary: true },
  { to: '/transactions', label: '流水明细', icon: Tickets, mobilePrimary: true },
  { to: '/accounts', label: '资金账户', icon: Wallet },
  { to: '/members', label: '家庭成员', icon: UserFilled },
]

defineProps<{
  variant?: 'sidebar' | 'drawer'
}>()

const route = useRoute()
const emit = defineEmits<{
  (event: 'navigate'): void
}>()

function isActive(path: string): boolean {
  if (path === '/') return route.path === '/'
  return route.path === path || route.path.startsWith(`${path}/`)
}

const statisticsActive = computed(() => isActive('/statistics'))
const statisticsExpanded = ref(statisticsActive.value)
const statisticsSubmenuId = useId()

watch(statisticsActive, (active) => {
  if (active) statisticsExpanded.value = true
})

function toggleStatistics(): void {
  statisticsExpanded.value = !statisticsExpanded.value
}
</script>

<template>
  <div class="nav-panel" :class="{ 'nav-panel--drawer': variant === 'drawer' }">
    <router-link to="/" class="brand" aria-label="家庭账本总览" @click="emit('navigate')">
      <span class="brand__mark" aria-hidden="true">家</span>
      <span class="brand__copy">
        <strong>家庭账本</strong>
        <small>Household Ledger</small>
      </span>
    </router-link>

    <nav class="nav-list" aria-label="主导航">
      <router-link
        v-for="item in baseNavItems"
        :key="item.to"
        :to="item.to"
        class="nav-link"
        :class="{
          'is-active': isActive(item.to),
          'nav-link--mobile-primary': item.mobilePrimary,
        }"
        :aria-current="isActive(item.to) ? 'page' : undefined"
        @click="emit('navigate')"
      >
        <el-icon :size="18">
          <component :is="item.icon" />
        </el-icon>
        <span>{{ item.label }}</span>
      </router-link>

      <div class="nav-group">
        <button
          type="button"
          class="nav-link nav-group__toggle"
          :class="{ 'is-active': statisticsActive }"
          :aria-expanded="statisticsExpanded"
          :aria-controls="statisticsSubmenuId"
          @click="toggleStatistics"
        >
          <el-icon :size="18"><DataAnalysis /></el-icon>
          <span>统计</span>
          <el-icon class="nav-group__chevron" :size="14"><ArrowDown /></el-icon>
        </button>

        <div
          v-show="statisticsExpanded"
          :id="statisticsSubmenuId"
          class="nav-sublist"
        >
          <router-link
            to="/statistics/reports"
            class="nav-link"
            :class="{ 'is-active': isActive('/statistics/reports') }"
            :aria-current="isActive('/statistics/reports') ? 'page' : undefined"
            @click="emit('navigate')"
          >
            <el-icon :size="16"><PieChart /></el-icon>
            <span>报表</span>
          </router-link>

          <span
            class="nav-link nav-link--disabled"
            aria-disabled="true"
            title="日历暂未开放"
          >
            <el-icon :size="16"><Calendar /></el-icon>
            <span>日历</span>
            <span class="nav-badge">暂未开放</span>
          </span>
        </div>
      </div>
    </nav>

    <div class="nav-foot">
      <p class="nav-foot__label">当前家庭</p>
      <p class="nav-foot__name">{{ authState.household?.name ?? '家庭账本' }}</p>
      <p class="nav-foot__meta">
        {{ familyMembers.length }} 位成员 · 人民币记账
      </p>
    </div>
  </div>
</template>

<style scoped>
.nav-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}

.brand {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 4px 8px;
  color: var(--hb-ink);
  border-radius: 14px;
}

.brand:hover {
  color: var(--hb-ink);
}

.brand__mark {
  display: grid;
  flex: 0 0 auto;
  width: 42px;
  height: 42px;
  place-items: center;
  color: #fffdf8;
  background: linear-gradient(145deg, var(--hb-teal) 0%, #0a5b55 100%);
  border-radius: 13px;
  box-shadow: 0 8px 18px rgba(15, 118, 110, 0.22);
  font-family: var(--hb-display);
  font-size: 21px;
  font-weight: 600;
}

.brand__copy {
  display: flex;
  flex-direction: column;
  min-width: 0;
  line-height: 1.25;
}

.brand__copy strong {
  font-family: var(--hb-display);
  font-size: 18px;
  font-weight: 600;
  letter-spacing: 0.03em;
}

.brand__copy small {
  margin-top: 3px;
  color: var(--hb-ink-faint);
  font-size: 10px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.nav-list {
  display: flex;
  flex-direction: column;
  gap: 5px;
  margin-top: 30px;
}

.nav-link {
  position: relative;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 11px 13px;
  color: var(--hb-ink-soft);
  border-radius: 12px;
  font-size: 14px;
  font-weight: 500;
  transition: color 0.18s ease, background-color 0.18s ease;
}

.nav-link .el-icon {
  color: var(--hb-ink-faint);
  transition: color 0.18s ease;
}

.nav-link:hover {
  color: var(--hb-ink);
  background: rgba(15, 118, 110, 0.055);
}

.nav-link:hover .el-icon {
  color: var(--hb-teal);
}

.nav-link.is-active {
  color: var(--hb-teal-dark);
  background: var(--hb-teal-soft);
  font-weight: 600;
}

.nav-link.is-active .el-icon {
  color: var(--hb-teal);
}

.nav-link.is-active::after {
  content: "";
  position: absolute;
  right: 13px;
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: var(--hb-teal);
}

.nav-group {
  display: flex;
  flex-direction: column;
}

.nav-group__toggle {
  width: 100%;
  border: 0;
  background: transparent;
  font: inherit;
  text-align: left;
  cursor: pointer;
}

.nav-group__toggle.is-active::after {
  display: none;
}

.nav-group__chevron {
  margin-left: auto;
  color: var(--hb-ink-faint);
  transition: color 0.18s ease, transform 0.18s ease;
}

.nav-group__toggle:hover .nav-group__chevron {
  color: var(--hb-teal);
}

.nav-group__toggle[aria-expanded="true"] .nav-group__chevron {
  transform: rotate(180deg);
}

.nav-sublist {
  display: flex;
  flex-direction: column;
  gap: 3px;
  margin: 3px 0 2px 22px;
  padding-left: 10px;
  border-left: 1px solid var(--hb-line);
}

.nav-sublist .nav-link {
  padding: 9px 11px;
  font-size: 13px;
}

.nav-sublist .nav-link.is-active::after {
  right: 11px;
}

.nav-link--disabled,
.nav-link--disabled:hover {
  color: var(--hb-ink-faint);
  background: transparent;
  cursor: not-allowed;
}

.nav-link--disabled .el-icon,
.nav-link--disabled:hover .el-icon {
  color: var(--hb-ink-faint);
}

.nav-badge {
  margin-left: auto;
  padding: 1px 7px;
  color: var(--hb-ink-faint);
  border: 1px solid var(--hb-line);
  border-radius: 999px;
  background: #faf7f0;
  font-size: 10px;
  font-weight: 500;
  letter-spacing: 0.02em;
  white-space: nowrap;
}

.nav-foot {
  margin-top: auto;
  padding: 16px;
  border: 1px solid var(--hb-line);
  border-radius: 15px;
  background:
    radial-gradient(120px 80px at 100% 0%, rgba(15, 118, 110, 0.08), transparent 70%),
    #faf7f0;
}

.nav-foot__label {
  color: var(--hb-ink-faint);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.14em;
}

.nav-foot__name {
  margin-top: 5px;
  overflow: hidden;
  color: var(--hb-ink);
  font-size: 14px;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.nav-foot__meta {
  margin-top: 3px;
  color: var(--hb-ink-soft);
  font-size: 11px;
}

/*
 * On phones the bottom tab bar owns 总览 / 明细 / 统计, so the drawer stops
 * repeating them. Accounts, members and the account footer stay available.
 * Between 761px and 960px the drawer keeps the full navigation.
 */
@media (max-width: 760px) {
  .nav-panel--drawer .nav-link--mobile-primary,
  .nav-panel--drawer .nav-group {
    display: none;
  }
}
</style>
