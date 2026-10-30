<script setup lang="ts">
import { computed, nextTick, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { getErrorMessage } from '../api/client'
import type { UpdateUserPayload, User } from '../api/types'
import { patchUser, uploadUserAvatar } from '../api/users'
import {
  authState,
  clearAuthState,
  refreshAuth,
  updateAuthUser,
} from '../stores/auth'
import { displayName } from '../utils/user'
import UserAvatar from './UserAvatar.vue'

const props = defineProps<{
  modelValue: boolean
  user: User
}>()

const emit = defineEmits<{
  (event: 'update:modelValue', value: boolean): void
}>()

const router = useRouter()
const formRef = ref<FormInstance>()
const fileInput = ref<HTMLInputElement>()

const saving = ref(false)
const uploading = ref(false)
const errorMessage = ref('')
const avatarError = ref('')
const selectedAvatar = ref<File | null>(null)

const form = reactive({
  username: '',
  nickname: '',
  password: '',
  confirmPassword: '',
  currentPassword: '',
})

const isSelf = computed(() => authState.user?.id === props.user.id)
const usernameLocked = computed(() => props.user.is_admin)
const selfPasswordChange = computed(() => isSelf.value && form.password.length > 0)
const busy = computed(() => saving.value || uploading.value)
const dialogTitle = computed(() => (isSelf.value ? '个人设置' : '编辑成员'))

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const USERNAME_PATTERN = /^[\p{L}\p{Nd}@.+\-_]+$/u

const rules: FormRules = {
  username: [
    {
      required: true,
      whitespace: true,
      message: '请输入用户名',
      trigger: 'blur',
    },
    {
      validator: (_rule, value, callback) => {
        const username = String(value ?? '').trim()
        if (!username) {
          callback()
          return
        }
        if ([...username].length > 150) {
          callback(new Error('用户名最多 150 个字符'))
          return
        }
        if (username.includes('\u0000') || !USERNAME_PATTERN.test(username)) {
          callback(new Error('用户名只能包含字母、数字和 @ . + - _ 字符'))
          return
        }
        callback()
      },
      trigger: 'blur',
    },
  ],
  nickname: [
    {
      validator: (_rule, value, callback) => {
        const nickname = String(value ?? '').trim()
        if (!nickname) {
          callback()
          return
        }
        if (nickname.includes('\u0000')) {
          callback(new Error('显示昵称包含非法字符'))
          return
        }
        if ([...nickname].length > 50) {
          callback(new Error('显示昵称最多 50 个字符'))
          return
        }
        callback()
      },
      trigger: 'blur',
    },
  ],
  password: [
    {
      validator: (_rule, value, callback) => {
        const password = String(value ?? '')
        if (!password) {
          callback()
          return
        }
        if ([...password].length < 10) {
          callback(new Error('密码至少需要 10 个字符'))
          return
        }
        if (new TextEncoder().encode(password).length > 72) {
          callback(new Error('密码不能超过 72 个字节'))
          return
        }
        callback()
      },
      trigger: 'blur',
    },
  ],
  confirmPassword: [
    {
      validator: (_rule, value, callback) => {
        const confirmation = String(value ?? '')
        if (!form.password && !confirmation) {
          callback()
          return
        }
        if (!form.password) {
          callback(new Error('请先输入新密码'))
          return
        }
        if (!confirmation) {
          callback(new Error('请再次输入新密码'))
          return
        }
        if (confirmation !== form.password) {
          callback(new Error('两次输入的密码不一致'))
          return
        }
        callback()
      },
      trigger: 'blur',
    },
  ],
  currentPassword: [
    {
      validator: (_rule, value, callback) => {
        if (!selfPasswordChange.value) {
          callback()
          return
        }
        if (String(value ?? '').length === 0) {
          callback(new Error('请输入当前密码'))
          return
        }
        callback()
      },
      trigger: 'blur',
    },
  ],
}

function resetForm(): void {
  form.username = props.user.username
  form.nickname = props.user.nickname
  form.password = ''
  form.confirmPassword = ''
  form.currentPassword = ''
  selectedAvatar.value = null
  avatarError.value = ''
  errorMessage.value = ''
  if (fileInput.value) fileInput.value.value = ''
  void nextTick(() => {
    formRef.value?.clearValidate()
  })
}

watch(
  () => props.modelValue,
  (open) => {
    if (open) resetForm()
  },
  { immediate: true },
)

watch(
  () => form.password,
  (password) => {
    if (!password) form.confirmPassword = ''
  },
)

function closeDialog(): void {
  if (busy.value) return
  emit('update:modelValue', false)
}

function chooseFile(): void {
  if (busy.value) return
  fileInput.value?.click()
}

function handleFileChange(event: Event): void {
  avatarError.value = ''
  selectedAvatar.value = null
  const input = event.target as HTMLInputElement
  const file = input.files?.[0] ?? null
  if (!file) return

  const type = file.type.toLowerCase()
  const isPng = type === 'image/png' || /\.png$/i.test(file.name)
  const isJpeg =
    type === 'image/jpeg' || type === 'image/jpg' || /\.jpe?g$/i.test(file.name)
  if (!isPng && !isJpeg) {
    avatarError.value = '仅支持 PNG 或 JPEG 格式的图片。'
    input.value = ''
    return
  }
  if (file.size === 0) {
    avatarError.value = '图片文件为空，请重新选择。'
    input.value = ''
    return
  }
  if (file.size > 2 * 1024 * 1024) {
    avatarError.value = '图片不能超过 2 MiB，请压缩后重试。'
    input.value = ''
    return
  }

  selectedAvatar.value = file
}

async function uploadAvatar(): Promise<void> {
  const file = selectedAvatar.value
  if (!file || busy.value) return

  uploading.value = true
  avatarError.value = ''
  errorMessage.value = ''
  try {
    const updated = await uploadUserAvatar(props.user.id, file)
    updateAuthUser(updated)
    selectedAvatar.value = null
    if (fileInput.value) fileInput.value.value = ''
    ElMessage.success('头像已更新')
  } catch (error) {
    avatarError.value = getErrorMessage(error)
  } finally {
    uploading.value = false
  }
}

async function completeSelfPasswordChange(): Promise<void> {
  try {
    await refreshAuth()
  } catch {
    // The server already revoked the session; clear locally even if probing fails.
  }
  if (authState.user) clearAuthState()
  await router.replace({ name: 'login' })
}

async function submit(): Promise<void> {
  if (busy.value || !formRef.value) return
  errorMessage.value = ''

  let valid = false
  try {
    valid = await formRef.value.validate()
  } catch {
    valid = false
  }
  if (!valid) return

  const payload: UpdateUserPayload = {}
  const username = form.username.trim()
  if (!usernameLocked.value && username !== props.user.username) {
    payload.username = username
  }
  const nickname = form.nickname.trim()
  if (nickname !== (props.user.nickname ?? '')) {
    payload.nickname = nickname
  }
  if (form.password) {
    payload.password = form.password
    if (isSelf.value) payload.current_password = form.currentPassword
  }

  if (Object.keys(payload).length === 0) {
    ElMessage.info('资料没有变化，无需保存。')
    return
  }

  const changedOwnPassword = Boolean(payload.password) && isSelf.value
  saving.value = true
  try {
    const updated = await patchUser(props.user.id, payload)
    updateAuthUser(updated)
    form.password = ''
    form.confirmPassword = ''
    form.currentPassword = ''

    if (changedOwnPassword) {
      ElMessage.success('密码已更新，请使用新密码重新登录。')
      emit('update:modelValue', false)
      await completeSelfPasswordChange()
      return
    }

    ElMessage.success(
      payload.password
        ? '成员资料已更新，该成员需使用新密码重新登录。'
        : '成员资料已更新',
    )
    emit('update:modelValue', false)
  } catch (error) {
    errorMessage.value = getErrorMessage(error)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <el-dialog
    v-model="visible"
    :title="dialogTitle"
    width="560px"
    class="ledger-dialog"
    :close-on-click-modal="false"
    :close-on-press-escape="!busy"
    :show-close="!busy"
    @closed="resetForm"
  >
    <section class="profile-avatar" data-testid="profile-avatar-section">
      <UserAvatar :user="user" :size="64" />
      <div class="profile-avatar__body">
        <div class="profile-avatar__copy">
          <strong>{{ displayName(user) }}</strong>
          <span>支持 PNG / JPEG 图片，大小不超过 2 MiB；头像通过“上传头像”单独保存。</span>
        </div>
        <div class="profile-avatar__actions">
          <input
            ref="fileInput"
            class="profile-file-input"
            type="file"
            accept=".png,.jpg,.jpeg,image/png,image/jpeg"
            data-testid="profile-avatar-input"
            @change="handleFileChange"
          />
          <el-button
            size="small"
            :disabled="busy"
            data-testid="profile-avatar-choose"
            @click="chooseFile"
          >
            选择图片
          </el-button>
          <el-button
            size="small"
            type="primary"
            plain
            data-testid="profile-avatar-upload"
            :disabled="!selectedAvatar || busy"
            :loading="uploading"
            @click="uploadAvatar"
          >
            {{ uploading ? '上传中…' : '上传头像' }}
          </el-button>
          <span class="profile-avatar__file">
            {{ selectedAvatar ? selectedAvatar.name : '尚未选择新图片' }}
          </span>
        </div>
        <p v-if="avatarError" class="profile-field-error" role="alert">
          {{ avatarError }}
        </p>
      </div>
    </section>

    <el-alert
      v-if="errorMessage"
      class="form-alert"
      :title="errorMessage"
      type="error"
      :closable="false"
      show-icon
    />

    <el-form
      ref="formRef"
      :model="form"
      :rules="rules"
      label-position="top"
      class="user-form"
      data-testid="user-profile-form"
      @submit.prevent="submit"
    >
      <el-form-item label="用户名" prop="username">
        <el-input
          v-model="form.username"
          size="large"
          maxlength="150"
          :disabled="usernameLocked"
          autocomplete="off"
          aria-label="用户名"
          data-testid="profile-username-input"
        />
        <p v-if="usernameLocked" class="profile-field-hint">
          管理员登录名固定，不可修改。
        </p>
      </el-form-item>

      <el-form-item label="显示昵称" prop="nickname">
        <el-input
          v-model="form.nickname"
          size="large"
          placeholder="留空则显示用户名"
          autocomplete="off"
          aria-label="显示昵称"
          data-testid="profile-nickname-input"
        />
      </el-form-item>

      <el-divider content-position="left">修改密码</el-divider>

      <el-form-item label="新密码" prop="password">
        <el-input
          v-model="form.password"
          type="password"
          size="large"
          placeholder="留空则不修改密码"
          autocomplete="new-password"
          show-password
          aria-label="新密码"
          data-testid="profile-password-input"
        />
      </el-form-item>

      <el-form-item
        v-if="form.password"
        label="确认新密码"
        prop="confirmPassword"
      >
        <el-input
          v-model="form.confirmPassword"
          type="password"
          size="large"
          autocomplete="new-password"
          show-password
          aria-label="确认新密码"
          data-testid="profile-confirm-password-input"
        />
      </el-form-item>

      <el-form-item
        v-if="selfPasswordChange"
        label="当前密码"
        prop="currentPassword"
      >
        <el-input
          v-model="form.currentPassword"
          type="password"
          size="large"
          placeholder="修改自己的密码需要验证当前密码"
          autocomplete="current-password"
          show-password
          aria-label="当前密码"
          data-testid="profile-current-password-input"
        />
      </el-form-item>

      <p class="profile-hint">
        用户名最多 150 个字符，只能包含字母、数字和 @ . + - _；显示昵称最多 50 个字符。密码至少 10 个字符且不超过 72 个字节。修改自己的密码后需要重新登录；管理员修改其他成员密码后，该成员的登录状态会失效。
      </p>
    </el-form>

    <template #footer>
      <div class="dialog-footer">
        <el-button :disabled="busy" @click="closeDialog">取消</el-button>
        <el-button
          type="primary"
          :loading="saving"
          :disabled="uploading"
          data-testid="user-profile-submit"
          @click="submit"
        >
          保存信息
        </el-button>
      </div>
    </template>
  </el-dialog>
</template>

<style scoped>
.profile-avatar {
  display: flex;
  gap: 16px;
  margin-bottom: 18px;
  padding: 14px;
  background: #faf6ef;
  border: 1px solid var(--hb-line);
  border-radius: 12px;
}

.profile-avatar__body {
  flex: 1;
  min-width: 0;
}

.profile-avatar__copy {
  display: flex;
  flex-direction: column;
  gap: 3px;
  line-height: 1.5;
}

.profile-avatar__copy strong {
  font-size: 14px;
}

.profile-avatar__copy span {
  color: var(--hb-ink-faint);
  font-size: 11px;
}

.profile-avatar__actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin-top: 10px;
}

.profile-avatar__file {
  overflow: hidden;
  color: var(--hb-ink-faint);
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.profile-file-input {
  display: none;
}

.profile-field-error {
  margin-top: 8px;
  color: #8a4136;
  font-size: 11px;
  line-height: 1.6;
}

.profile-field-hint {
  margin-top: 4px;
  color: var(--hb-ink-faint);
  font-size: 11px;
  line-height: 1.5;
}

.user-form :deep(.el-form-item) {
  margin-bottom: 18px;
}

.user-form :deep(.el-form-item__label) {
  padding-bottom: 6px;
  font-size: 12px;
}

.user-form :deep(.el-divider) {
  margin: 6px 0 20px;
}

.user-form :deep(.el-divider__text) {
  color: var(--hb-ink-soft);
  font-size: 12px;
}

.profile-hint {
  padding: 10px 12px;
  color: var(--hb-ink-faint);
  background: #faf6ef;
  border: 1px solid var(--hb-line);
  border-radius: 10px;
  font-size: 11px;
  line-height: 1.7;
}

@media (max-width: 600px) {
  .profile-avatar {
    align-items: flex-start;
    padding: 12px;
    gap: 12px;
  }
}
</style>
