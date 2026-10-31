<script setup lang="ts">
import { computed, nextTick, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import { getErrorMessage } from '../api/client'
import { createAccount, deleteAccount, updateAccount } from '../api/accounts'
import type { Account, AccountPayload } from '../api/types'
import { authState, familyMembers } from '../stores/auth'
import { markDataChanged } from '../stores/data'
import { decimalString } from '../utils/format'
import { displayName } from '../utils/user'

const props = defineProps<{
  modelValue: boolean
  account?: Account | null
}>()

const emit = defineEmits<{
  (event: 'update:modelValue', value: boolean): void
  (event: 'saved'): void
}>()

const formRef = ref<FormInstance>()
const saving = ref(false)
const deleting = ref(false)
const errorMessage = ref('')

/** 下拉框不接受 null 作为选项值，用 0 代表“公共账户”，提交时还原为 null。 */
const PUBLIC_OWNER_VALUE = 0

const form = reactive({
  name: '',
  balance: 0 as number | undefined,
  owner: null as number | null,
})

const isEditing = computed(() => Boolean(props.account))
const dialogTitle = computed(() => (isEditing.value ? '编辑资金账户' : '添加资金账户'))
const busy = computed(() => saving.value || deleting.value)

const ownerSelection = computed<number>({
  get: () => form.owner ?? PUBLIC_OWNER_VALUE,
  set: (value) => {
    form.owner = value === PUBLIC_OWNER_VALUE ? null : value
  },
})

type OwnerOption = {
  value: number
  label: string
  disabled?: boolean
}

const ownerOptions = computed<OwnerOption[]>(() => {
  const selectableMembers = familyMembers.value
  const options: OwnerOption[] = [
    { value: PUBLIC_OWNER_VALUE, label: '公共账户' },
    ...selectableMembers.map((member) => ({
      value: member.id,
      label: displayName(member),
    })),
  ]
  const account = props.account
  const currentOwner = account?.owner
  if (
    !account ||
    currentOwner == null ||
    selectableMembers.some((member) => member.id === currentOwner)
  ) {
    return options
  }

  // 历史归属可能指向管理员或已删除成员：仅用于回显原名，禁止重新选中。
  const member = authState.members.find((item) => item.id === currentOwner)
  if (member?.is_admin) {
    options.push({
      value: currentOwner,
      label: `${displayName(member)}（管理员）`,
      disabled: true,
    })
  } else {
    options.push({
      value: currentOwner,
      label: `${account.owner_name || '已删除成员'}（已删除）`,
      disabled: true,
    })
  }
  return options
})

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const rules: FormRules = {
  name: [
    {
      validator: (_rule, value, callback) => {
        const name = String(value ?? '').trim()
        if (!name) {
          callback(new Error('请输入账户名称'))
          return
        }
        if (name.length > 100) {
          callback(new Error('账户名称最多 100 个字符'))
          return
        }
        callback()
      },
      trigger: ['blur', 'change'],
    },
  ],
  balance: [
    {
      validator: (_rule, value, callback) => {
        if (value === undefined || value === null || value === '') {
          callback(new Error('请输入当前余额，可填写 0'))
          return
        }
        const amount = Number(value)
        if (!Number.isFinite(amount)) {
          callback(new Error('请输入有效的金额'))
          return
        }
        if (Math.round(amount * 100) / 100 !== amount) {
          callback(new Error('金额最多保留两位小数'))
          return
        }
        callback()
      },
      trigger: ['blur', 'change'],
    },
  ],
}

function resetForm(): void {
  errorMessage.value = ''
  if (props.account) {
    form.name = props.account.name
    form.balance = Number(props.account.balance)
  } else {
    form.name = ''
    form.balance = 0
  }
  form.owner = props.account?.owner ?? null
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
  if (busy.value || !formRef.value) return
  errorMessage.value = ''

  let valid = false
  try {
    valid = await formRef.value.validate()
  } catch {
    valid = false
  }
  if (!valid || busy.value) return

  saving.value = true
  try {
    const payload: AccountPayload = {
      name: form.name.trim(),
      balance: decimalString(form.balance),
    }

    // 新建时显式发送归属（null 即公共账户）；编辑时仅在变更后发送，
    // 这样管理员或已删除成员的原有归属可以通过省略字段继续保留。
    const originalOwner = props.account?.owner ?? null
    if (!props.account || form.owner !== originalOwner) {
      payload.owner = form.owner
    }

    if (props.account) {
      await updateAccount(props.account.id, payload)
    } else {
      await createAccount(payload)
    }

    markDataChanged()
    ElMessage.success(isEditing.value ? '账户已更新' : '资金账户已添加')
    emit('saved')
    closeDialog()
  } catch (error) {
    errorMessage.value = getErrorMessage(error)
  } finally {
    saving.value = false
  }
}

async function handleDelete(): Promise<void> {
  const account = props.account
  if (!account || busy.value) return

  // 先锁定按钮状态，确认框打开期间不允许保存或重复触发删除。
  deleting.value = true
  try {
    await ElMessageBox.confirm(
      `删除账户“${account.name}”后无法恢复。只有没有任何收支或转账记录的账户才能删除；删除后，该账户当前余额将从家庭总余额中移除。`,
      '删除账户',
      {
        confirmButtonText: '删除账户',
        cancelButtonText: '取消',
        type: 'warning',
        confirmButtonClass: 'el-button--danger',
      },
    )
  } catch {
    deleting.value = false
    return
  }

  errorMessage.value = ''
  try {
    await deleteAccount(account.id)
    markDataChanged()
    ElMessage.success('账户已删除')
    emit('saved')
    // 确认期间可能已切换账户：只关闭仍然显示被删除账户的弹窗。
    if (props.account?.id === account.id) closeDialog()
  } catch (error) {
    errorMessage.value = getErrorMessage(error)
  } finally {
    deleting.value = false
  }
}
</script>

<template>
  <el-dialog
    v-model="visible"
    :title="dialogTitle"
    width="500px"
    class="ledger-dialog"
    :close-on-click-modal="false"
    :close-on-press-escape="!busy"
    :show-close="!busy"
  >
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
      class="account-form"
      @submit.prevent="submit"
    >
      <el-form-item label="账户名称" prop="name">
        <el-input
          v-model="form.name"
          size="large"
          maxlength="100"
          show-word-limit
          placeholder="例如：日常银行卡、现金钱包"
          aria-label="账户名称"
        />
      </el-form-item>

      <el-form-item label="当前余额（元）" prop="balance">
        <el-input-number
          v-model="form.balance"
          :precision="2"
          :step="100"
          :controls="false"
          placeholder="0.00"
          aria-label="当前余额（元）"
        />
      </el-form-item>

      <el-form-item label="账户归属" prop="owner">
        <el-select
          v-model="ownerSelection"
          class="owner-select"
          size="large"
          aria-label="账户归属"
        >
          <el-option
            v-for="option in ownerOptions"
            :key="option.value"
            :label="option.label"
            :value="option.value"
            :disabled="option.disabled"
          />
        </el-select>
      </el-form-item>

      <p class="account-hint">
        当前余额可以为负数，例如信用卡待还金额。保存后，今后的收入、支出和账户转账都会在此基础上自动更新余额。账户归属只用于标记这个钱包由谁使用，可选“公共账户”或普通家庭成员；选择“公共账户”时全家成员仍可共同使用。
      </p>
    </el-form>

    <template #footer>
      <div class="dialog-footer">
        <el-button
          v-if="isEditing"
          class="account-delete-button"
          type="danger"
          :loading="deleting"
          :disabled="saving"
          @click="handleDelete"
        >
          删除账户
        </el-button>
        <el-button :disabled="busy" @click="closeDialog">取消</el-button>
        <el-button type="primary" :loading="saving" :disabled="deleting" @click="submit">
          {{ isEditing ? '保存修改' : '添加账户' }}
        </el-button>
      </div>
    </template>
  </el-dialog>
</template>

<style scoped>
.account-form :deep(.el-form-item) {
  margin-bottom: 18px;
}

.account-form :deep(.el-form-item__label) {
  padding-bottom: 6px;
  font-size: 12px;
}

.owner-select {
  width: 100%;
}

.account-delete-button {
  margin-right: auto;
}

.account-hint {
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
