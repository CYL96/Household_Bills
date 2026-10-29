<script setup lang="ts">
import { computed, nextTick, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { WarningFilled } from '@element-plus/icons-vue'
import { getErrorMessage } from '../api/client'
import { createTransaction, updateTransaction } from '../api/transactions'
import type {
  Account,
  Transaction,
  TransactionKind,
  TransactionPayload,
} from '../api/types'
import { markDataChanged } from '../stores/data'
import { categorySuggestions } from '../utils/categories'
import { decimalString, todayISO } from '../utils/format'

const props = defineProps<{
  modelValue: boolean
  transaction?: Transaction | null
  accounts: Account[]
}>()

const emit = defineEmits<{
  (event: 'update:modelValue', value: boolean): void
  (event: 'saved'): void
}>()

const router = useRouter()

const formRef = ref<FormInstance>()
const saving = ref(false)
const errorMessage = ref('')

interface TransactionFormState {
  account: number | undefined
  kind: TransactionKind
  amount: number | undefined
  category: string
  date: string
  note: string
}

const form = reactive<TransactionFormState>({
  account: undefined,
  kind: 'expense',
  amount: undefined,
  category: '',
  date: todayISO(),
  note: '',
})

const isEditing = computed(() => Boolean(props.transaction))
const dialogTitle = computed(() => (isEditing.value ? '编辑收支' : '记一笔'))

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

const categoryOptions = computed(() => {
  const options = categorySuggestions(form.kind)
  const current = form.category.trim()
  if (current && !options.includes(current)) {
    return [form.category, ...options]
  }
  return options
})

const rules: FormRules = {
  account: [{ required: true, message: '请选择资金账户', trigger: 'change' }],
  kind: [{ required: true, message: '请选择收支类型', trigger: 'change' }],
  amount: [
    {
      validator: (_rule, value, callback) => {
        if (value === undefined || value === null || value === '') {
          callback(new Error('请输入金额'))
          return
        }
        const amount = Number(value)
        if (!Number.isFinite(amount) || amount <= 0) {
          callback(new Error('金额需大于 0'))
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
  category: [
    {
      validator: (_rule, value, callback) => {
        const category = String(value ?? '').trim()
        if (!category) {
          callback(new Error('请选择或输入分类'))
          return
        }
        if ([...category].length > 64) {
          callback(new Error('分类最多 64 个字符'))
          return
        }
        callback()
      },
      trigger: ['blur', 'change'],
    },
  ],
  date: [{ required: true, message: '请选择日期', trigger: 'change' }],
}

function resetForm(): void {
  errorMessage.value = ''
  form.category = ''
  form.note = ''
  form.amount = undefined
  form.date = todayISO()
  form.kind = 'expense'

  if (props.transaction) {
    form.account = props.transaction.account
    form.kind = props.transaction.kind
    form.amount = Number(props.transaction.amount)
    form.category = props.transaction.category
    form.date = props.transaction.date
    form.note = props.transaction.note ?? ''
  } else {
    form.account = props.accounts[0]?.id
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

watch(
  () => props.accounts,
  (accounts) => {
    if (props.modelValue && form.account === undefined && accounts.length > 0) {
      form.account = accounts[0]?.id
    }
  },
)

function closeDialog(): void {
  emit('update:modelValue', false)
}

function goCreateAccount(): void {
  closeDialog()
  void router.push({ name: 'accounts', query: { new: '1' } })
}

async function submit(): Promise<void> {
  if (saving.value) return
  errorMessage.value = ''

  if (props.accounts.length === 0) {
    errorMessage.value = '请先创建资金账户，再记录收支。'
    return
  }
  if (!formRef.value) return

  let valid = false
  try {
    valid = await formRef.value.validate()
  } catch {
    valid = false
  }
  if (!valid) return

  saving.value = true
  try {
    const payload: TransactionPayload = {
      account: Number(form.account),
      kind: form.kind,
      amount: decimalString(form.amount),
      category: form.category.trim(),
      date: form.date,
      note: form.note.trim(),
    }

    if (props.transaction) {
      await updateTransaction(props.transaction.id, payload)
    } else {
      await createTransaction(payload)
    }

    markDataChanged()
    ElMessage.success(isEditing.value ? '收支记录已更新' : '已记下这笔收支')
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
    width="560px"
    class="ledger-dialog"
    :close-on-click-modal="false"
    :close-on-press-escape="!saving"
    :show-close="!saving"
  >
    <div v-if="accounts.length === 0" class="dialog-notice" role="alert">
      <el-icon class="dialog-notice__icon" :size="18">
        <WarningFilled />
      </el-icon>
      <div class="dialog-notice__copy">
        <strong>还没有资金账户</strong>
        <p>请先创建资金账户，再记录这笔收支。</p>
      </div>
      <el-button text type="primary" @click="goCreateAccount">
        去创建账户
      </el-button>
    </div>

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
      :disabled="accounts.length === 0"
      class="transaction-form"
      @submit.prevent="submit"
    >
      <div class="form-grid">
        <el-form-item label="收支类型" prop="kind">
          <el-radio-group v-model="form.kind" aria-label="收支类型">
            <el-radio-button value="expense">支出</el-radio-button>
            <el-radio-button value="income">收入</el-radio-button>
          </el-radio-group>
        </el-form-item>

        <el-form-item label="资金账户" prop="account">
          <el-select
            v-model="form.account"
            placeholder="选择账户"
            aria-label="资金账户"
          >
            <el-option
              v-for="account in accounts"
              :key="account.id"
              :label="account.name"
              :value="account.id"
            />
          </el-select>
        </el-form-item>

        <el-form-item label="金额（元）" prop="amount">
          <el-input-number
            v-model="form.amount"
            :min="0.01"
            :precision="2"
            :step="1"
            :controls="false"
            placeholder="0.00"
            aria-label="金额（元）"
          />
        </el-form-item>

        <el-form-item label="日期" prop="date">
          <el-date-picker
            v-model="form.date"
            type="date"
            value-format="YYYY-MM-DD"
            format="YYYY年MM月DD日"
            placeholder="选择日期"
            :clearable="false"
            aria-label="收支日期"
          />
        </el-form-item>
      </div>

      <el-form-item label="分类" prop="category">
        <el-select
          v-model="form.category"
          filterable
          allow-create
          default-first-option
          placeholder="选择或输入分类"
          aria-label="收支分类"
        >
          <el-option
            v-for="option in categoryOptions"
            :key="option"
            :label="option"
            :value="option"
          />
        </el-select>
        <p class="field-hint">
          没有合适的分类？输入自定义分类后按回车即可使用。
        </p>
      </el-form-item>

      <el-form-item label="备注（选填）" prop="note">
        <el-input
          v-model="form.note"
          type="textarea"
          :rows="3"
          placeholder="补充用途或说明"
          aria-label="备注（选填）"
        />
      </el-form-item>
    </el-form>

    <template #footer>
      <div class="dialog-footer">
        <el-button :disabled="saving" @click="closeDialog">取消</el-button>
        <el-button
          type="primary"
          :loading="saving"
          :disabled="accounts.length === 0"
          @click="submit"
        >
          {{ isEditing ? '保存修改' : '保存记录' }}
        </el-button>
      </div>
    </template>
  </el-dialog>
</template>

<style scoped>
.form-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 0 14px;
}

.transaction-form :deep(.el-form-item) {
  margin-bottom: 18px;
}

.transaction-form :deep(.el-form-item__label) {
  padding-bottom: 6px;
  font-size: 12px;
}

.transaction-form :deep(.el-select),
.transaction-form :deep(.el-date-editor.el-input) {
  width: 100%;
}

.field-hint {
  width: 100%;
  margin: 6px 0 0;
  color: var(--hb-ink-faint);
  font-size: 11px;
  line-height: 1.6;
}

.transaction-form :deep(.el-radio-group) {
  display: flex;
  width: 100%;
}

.transaction-form :deep(.el-radio-button) {
  flex: 1;
}

.transaction-form :deep(.el-radio-button__inner) {
  width: 100%;
  padding: 10px 16px;
}

.dialog-notice {
  display: flex;
  align-items: center;
  gap: 11px;
  margin-bottom: 17px;
  padding: 12px 14px;
  background: #fdf5e9;
  border: 1px solid #f0dfc5;
  border-radius: 12px;
}

.dialog-notice__icon {
  flex: 0 0 auto;
  color: #a9772f;
}

.dialog-notice__copy {
  flex: 1;
  min-width: 0;
}

.dialog-notice__copy strong {
  display: block;
  color: #7d5a22;
  font-size: 12px;
  font-weight: 600;
}

.dialog-notice__copy p {
  margin-top: 1px;
  color: #8f7045;
  font-size: 11px;
  line-height: 1.55;
}

@media (max-width: 520px) {
  .form-grid {
    grid-template-columns: minmax(0, 1fr);
    gap: 0;
  }

  .dialog-notice {
    align-items: flex-start;
    flex-wrap: wrap;
  }

  .dialog-notice .el-button {
    margin-left: 29px;
    padding-left: 0;
  }
}
</style>
