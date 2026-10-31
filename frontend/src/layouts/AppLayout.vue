<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { RouterView, useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import {
  ArrowDown,
  DataAnalysis,
  Menu,
  Odometer,
  Setting,
  SwitchButton,
  Tickets,
} from '@element-plus/icons-vue'
import AppNav from '../components/AppNav.vue'
import { getErrorMessage } from '../api/client'
import UserAvatar from '../components/UserAvatar.vue'
import UserProfileDialog from '../components/UserProfileDialog.vue'
import { authState, logoutUser } from '../stores/auth'
import { displayName } from '../utils/user'

const route = useRoute()
const router = useRouter()

const navOpen = ref(false)
const profileOpen = ref(false)
const loggingOut = ref(false)

const pageTitle = computed(() =>
  typeof route.meta.title === 'string' ? route.meta.title : '家庭账本',
)

const overviewActive = computed(() => route.path === '/')
const transactionsActive = computed(
  () => route.path === '/transactions' || route.path.startsWith('/transactions/'),
)
const statisticsActive = computed(
  () => route.path === '/statistics' || route.path.startsWith('/statistics/'),
)

const householdName = computed(() => authState.household?.name ?? '我的家庭')

const userDisplayName = computed(() =>
  authState.user ? displayName(authState.user) : '',
)

watch(
  () => route.fullPath,
  () => {
    navOpen.value = false
  },
)

watch(
  () => authState.user,
  (user) => {
    if (!user) void router.replace({ name: 'login' })
  },
)

async function handleLogout(): Promise<void> {
  if (loggingOut.value) return
  loggingOut.value = true
  try {
    await logoutUser()
    ElMessage.success('已安全退出登录')
  } catch (error) {
    ElMessage.warning(`${getErrorMessage(error)} 本机登录状态已清除。`)
  } finally {
    loggingOut.value = false
    navOpen.value = false
  }
}

function openProfile(): void {
  navOpen.value = false
  profileOpen.value = true
}

function handleUserCommand(command: string | number | object): void {
  if (command === 'profile') openProfile()
  if (command === 'logout') void handleLogout()
}
</script>

<template>
  <div class="app-shell">
    <aside class="app-sidebar">
      <AppNav />
    </aside>

    <div class="app-workspace">
      <header class="topbar">
        <button
          type="button"
          class="menu-button"
          aria-label="打开导航菜单"
          @click="navOpen = true"
        >
          <el-icon :size="20"><Menu /></el-icon>
        </button>

        <div class="topbar__heading">
          <p class="topbar__eyebrow">{{ householdName }}</p>
          <h1>{{ pageTitle }}</h1>
        </div>

        <div class="topbar__user">
          <el-dropdown trigger="click" @command="handleUserCommand">
            <button
              type="button"
              class="user-chip"
              aria-label="账户菜单：个人设置与退出登录"
              data-testid="profile-settings-trigger"
            >
              <UserAvatar
                v-if="authState.user"
                :user="authState.user"
                :size="34"
              />
              <span class="user-chip__copy">
                <strong>{{ userDisplayName }}</strong>
                <small>{{ householdName }}</small>
              </span>
              <el-icon class="user-chip__arrow" :size="14">
                <ArrowDown />
              </el-icon>
            </button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item disabled>
                  家庭：{{ householdName }}
                </el-dropdown-item>
                <el-dropdown-item
                  command="profile"
                  :icon="Setting"
                  data-testid="profile-settings-menu-item"
                >
                  个人设置
                </el-dropdown-item>
                <el-dropdown-item command="logout" divided :icon="SwitchButton">
                  退出登录
                </el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </header>

      <main class="page-content">
        <RouterView />
      </main>
    </div>

    <nav class="mobile-tab-nav" aria-label="手机快捷导航">
      <router-link
        to="/"
        class="mobile-tab"
        :class="{ 'is-active': overviewActive }"
        :aria-current="overviewActive ? 'page' : undefined"
      >
        <el-icon :size="20"><Odometer /></el-icon>
        <span class="mobile-tab__label">总览</span>
      </router-link>
      <router-link
        to="/transactions"
        class="mobile-tab"
        :class="{ 'is-active': transactionsActive }"
        :aria-current="transactionsActive ? 'page' : undefined"
      >
        <el-icon :size="20"><Tickets /></el-icon>
        <span class="mobile-tab__label">明细</span>
      </router-link>
      <router-link
        to="/statistics/reports"
        class="mobile-tab"
        :class="{ 'is-active': statisticsActive }"
        :aria-current="statisticsActive ? 'page' : undefined"
      >
        <el-icon :size="20"><DataAnalysis /></el-icon>
        <span class="mobile-tab__label">统计</span>
      </router-link>
    </nav>

    <el-drawer
      v-model="navOpen"
      title="导航"
      direction="ltr"
      size="286px"
      class="app-drawer"
    >
      <div class="drawer-nav">
        <AppNav variant="drawer" @navigate="navOpen = false" />
      </div>
      <div class="drawer-account">
        <div class="drawer-account__identity">
          <UserAvatar
            v-if="authState.user"
            :user="authState.user"
            :size="34"
          />
          <div class="drawer-account__copy">
            <strong>{{ userDisplayName }}</strong>
            <small>{{ householdName }}</small>
          </div>
        </div>
        <div class="drawer-account__actions">
          <el-button
            text
            :icon="Setting"
            data-testid="profile-settings-button"
            @click="openProfile"
          >
            个人设置
          </el-button>
          <el-button
            text
            :icon="SwitchButton"
            :loading="loggingOut"
            @click="handleLogout"
          >
            退出登录
          </el-button>
        </div>
      </div>
    </el-drawer>

    <UserProfileDialog
      v-if="authState.user"
      v-model="profileOpen"
      :user="authState.user"
    />
  </div>
</template>

<style scoped>
.app-shell {
  /* Shared with main.css so the floating entry clears the bottom tab bar. */
  --hb-mobile-nav-height: 58px;

  min-height: 100vh;
}

.app-sidebar {
  position: fixed;
  z-index: 30;
  inset: 0 auto 0 0;
  display: flex;
  flex-direction: column;
  width: 252px;
  padding: 22px 18px;
  overflow-y: auto;
  background: rgba(255, 253, 248, 0.76);
  border-right: 1px solid rgba(216, 205, 188, 0.75);
  backdrop-filter: blur(18px);
}

.app-workspace {
  display: flex;
  flex-direction: column;
  min-height: 100vh;
  margin-left: 252px;
}

.topbar {
  position: sticky;
  top: 0;
  z-index: 20;
  display: flex;
  align-items: center;
  gap: 18px;
  height: 78px;
  padding: 0 32px;
  background: rgba(244, 240, 232, 0.82);
  border-bottom: 1px solid rgba(216, 205, 188, 0.62);
  backdrop-filter: blur(16px);
}

.menu-button {
  display: none;
  flex: 0 0 auto;
  width: 38px;
  height: 38px;
  padding: 0;
  place-items: center;
  color: var(--hb-ink);
  background: var(--hb-surface);
  border: 1px solid var(--hb-line);
  border-radius: 11px;
  cursor: pointer;
}

.menu-button:hover {
  border-color: var(--hb-line-strong);
  background: #fff;
}

.topbar__heading {
  flex: 1;
  min-width: 0;
}

.topbar__eyebrow {
  overflow: hidden;
  color: var(--hb-ink-faint);
  font-size: 11px;
  letter-spacing: 0.1em;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.topbar__heading h1 {
  margin-top: 1px;
  overflow: hidden;
  font-family: var(--hb-display);
  font-size: 21px;
  font-weight: 600;
  letter-spacing: 0.03em;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.topbar__user {
  flex: 0 0 auto;
}

.user-chip {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 5px 10px 5px 5px;
  color: var(--hb-ink);
  background: transparent;
  border: 1px solid transparent;
  border-radius: 999px;
  cursor: pointer;
  transition: background-color 0.18s ease, border-color 0.18s ease;
}

.user-chip:hover {
  background: rgba(255, 253, 248, 0.85);
  border-color: var(--hb-line);
}

.user-chip__copy {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  max-width: 170px;
  line-height: 1.25;
}

.user-chip__copy strong {
  max-width: 100%;
  overflow: hidden;
  font-size: 13px;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.user-chip__copy small {
  max-width: 100%;
  margin-top: 2px;
  overflow: hidden;
  color: var(--hb-ink-faint);
  font-size: 10px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.user-chip__arrow {
  color: var(--hb-ink-faint);
}

.page-content {
  flex: 1;
  width: 100%;
  max-width: 1280px;
  margin: 0 auto;
  padding: 30px 32px 56px;
}

.mobile-tab-nav {
  display: none;
}

.drawer-nav {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
}

.drawer-account {
  display: flex;
  flex-direction: column;
  gap: 10px;
  margin-top: 14px;
  padding: 16px 6px 0;
  border-top: 1px solid var(--hb-line);
}

.drawer-account__identity {
  display: flex;
  align-items: center;
  gap: 10px;
}

.drawer-account__actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 4px;
}

.drawer-account__copy {
  display: flex;
  flex-direction: column;
  min-width: 0;
  flex: 1;
  line-height: 1.3;
}

.drawer-account__copy strong {
  overflow: hidden;
  font-size: 13px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.drawer-account__copy small {
  margin-top: 2px;
  overflow: hidden;
  color: var(--hb-ink-faint);
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (max-width: 1180px) {
  .page-content {
    padding: 26px 24px 48px;
  }

  .topbar {
    padding: 0 24px;
  }
}

@media (max-width: 960px) {
  .app-sidebar {
    display: none;
  }

  .app-workspace {
    margin-left: 0;
  }

  .topbar {
    height: 68px;
    padding: 0 18px;
  }

  .menu-button {
    display: grid;
  }

  .page-content {
    padding: 22px 18px 44px;
  }
}

@media (max-width: 600px) {
  .topbar {
    gap: 12px;
    padding: 0 14px;
  }

  .topbar__eyebrow {
    font-size: 10px;
    letter-spacing: 0.06em;
  }

  .topbar__heading h1 {
    font-size: 18px;
  }

  .user-chip {
    padding: 4px;
  }

  .user-chip__copy,
  .user-chip__arrow {
    display: none;
  }

  .page-content {
    padding: 18px 14px 40px;
  }
}

/*
 * Phones get a fixed three-tab bar for 总览 / 明细 / 统计. Kept below the
 * Element Plus drawers and dialogs (z-index >= 2000) so overlays cover it.
 */
@media (max-width: 760px) {
  .mobile-tab-nav {
    position: fixed;
    right: 0;
    bottom: 0;
    left: 0;
    z-index: 30;
    display: flex;
    height: calc(
      var(--hb-mobile-nav-height) + env(safe-area-inset-bottom, 0px)
    );
    padding:
      4px env(safe-area-inset-right, 0px)
      env(safe-area-inset-bottom, 0px) env(safe-area-inset-left, 0px);
    background: rgba(255, 253, 248, 0.94);
    border-top: 1px solid rgba(216, 205, 188, 0.7);
    box-shadow: 0 -6px 24px rgba(59, 48, 33, 0.08);
    backdrop-filter: blur(18px);
  }

  .mobile-tab {
    display: flex;
    flex: 1;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 2px;
    min-width: 0;
    padding: 2px 4px;
    color: var(--hb-ink-faint);
    font-size: 11px;
    font-weight: 500;
    line-height: 1.2;
    -webkit-tap-highlight-color: transparent;
  }

  .mobile-tab .el-icon {
    display: grid;
    width: 46px;
    height: 26px;
    place-items: center;
    border-radius: 999px;
    transition: color 0.18s ease, background-color 0.18s ease;
  }

  .mobile-tab:hover {
    color: var(--hb-ink);
  }

  .mobile-tab.is-active {
    color: var(--hb-teal-dark);
    font-weight: 600;
  }

  .mobile-tab.is-active .el-icon {
    color: var(--hb-teal);
    background: var(--hb-teal-soft);
  }

  /*
   * Reserving the bar plus a small gap keeps the last page content and the
   * floating 记一笔 entry scrollable clear of the fixed navigation.
   */
  .page-content {
    padding-bottom: calc(
      var(--hb-mobile-nav-height) + 28px + env(safe-area-inset-bottom, 0px)
    );
  }
}
</style>
