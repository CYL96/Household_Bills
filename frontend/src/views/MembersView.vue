<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Delete,
  Edit,
  Plus,
  RefreshRight,
  UserFilled,
  WarningFilled,
} from '@element-plus/icons-vue'
import EmptyState from '../components/EmptyState.vue'
import UserAvatar from '../components/UserAvatar.vue'
import UserDialog from '../components/UserDialog.vue'
import UserProfileDialog from '../components/UserProfileDialog.vue'
import { getErrorMessage } from '../api/client'
import type { User } from '../api/types'
import { deleteUser } from '../api/users'
import {
  addAuthMember,
  authState,
  familyMembers,
  refreshAuth,
  removeAuthMember,
} from '../stores/auth'
import { displayName } from '../utils/user'

const refreshing = ref(false)
const errorMessage = ref('')
const dialogVisible = ref(false)
const editingUserId = ref<number | null>(null)
const deletingId = ref<number | null>(null)

const household = computed(() => authState.household)
const isAdmin = computed(() => authState.user?.is_admin === true)
const actionDisabled = computed(
  () => refreshing.value || deletingId.value !== null,
)
const editingMember = computed(() => {
  if (editingUserId.value === null) return null
  return (
    authState.members.find((member) => member.id === editingUserId.value) ?? null
  )
})
const editDialogVisible = computed({
  get: () => editingMember.value !== null,
  set: (visible: boolean) => {
    if (!visible) editingUserId.value = null
  },
})

function canEdit(member: User): boolean {
  return isAdmin.value || member.id === authState.user?.id
}

function canDelete(member: User): boolean {
  return isAdmin.value && !member.is_admin
}

async function refreshMembers(): Promise<void> {
  if (refreshing.value || deletingId.value !== null) return
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

function handleUserCreated(user: User): void {
  addAuthMember(user)
}

function openEdit(member: User): void {
  if (actionDisabled.value || !canEdit(member)) return
  editingUserId.value = member.id
}

async function handleDelete(member: User): Promise<void> {
  if (actionDisabled.value || !canDelete(member)) return
  deletingId.value = member.id
  try {
    await ElMessageBox.confirm(
      `删除“${displayName(member)}”后，该成员的所有登录会话会立即失效。其名下账户、账户归属、余额和历史流水均会保留，账户归属可在账户编辑中调整。`,
      '删除成员',
      {
        confirmButtonText: '删除成员',
        cancelButtonText: '取消',
        type: 'warning',
        confirmButtonClass: 'el-button--danger',
      },
    )
  } catch {
    deletingId.value = null
    return
  }

  try {
    await deleteUser(member.id)
    removeAuthMember(member.id)
    ElMessage.success('成员已删除，历史流水已保留。')
  } catch (error) {
    ElMessage.error(getErrorMessage(error))
  } finally {
    deletingId.value = null
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
        <h2>家庭成员</h2>
      </div>
      <div class="heading-actions">
        <el-button
          :icon="RefreshRight"
          :loading="refreshing"
          :disabled="deletingId !== null"
          @click="refreshMembers"
        >
          刷新成员
        </el-button>
        <el-button
          v-if="isAdmin"
          type="primary"
          :icon="Plus"
          :disabled="refreshing || deletingId !== null"
          data-testid="user-create-button"
          @click="dialogVisible = true"
        >
          新建用户
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
            <span class="member-count" data-testid="member-count">
              <el-icon :size="13" aria-hidden="true"><UserFilled /></el-icon>
              {{ familyMembers.length }} 位成员
            </span>
          </header>

          <ul
            v-if="familyMembers.length > 0"
            class="member-list"
            data-testid="member-list"
          >
            <li
              v-for="member in familyMembers"
              :key="member.id"
              class="member-row"
              :data-user-id="member.id"
              data-testid="member-row"
            >
              <UserAvatar :user="member" :size="36" />
              <div class="member-copy">
                <strong>{{ displayName(member) }}</strong>
                <span
                  v-if="member.id === authState.user?.id"
                  class="member-self"
                >
                  当前账号
                </span>
                <span class="member-role is-member">普通成员</span>
              </div>
              <div class="member-actions">
                <el-button
                  v-if="canEdit(member)"
                  text
                  size="small"
                  :icon="Edit"
                  data-testid="member-edit-button"
                  :disabled="actionDisabled"
                  :aria-label="`编辑${displayName(member)}`"
                  @click="openEdit(member)"
                >
                  编辑
                </el-button>
                <el-button
                  v-if="canDelete(member)"
                  text
                  size="small"
                  type="danger"
                  :icon="Delete"
                  :loading="deletingId === member.id"
                  data-testid="member-delete-button"
                  :disabled="actionDisabled"
                  :aria-label="`删除${displayName(member)}`"
                  @click="handleDelete(member)"
                >
                  删除
                </el-button>
              </div>
            </li>
          </ul>

          <p v-else class="members-empty" data-testid="members-empty">
            还没有家庭成员。
            {{ isAdmin ? '点击“新建用户”添加第一位成员。' : '请联系管理员添加成员。' }}
          </p>

          <p class="members-panel__foot">
            管理员可新建、编辑或删除成员；成员可以修改自己的资料和密码。
          </p>
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

    <UserDialog
      v-if="isAdmin"
      v-model="dialogVisible"
      @saved="handleUserCreated"
    />

    <UserProfileDialog
      v-if="editingMember"
      v-model="editDialogVisible"
      :user="editingMember"
    />
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

.member-actions {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 2px;
}

.member-actions :deep(.el-button + .el-button) {
  margin-left: 0;
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


.member-role.is-member {
  color: var(--hb-ink-faint);
  background: #f3ede3;
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


.members-error {
  padding: 14px;
}

@media (max-width: 640px) {
  .members-panel {
    padding: 19px 18px;
  }

  .member-row {
    flex-wrap: wrap;
  }

  .member-copy {
    flex-wrap: wrap;
    gap: 5px;
  }

  .member-actions {
    width: 100%;
    justify-content: flex-end;
    padding-left: 47px;
  }
}
</style>
