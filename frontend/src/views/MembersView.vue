<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  InfoFilled,
  RefreshRight,
  UserFilled,
  WarningFilled,
} from '@element-plus/icons-vue'
import EmptyState from '../components/EmptyState.vue'
import { getErrorMessage } from '../api/client'
import { authState, refreshAuth } from '../stores/auth'

const refreshing = ref(false)
const errorMessage = ref('')

const household = computed(() => authState.household)
const members = computed(() => authState.members)

async function refreshMembers(): Promise<void> {
  if (refreshing.value) return
  refreshing.value = true
  errorMessage.value = ''
  try {
    await refreshAuth()
  } catch (error) {
    errorMessage.value = getErrorMessage(error)
  } finally {
    refreshing.value = false
  }
}

onMounted(() => {
  void refreshMembers()
})
</script>

<template>
  <div class="members-page">
    <div class="page-heading">
      <div class="page-heading__copy">
        <p class="page-eyebrow">家庭成员</p>
        <h2>一起记录，一起看见</h2>
        <p class="page-heading__subtitle">
          每位成员独立登录，共享同一本家庭账本里的账户与收支。
        </p>
      </div>
      <div class="heading-actions">
        <el-button :icon="RefreshRight" :loading="refreshing" @click="refreshMembers">
          刷新成员
        </el-button>
      </div>
    </div>

    <div v-if="errorMessage" class="inline-error" role="alert">
      <el-icon :size="17" aria-hidden="true"><WarningFilled /></el-icon>
      <span>{{ errorMessage }}</span>
      <el-button text type="primary" :loading="refreshing" @click="refreshMembers">
        重试
      </el-button>
    </div>

    <div v-loading="refreshing" class="members-body">
      <template v-if="household">
        <section class="surface-card members-panel">
          <header class="members-panel__head">
            <div>
              <p class="page-eyebrow">家庭名单</p>
              <h3>{{ household.name }}</h3>
            </div>
            <span class="member-count">
              <el-icon :size="13" aria-hidden="true"><UserFilled /></el-icon>
              {{ members.length }} 位成员
            </span>
          </header>

          <ul v-if="members.length > 0" class="member-list" data-testid="member-list">
            <li
              v-for="(member, index) in members"
              :key="member.id"
              class="member-row"
              data-testid="member-row"
            >
              <span class="member-avatar" aria-hidden="true">
                {{ [...member.username][0]?.toUpperCase() }}
              </span>
              <div class="member-copy">
                <strong>{{ member.username }}</strong>
                <span
                  v-if="member.id === authState.user?.id"
                  class="member-self"
                >
                  当前账号
                </span>
                <span
                  class="member-role"
                  :class="member.is_admin ? 'is-admin' : 'is-member'"
                >
                  {{ member.is_admin ? '管理员' : '普通成员' }}
                </span>
              </div>
              <span class="member-index" aria-hidden="true">
                {{ String(index + 1).padStart(2, '0') }}
              </span>
            </li>
          </ul>

          <p v-else class="members-empty">
            还没有获取到成员列表，请点击刷新重试。
          </p>

          <p class="members-panel__foot">
            成员账号由管理员创建；新账号会立即出现在这里，并共享全部账户和收支记录。
          </p>
        </section>

        <section class="surface-card access-note">
          <div class="access-note__icon" aria-hidden="true">
            <el-icon :size="19"><InfoFilled /></el-icon>
          </div>
          <div class="access-note__copy">
            <h3>关于家庭共享</h3>
            <ul>
              <li>同一家庭的成员可以共同管理资金账户，并查看、添加、编辑和删除收支记录。</li>
              <li>每个人使用自己的用户名和密码登录，退出登录只影响当前设备。</li>
              <li>新成员账号由管理员创建，不需要自行注册。</li>
            </ul>
          </div>
        </section>
      </template>

      <div v-else class="surface-card members-error">
        <EmptyState
          title="暂时没有家庭信息"
          description="请刷新成员信息；如果问题持续，请退出后重新登录。"
          :icon="WarningFilled"
        >
          <template #actions>
            <el-button :icon="RefreshRight" @click="refreshMembers">
              重新加载
            </el-button>
          </template>
        </EmptyState>
      </div>
    </div>
  </div>
</template>

<style scoped>
.members-page {
  min-width: 0;
}

.members-body {
  min-height: 200px;
}

.inline-error {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 14px;
  padding: 11px 15px;
  color: #8a4136;
  background: var(--hb-danger-soft);
  border: 1px solid #f0d4cd;
  border-radius: 13px;
  font-size: 12px;
}

.inline-error > span {
  flex: 1;
  min-width: 0;
}

.members-panel {
  min-width: 0;
  padding: 23px 24px;
}

.members-panel__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 14px;
  padding-bottom: 15px;
  border-bottom: 1px solid var(--hb-line);
}

.members-panel__head h3 {
  margin-top: 2px;
  overflow-wrap: anywhere;
  font-size: 19px;
}

.member-count {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 4px;
  margin-top: 2px;
  padding: 3px 9px;
  color: var(--hb-teal-dark);
  background: var(--hb-teal-soft);
  border-radius: 999px;
  font-size: 10px;
  white-space: nowrap;
}

.member-list {
  margin: 0;
  padding: 4px 0 0;
  list-style: none;
}

.member-row {
  display: flex;
  align-items: center;
  gap: 11px;
  padding: 12px 0;
  border-bottom: 1px dashed var(--hb-line);
}

.member-row:last-child {
  border-bottom: 0;
}

.member-avatar {
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

.member-copy {
  display: flex;
  flex: 1;
  min-width: 0;
  align-items: center;
  gap: 8px;
}

.member-copy strong {
  overflow: hidden;
  font-size: 13px;
  font-weight: 500;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.member-self {
  flex: 0 0 auto;
  padding: 1px 7px;
  color: var(--hb-ink-faint);
  background: #f3ede3;
  border-radius: 999px;
  font-size: 9px;
}

.member-role {
  flex: 0 0 auto;
  padding: 1px 7px;
  border-radius: 999px;
  font-size: 9px;
  white-space: nowrap;
}

.member-role.is-admin {
  color: #85551f;
  background: #f8efdc;
  border: 1px solid #eadbbd;
}

.member-role.is-member {
  color: var(--hb-ink-faint);
  background: #f3ede3;
}

.member-index {
  color: #c7bdad;
  font-size: 10px;
  letter-spacing: 0.06em;
}

.members-panel__foot {
  margin-top: 10px;
  padding-top: 13px;
  color: var(--hb-ink-faint);
  border-top: 1px solid var(--hb-line);
  font-size: 10px;
  line-height: 1.65;
}

.members-empty {
  padding: 22px 0;
  color: var(--hb-ink-faint);
  font-size: 12px;
  text-align: center;
}

.access-note {
  display: flex;
  gap: 15px;
  margin-top: 14px;
  padding: 20px 23px;
  background:
    radial-gradient(500px 200px at 100% 100%, rgba(189, 106, 60, 0.06), transparent 70%),
    var(--hb-surface);
}

.access-note__icon {
  display: grid;
  flex: 0 0 auto;
  width: 38px;
  height: 38px;
  place-items: center;
  color: var(--hb-terracotta);
  background: var(--hb-terracotta-soft);
  border: 1px solid #efd7c6;
  border-radius: 11px;
}

.access-note__copy h3 {
  font-size: 15px;
}

.access-note__copy ul {
  margin: 8px 0 0;
  padding-left: 17px;
  color: var(--hb-ink-soft);
  font-size: 12px;
  line-height: 1.9;
}

.access-note__copy li::marker {
  color: var(--hb-line-strong);
}

.members-error {
  padding: 14px;
}

@media (max-width: 640px) {
  .members-panel {
    padding: 19px 18px;
  }

  .member-copy {
    flex-wrap: wrap;
    gap: 5px;
  }

  .access-note {
    padding: 17px;
    gap: 11px;
  }

  .access-note__icon {
    width: 34px;
    height: 34px;
  }

  .access-note__copy ul {
    padding-left: 15px;
  }
}
</style>
