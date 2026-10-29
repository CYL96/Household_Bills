<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  Plus,
  RefreshRight,
  UserFilled,
  WarningFilled,
} from '@element-plus/icons-vue'
import EmptyState from '../components/EmptyState.vue'
import UserDialog from '../components/UserDialog.vue'
import { listUsers } from '../api/users'
import { getErrorMessage } from '../api/client'
import type { User } from '../api/types'
import { addAuthMember, authState } from '../stores/auth'

const users = ref<User[]>([])
const loading = ref(true)
const errorMessage = ref('')
const dialogVisible = ref(false)

const householdName = computed(() => authState.household?.name ?? '家庭账本')
const adminCount = computed(() => users.value.filter((user) => user.is_admin).length)

let requestSequence = 0

async function load(): Promise<void> {
  const request = ++requestSequence
  loading.value = true
  errorMessage.value = ''
  try {
    const result = await listUsers()
    if (request !== requestSequence) return
    users.value = result
  } catch (error) {
    if (request !== requestSequence) return
    users.value = []
    errorMessage.value = getErrorMessage(error)
  } finally {
    if (request === requestSequence) loading.value = false
  }
}

function openCreate(): void {
  dialogVisible.value = true
}

function handleUserCreated(user: User): void {
  addAuthMember(user)
  void load()
}

onMounted(() => {
  void load()
})
</script>

<template>
  <div class="users-page" data-testid="users-page">
    <div class="page-heading">
      <div class="page-heading__copy">
        <p class="page-eyebrow">用户管理</p>
        <h2>家庭成员账号</h2>
        <p class="page-heading__subtitle">
          查看家庭账号，并为家人创建新的登录账号。只有管理员可以访问此页面。
        </p>
      </div>
      <div class="heading-actions">
        <el-button :icon="RefreshRight" :loading="loading" @click="load">
          刷新
        </el-button>
        <el-button
          type="primary"
          :icon="Plus"
          data-testid="user-create-button"
          @click="openCreate"
        >
          新建用户
        </el-button>
      </div>
    </div>

    <div v-if="loading" class="surface-card users-panel users-skeleton" aria-busy="true">
      <span class="sr-only">正在加载成员账号</span>
      <el-skeleton v-for="item in 4" :key="item" animated class="users-skeleton__row">
        <template #template>
          <div class="users-skeleton__line">
            <el-skeleton-item variant="circle" style="width: 36px; height: 36px" />
            <el-skeleton-item variant="text" style="width: 42%" />
            <el-skeleton-item variant="text" style="width: 18%" />
          </div>
        </template>
      </el-skeleton>
    </div>

    <div v-else-if="errorMessage" class="surface-card users-error">
      <EmptyState
        title="暂时无法加载用户"
        :description="errorMessage"
        :icon="WarningFilled"
      >
        <template #actions>
          <el-button :icon="RefreshRight" @click="load">重新加载</el-button>
        </template>
      </EmptyState>
    </div>

    <div v-else-if="users.length === 0" class="surface-card users-empty">
      <EmptyState
        title="还没有成员账号"
        description="为家庭成员创建登录账号，新成员会加入当前家庭。"
        :icon="UserFilled"
      >
        <template #actions>
          <el-button type="primary" :icon="Plus" @click="openCreate">
            新建用户
          </el-button>
        </template>
      </EmptyState>
    </div>

    <section v-else class="surface-card users-panel" data-testid="user-list">
      <header class="users-panel__head">
        <div>
          <p class="page-eyebrow">家庭账号</p>
          <h3>{{ householdName }}</h3>
        </div>
        <span class="users-summary">
          <strong>{{ users.length }}</strong> 个账号 ·
          <strong>{{ adminCount }}</strong> 位管理员
        </span>
      </header>

      <ul class="user-list">
        <li
          v-for="user in users"
          :key="user.id"
          class="user-row"
          data-testid="user-row"
          :data-username="user.username"
        >
          <span class="user-avatar" aria-hidden="true">
            {{ [...user.username][0]?.toUpperCase() }}
          </span>
          <div class="user-copy">
            <strong>{{ user.username }}</strong>
            <span v-if="user.id === authState.user?.id" class="user-self">
              当前账号
            </span>
          </div>
          <span
            class="role-badge"
            :class="user.is_admin ? 'is-admin' : 'is-member'"
            data-testid="user-role"
          >
            {{ user.is_admin ? '管理员' : '普通成员' }}
          </span>
          <span class="user-id" aria-hidden="true">#{{ user.id }}</span>
        </li>
      </ul>

      <p class="users-panel__foot">
        账号创建后立即生效。请通过安全方式将初始密码告知成员。
      </p>
    </section>

    <UserDialog v-model="dialogVisible" @saved="handleUserCreated" />
  </div>
</template>

<style scoped>
.users-page {
  min-width: 0;
}

.users-panel {
  min-width: 0;
  padding: 23px 24px;
}

.users-skeleton__row {
  margin-bottom: 18px;
}

.users-skeleton__row:last-child {
  margin-bottom: 0;
}

.users-skeleton__line {
  display: flex;
  align-items: center;
  gap: 14px;
}

.users-error,
.users-empty {
  padding: 12px;
}

.users-panel__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 14px;
  padding-bottom: 15px;
  border-bottom: 1px solid var(--hb-line);
}

.users-panel__head h3 {
  margin-top: 2px;
  overflow-wrap: anywhere;
  font-size: 19px;
}

.users-summary {
  flex: 0 0 auto;
  padding: 3px 9px;
  color: var(--hb-teal-dark);
  background: var(--hb-teal-soft);
  border-radius: 999px;
  font-size: 10px;
  white-space: nowrap;
}

.users-summary strong {
  font-weight: 600;
}

.user-list {
  margin: 0;
  padding: 4px 0 0;
  list-style: none;
}

.user-row {
  display: flex;
  align-items: center;
  gap: 11px;
  padding: 13px 0;
  border-bottom: 1px dashed var(--hb-line);
}

.user-row:last-child {
  border-bottom: 0;
}

.user-avatar {
  display: grid;
  flex: 0 0 auto;
  width: 36px;
  height: 36px;
  place-items: center;
  color: var(--hb-teal-dark);
  background: var(--hb-teal-faint);
  border: 1px solid #d4e8e2;
  border-radius: 50%;
  font-size: 14px;
  font-weight: 600;
}

.user-copy {
  display: flex;
  flex: 1;
  min-width: 0;
  align-items: center;
  gap: 8px;
}

.user-copy strong {
  overflow: hidden;
  font-size: 13px;
  font-weight: 500;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.user-self {
  flex: 0 0 auto;
  padding: 1px 7px;
  color: var(--hb-ink-faint);
  background: #f3ede3;
  border-radius: 999px;
  font-size: 9px;
}

.role-badge {
  flex: 0 0 auto;
  padding: 2px 9px;
  border-radius: 999px;
  font-size: 10px;
  font-weight: 500;
  white-space: nowrap;
}

.role-badge.is-admin {
  color: #85551f;
  background: #f8efdc;
  border: 1px solid #eadbbd;
}

.role-badge.is-member {
  color: var(--hb-ink-soft);
  background: #f3ede3;
  border: 1px solid transparent;
}

.user-id {
  flex: 0 0 auto;
  width: 42px;
  color: #c7bdad;
  font-size: 10px;
  letter-spacing: 0.04em;
  text-align: right;
}

.users-panel__foot {
  margin-top: 10px;
  padding-top: 13px;
  color: var(--hb-ink-faint);
  border-top: 1px solid var(--hb-line);
  font-size: 10px;
  line-height: 1.65;
}

@media (max-width: 640px) {
  .users-panel {
    padding: 19px 18px;
  }

  .users-panel__head {
    flex-wrap: wrap;
    gap: 8px;
  }

  .user-copy {
    flex-wrap: wrap;
    gap: 5px;
  }

  .user-id {
    display: none;
  }
}
</style>
