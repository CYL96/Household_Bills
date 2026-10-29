<script setup lang="ts">
import { computed, nextTick, reactive, ref, watch } from 'vue'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { getErrorMessage } from '../api/client'
import { createAccount, updateAccount } from '../api/accounts'
import type { Account, AccountPayload } from '../api/types'
import { markDataChanged } from '../stores/data'
import { decimalString } from '../utils/format'

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
const errorMessage = ref('')

const form = reactive({
  name: '',
  opening_balance: 0 as number | undefined,
})

const isEditing = computed(() => Boolean(props.account))
const dialogTitle = computed(() => (isEditing.value ? '编辑资金账户' : '添加资金账户'))

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
  opening_balance: [
    {
      validator: (_rule, value, callback) => {
        if (value === undefined || value === null || value === '') {
          callback(new Error('请输入期初余额，可填写 0'))
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
    form.opening_balance = Number(props.account.opening_balance)
  } else {
    form.name = ''
    form.opening_balance = 0
  }
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
    const payload: AccountPayload = {
      name: form.name.trim(),
      opening_balance: decimalString(form.opening_balance),
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
</script>

<template>
  <el-dialog
    v-model="visible"
    :title="dialogTitle"
    width="500px"
    class="ledger-dialog"
    :close-on-click-modal="false"
    :close-on-press-escape="!saving"
    :show-close="!saving"
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

      <el-form-item label="期初余额（元）" prop="opening_balance">
        <el-input-number
          v-model="form.opening_balance"
          :precision="2"
          :step="100"
          :controls="false"
          placeholder="0.00"
          aria-label="期初余额（元）"
        />
      </el-form-item>

      <p class="account-hint">
        期初余额可以为负数，例如信用卡待还金额。当前余额由期初余额与全部收支自动计算，不能直接修改。
      </p>
    </el-form>

    <template #footer>
      <div class="dialog-footer">
        <el-button :disabled="saving" @click="closeDialog">取消</el-button>
        <el-button type="primary" :loading="saving" @click="submit">
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
