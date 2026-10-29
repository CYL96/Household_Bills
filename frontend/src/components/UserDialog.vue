<script setup lang="ts">
import { computed, nextTick, reactive, ref, watch } from 'vue'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { createUser } from '../api/users'
import { getErrorMessage } from '../api/client'
import type { CreateUserPayload, User } from '../api/types'

const props = defineProps<{
  modelValue: boolean
}>()

const emit = defineEmits<{
  (event: 'update:modelValue', value: boolean): void
  (event: 'saved', user: User): void
}>()

const formRef = ref<FormInstance>()
const saving = ref(false)
const errorMessage = ref('')

const form = reactive({
  username: '',
  password: '',
})

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
  password: [
    { required: true, message: '请设置密码', trigger: 'blur' },
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
}

function resetForm(): void {
  errorMessage.value = ''
  form.username = ''
  form.password = ''
  void nextTick(() => {
    formRef.value?.clearValidate()
  })
}

watch(
  () => props.modelValue,
  (open) => {
    if (open) resetForm()
  },
)

function closeDialog(): void {
  emit('update:modelValue', false)
}

async function submit(): Promise<void> {
  if (saving.value || !formRef.value) return
  errorMessage.value = ''

  let valid = false
  try {
    valid = await formRef.value.validate()
  } catch {
    valid = false
  }
  if (!valid) return

  saving.value = true
  try {
    const payload: CreateUserPayload = {
      username: form.username.trim(),
      password: form.password,
    }
    const createdUser = await createUser(payload)
    form.password = ''
    ElMessage.success('用户已创建')
    emit('saved', createdUser)
    closeDialog()
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
    title="新建用户"
    width="520px"
    class="ledger-dialog"
    :close-on-click-modal="false"
    :close-on-press-escape="!saving"
    :show-close="!saving"
    @closed="resetForm"
  >
    <p class="user-dialog__intro">
      为家庭成员创建登录账号。新账号会加入当前家庭，并共享账户与收支记录。
    </p>

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
      data-testid="user-create-form"
      @submit.prevent="submit"
    >
      <el-form-item label="用户名" prop="username">
        <el-input
          v-model="form.username"
          size="large"
          maxlength="150"
          placeholder="成员登录时使用的用户名"
          autocomplete="off"
          aria-label="用户名"
        />
      </el-form-item>

      <el-form-item label="初始密码" prop="password">
        <el-input
          v-model="form.password"
          type="password"
          size="large"
          placeholder="至少 10 个字符"
          autocomplete="new-password"
          show-password
          aria-label="初始密码"
        />
      </el-form-item>

      <p class="user-form__hint">
        用户名最多 150 个字符，只能包含字母、数字和 @ . + - _。密码至少 10 个字符，且不能超过 72 个字节；创建后请通过安全方式告知成员。
      </p>
    </el-form>

    <template #footer>
      <div class="dialog-footer">
        <el-button :disabled="saving" @click="closeDialog">取消</el-button>
        <el-button
          type="primary"
          :loading="saving"
          data-testid="user-create-submit"
          @click="submit"
        >
          创建用户
        </el-button>
      </div>
    </template>
  </el-dialog>
</template>

<style scoped>
.user-dialog__intro {
  margin-bottom: 16px;
  color: var(--hb-ink-soft);
  font-size: 12px;
  line-height: 1.7;
}

.user-form :deep(.el-form-item) {
  margin-bottom: 18px;
}

.user-form :deep(.el-form-item__label) {
  padding-bottom: 6px;
  font-size: 12px;
}

.user-form__hint {
  margin-top: -2px;
  padding: 10px 12px;
  color: var(--hb-ink-faint);
  background: #faf6ef;
  border: 1px solid var(--hb-line);
  border-radius: 10px;
  font-size: 11px;
  line-height: 1.7;
}
</style>
