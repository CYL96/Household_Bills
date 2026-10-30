<script setup lang="ts">
import { computed, nextTick, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import {
  Back,
  Loading,
  Setting,
  Switch as SwitchIcon,
  WarningFilled,
} from '@element-plus/icons-vue'
import { getErrorMessage } from '../api/client'
import { createTransaction, updateTransaction } from '../api/transactions'
import type {
  Account,
  Transaction,
  TransactionKind,
  TransactionPayload,
} from '../api/types'
import { useMediaQuery } from '../composables/useMediaQuery'
import {
  readCategoryEntries,
  visibleCategorySuggestions,
  type CategoryKind,
} from '../stores/categories'
import { markDataChanged } from '../stores/data'
import { categoryIcon } from '../utils/categoryIcons'
import { decimalString, nowLocalDateTime } from '../utils/format'
import CategorySettingsDialog from './CategorySettingsDialog.vue'

const props = defineProps<{
  modelValue: boolean
  transaction?: Transaction | null
  accounts: Account[]
  initialKind?: TransactionKind
}>()

const emit = defineEmits<{
  (event: 'update:modelValue', value: boolean): void
  (event: 'saved'): void
}>()

const router = useRouter()
const isMobile = useMediaQuery('(max-width: 760px)')
const amountDockRef = ref<HTMLElement | null>(null)

const TRANSFER_CATEGORY = '账户转账'
const MAX_INTEGER_DIGITS = 10
const MAX_AMOUNT = 9999999999.99

const formRef = ref<FormInstance>()
const amountInputRef = ref<HTMLInputElement | null>(null)
const saving = ref(false)
const submitMode = ref<'save' | 'again' | null>(null)
const errorMessage = ref('')
const categorySettingsOpen = ref(false)
const focusAmountAfterSave = ref(false)

interface TransactionFormState {
  account: number | undefined
  transferAccount: number | undefined
  kind: TransactionKind
  amount: string
  category: string
  /** Local civil date-time as YYYY-MM-DDTHH:mm:ss (seconds included). */
  date: string
  note: string
}

const form = reactive<TransactionFormState>({
  account: undefined,
  transferAccount: undefined,
  kind: 'expense',
  amount: '',
  category: '',
  date: nowLocalDateTime(),
  note: '',
})

let lastKind: TransactionKind = 'expense'

const isEditing = computed(() => Boolean(props.transaction))
const dialogTitle = computed(() => {
  if (isEditing.value) return '编辑账目'
  if (form.kind === 'transfer') return '记一笔转账'
  return form.kind === 'income' ? '记一笔收入' : '记一笔支出'
})

const visible = computed({
  get: () => props.modelValue,
  set: (value: boolean) => emit('update:modelValue', value),
})

/**
 * Category stored on the transaction being edited, when it belongs to the
 * active kind. It may predate the configurable category list.
 */
const historicalCategory = computed(() => {
  if (!isEditing.value || !props.transaction) return ''
  if (form.kind !== props.transaction.kind) return ''
  return props.transaction.category.trim()
})

/**
 * Visible category names in configured order. While editing a historical
 * transaction whose category was never configured, keep it available so it
 * stays visible and manageable. Hidden categories are part of the stored
 * configuration, so they are never resurrected this way.
 */
const categoryDisplay = computed<string[]>(() => {
  if (form.kind === 'transfer') return []
  const options = visibleCategorySuggestions(form.kind)
  const historical = historicalCategory.value
  if (!historical || options.includes(historical)) return options
  const configured = readCategoryEntries(form.kind).some(
    (entry) => entry.name === historical,
  )
  return configured ? options : [...options, historical]
})

/** Ensure the historical category is manageable even after picking another. */
const categorySettingsCurrent = computed(() => {
  if (historicalCategory.value) return historicalCategory.value
  return form.category
})

const categorySettingsKind = computed<CategoryKind>(() =>
  form.kind === 'income' ? 'income' : 'expense',
)

const transferUnavailable = computed(
  () => form.kind === 'transfer' && props.accounts.length < 2,
)

const saveDisabled = computed(
  () => props.accounts.length === 0 || transferUnavailable.value,
)

const inputDisabled = computed(() => saving.value || props.accounts.length === 0)

const dateDisplayFormat = computed(() =>
  isMobile.value ? 'MM-DD HH:mm:ss' : 'YYYY-MM-DD HH:mm:ss',
)

const transferSummary = computed(() => {
  const source = props.accounts.find((account) => account.id === form.account)
  const target = props.accounts.find(
    (account) => account.id === form.transferAccount,
  )
  if (source && target) return `${source.name} → ${target.name}`
  return '请选择转出与转入账户'
})

function defaultCategory(kind: TransactionKind): string {
  if (kind === 'transfer') return TRANSFER_CATEGORY
  return visibleCategorySuggestions(kind)[0] ?? ''
}

const rules: FormRules = {
  account: [
    {
      validator: (_rule, value, callback) => {
        if (value === undefined || value === null || value === '') {
          callback(
            new Error(form.kind === 'transfer' ? '请选择转出账户' : '请选择资金账户'),
          )
          return
        }
        if (!props.accounts.some((account) => account.id === value)) {
          callback(new Error('请选择有效的资金账户'))
          return
        }
        callback()
      },
      trigger: ['blur', 'change'],
    },
  ],
  transferAccount: [
    {
      validator: (_rule, value, callback) => {
        if (form.kind !== 'transfer') {
          callback()
          return
        }
        if (value === undefined || value === null || value === '') {
          callback(new Error('请选择转入账户'))
          return
        }
        if (value === form.account) {
          callback(new Error('转入账户不能与转出账户相同'))
          return
        }
        if (!props.accounts.some((account) => account.id === value)) {
          callback(new Error('请选择有效的转入账户'))
          return
        }
        callback()
      },
      trigger: ['blur', 'change'],
    },
  ],
  kind: [{ required: true, message: '请选择交易类型', trigger: 'change' }],
  amount: [
    {
      validator: (_rule, value, callback) => {
        const text = String(value ?? '').trim()
        if (!text) {
          callback(new Error('请输入金额'))
          return
        }
        const normalized = text.replace(/\.$/, '')
        if (!/^\d+(\.\d{1,2})?$/.test(normalized)) {
          callback(new Error('金额最多保留两位小数'))
          return
        }
        const amount = Number(normalized)
        if (!Number.isFinite(amount) || amount <= 0) {
          callback(new Error('金额需大于 0'))
          return
        }
        if (amount > MAX_AMOUNT) {
          callback(new Error('金额超出可记录范围'))
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
        if (form.kind === 'transfer') {
          callback()
          return
        }
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
  date: [{ required: true, message: '请选择记账时间', trigger: 'change' }],
}

function sanitizeAmount(raw: string): string {
  const cleaned = raw.replace(/[^\d.]/g, '')
  if (!cleaned) return ''

  const dotIndex = cleaned.indexOf('.')
  if (dotIndex === -1) {
    return cleaned.replace(/^0+(?=\d)/, '').slice(0, MAX_INTEGER_DIGITS)
  }

  const whole = cleaned.slice(0, dotIndex).replace(/\D/g, '')
  const fraction = cleaned.slice(dotIndex + 1).replace(/\./g, '').slice(0, 2)
  const integer = whole.replace(/^0+(?=\d)/, '').slice(0, MAX_INTEGER_DIGITS) || '0'
  return `${integer}.${fraction}`
}

function amountFromRecord(value: string | null | undefined): string {
  if (value === null || value === undefined || value === '') return ''
  return sanitizeAmount(String(value))
}

function ensureTransferAccounts(): void {
  if (props.accounts.length === 0) return

  if (
    form.account === undefined ||
    !props.accounts.some((account) => account.id === form.account)
  ) {
    form.account = props.accounts[0]?.id
  }

  if (form.kind !== 'transfer') return

  const targetMissing =
    form.transferAccount === undefined ||
    form.transferAccount === form.account ||
    !props.accounts.some((account) => account.id === form.transferAccount)
  if (targetMissing) {
    form.transferAccount = props.accounts.find(
      (account) => account.id !== form.account,
    )?.id
  }
}

function resetForm(): void {
  errorMessage.value = ''
  categorySettingsOpen.value = false
  focusAmountAfterSave.value = false
  form.transferAccount = undefined
  form.note = ''
  form.amount = ''
  form.date = nowLocalDateTime()
  form.account = undefined
  form.kind = props.initialKind ?? 'expense'
  form.category = defaultCategory(form.kind)

  if (props.transaction) {
    form.account = props.transaction.account
    form.kind = props.transaction.kind
    form.amount = amountFromRecord(props.transaction.amount)
    form.category =
      props.transaction.kind === 'transfer'
        ? props.transaction.category || TRANSFER_CATEGORY
        : props.transaction.category || defaultCategory(props.transaction.kind)
    form.date = props.transaction.date
    form.note = props.transaction.note ?? ''
    form.transferAccount =
      props.transaction.kind === 'transfer'
        ? props.transaction.transfer_account ?? undefined
        : undefined
    if (form.kind === 'transfer' && form.transferAccount === undefined) {
      ensureTransferAccounts()
    }
  } else {
    form.account = props.accounts[0]?.id
    if (form.kind === 'transfer') {
      form.category = TRANSFER_CATEGORY
      ensureTransferAccounts()
    }
  }

  lastKind = form.kind
  void nextTick(() => {
    formRef.value?.clearValidate()
  })
}

function closeDialog(force = false): void {
  if (saving.value && !force) return
  categorySettingsOpen.value = false
  emit('update:modelValue', false)
}

function goCreateAccount(): void {
  closeDialog()
  void router.push({ name: 'accounts', query: { new: '1' } })
}

function isTransactionKind(value: unknown): value is TransactionKind {
  return value === 'expense' || value === 'income' || value === 'transfer'
}

function handleKindChange(value: unknown): void {
  if (!isTransactionKind(value)) return
  const next = value
  const previous = lastKind
  lastKind = next
  errorMessage.value = ''
  categorySettingsOpen.value = false

  if (next === 'transfer') {
    form.category = TRANSFER_CATEGORY
    ensureTransferAccounts()
  } else {
    form.transferAccount = undefined
    if (previous !== next) {
      form.category = defaultCategory(next)
    }
  }

  void nextTick(() => {
    formRef.value?.clearValidate(['account', 'transferAccount', 'category', 'kind'])
  })
}

function selectCategory(category: string): void {
  form.category = category
  errorMessage.value = ''
  formRef.value?.clearValidate('category')
}

function openCategorySettings(): void {
  if (saving.value || form.kind === 'transfer') return
  categorySettingsOpen.value = true
}

function handleCategorySaved(selectedCategory?: string): void {
  if (form.kind === 'transfer') return
  const options = visibleCategorySuggestions(form.kind)
  if (selectedCategory && options.includes(selectedCategory)) {
    form.category = selectedCategory
  } else if (!options.includes(form.category.trim())) {
    form.category = options[0] ?? ''
  }
  errorMessage.value = ''
  formRef.value?.clearValidate('category')
}

function handleAmountInput(event: Event): void {
  const input = event.target as HTMLInputElement
  const sanitized = sanitizeAmount(input.value)
  if (input.value !== sanitized) input.value = sanitized
  form.amount = sanitized
  if (sanitized) formRef.value?.clearValidate('amount')
}

function appendAmountKey(key: string): void {
  if (inputDisabled.value) return

  if (key === '.') {
    if (form.amount.includes('.')) return
    form.amount = form.amount ? `${form.amount}.` : '0.'
  } else {
    const dotIndex = form.amount.indexOf('.')
    const fraction = dotIndex === -1 ? undefined : form.amount.slice(dotIndex + 1)
    if (fraction !== undefined && fraction.length >= 2) return
    if (fraction === undefined && form.amount.length >= MAX_INTEGER_DIGITS) return
    form.amount = form.amount === '0' ? key : `${form.amount}${key}`
  }

  formRef.value?.clearValidate('amount')
}

function backspaceAmount(): void {
  if (inputDisabled.value || !form.amount) return
  form.amount = form.amount.slice(0, -1)
  formRef.value?.clearValidate('amount')
}

function handleEnterSubmit(event: Event): void {
  if (event instanceof KeyboardEvent && event.isComposing) return
  void submit('save')
}

function handleSourceAccountChange(): void {
  if (form.transferAccount !== undefined && form.account === form.transferAccount) {
    form.transferAccount = props.accounts.find(
      (account) => account.id !== form.account,
    )?.id
  }
  void nextTick(() => {
    formRef.value?.clearValidate(['account', 'transferAccount'])
  })
}

function swapTransferAccounts(): void {
  if (
    form.account === undefined ||
    form.transferAccount === undefined ||
    props.accounts.length < 2
  ) {
    return
  }
  const source = form.account
  form.account = form.transferAccount
  form.transferAccount = source
  void nextTick(() => {
    formRef.value?.clearValidate(['account', 'transferAccount'])
  })
}

function createdSuccessText(): string {
  if (form.kind === 'transfer') return '转账已记下'
  return form.kind === 'income' ? '收入已记下' : '支出已记下'
}

async function submit(mode: 'save' | 'again' = 'save'): Promise<void> {
  if (saving.value) return
  errorMessage.value = ''

  if (props.accounts.length === 0) {
    errorMessage.value = '请先创建资金账户，再记录账目。'
    return
  }
  if (form.kind === 'transfer' && props.accounts.length < 2) {
    errorMessage.value = '账户转账至少需要两个账户，请先新增另一个账户。'
    return
  }
  if (!formRef.value) return

  let valid = false
  try {
    valid = await formRef.value.validate()
  } catch {
    valid = false
  }
  if (!valid || saving.value) return

  if (form.kind === 'transfer' && form.account === form.transferAccount) {
    errorMessage.value = '转入账户不能与转出账户相同。'
    return
  }

  saving.value = true
  submitMode.value = mode
  try {
    const payload: TransactionPayload = {
      account: Number(form.account),
      kind: form.kind,
      amount: decimalString(Number(form.amount)),
      category:
        form.kind === 'transfer' ? TRANSFER_CATEGORY : form.category.trim(),
      date: form.date,
      note: form.note.trim(),
      transfer_account:
        form.kind === 'transfer' && form.transferAccount !== undefined
          ? Number(form.transferAccount)
          : null,
    }

    if (props.transaction) {
      await updateTransaction(props.transaction.id, payload)
    } else {
      await createTransaction(payload)
    }

    markDataChanged()
    emit('saved')

    if (props.transaction) {
      ElMessage.success('账目已更新')
      closeDialog(true)
    } else if (mode === 'again') {
      ElMessage.success(`${createdSuccessText()}，继续记一笔`)
      form.amount = ''
      form.note = ''
      focusAmountAfterSave.value = true
      await nextTick()
      formRef.value?.clearValidate()
    } else {
      ElMessage.success(createdSuccessText())
      closeDialog(true)
    }
  } catch (error) {
    errorMessage.value = getErrorMessage(error)
  } finally {
    saving.value = false
    submitMode.value = null
    if (focusAmountAfterSave.value) {
      focusAmountAfterSave.value = false
      await nextTick()
      amountInputRef.value?.focus()
    }
  }
}

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      resetForm()
    } else {
      categorySettingsOpen.value = false
    }
  },
  { immediate: true },
)

watch(
  () => props.accounts,
  (accounts) => {
    if (!props.modelValue || accounts.length === 0) return
    if (form.kind === 'transfer') {
      ensureTransferAccounts()
      return
    }
    if (
      form.account === undefined ||
      !accounts.some((account) => account.id === form.account)
    ) {
      form.account = accounts[0]?.id
    }
  },
)
</script>

<template>
  <el-dialog
    v-model="visible"
    :title="dialogTitle"
    width="560px"
    :fullscreen="isMobile"
    class="ledger-dialog quick-entry-dialog"
    :close-on-click-modal="false"
    :close-on-press-escape="!saving && !categorySettingsOpen"
    :show-close="false"
  >
    <template #header>
      <div class="entry-header">
        <button
          type="button"
          class="entry-header__back"
          aria-label="返回"
          title="返回"
          :disabled="saving"
          @click="closeDialog()"
        >
          <el-icon><Back /></el-icon>
        </button>

        <el-radio-group
          v-model="form.kind"
          class="kind-switch entry-kind-switch"
          aria-label="交易类型"
          :disabled="inputDisabled"
          @change="handleKindChange"
        >
          <el-radio-button value="expense">支出</el-radio-button>
          <el-radio-button value="income">收入</el-radio-button>
          <el-radio-button value="transfer">转账</el-radio-button>
        </el-radio-group>

        <button
          type="button"
          class="entry-header__manage"
          aria-label="分类管理"
          title="分类管理"
          :disabled="saving || form.kind === 'transfer'"
          @click="openCategorySettings"
        >
          <el-icon><Setting /></el-icon>
        </button>
      </div>
    </template>

    <div v-if="accounts.length === 0" class="dialog-notice" role="alert">
      <el-icon class="dialog-notice__icon" :size="18">
        <WarningFilled />
      </el-icon>
      <div class="dialog-notice__copy">
        <strong>还没有资金账户</strong>
        <p>请先创建资金账户，再记录账目。</p>
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
      :disabled="inputDisabled"
      class="transaction-form quick-form"
      :class="`quick-form--${form.kind}`"
      @submit.prevent
    >
      <div class="entry-main">
        <template v-if="form.kind !== 'transfer'">
          <el-form-item prop="category" class="category-item">
            <div class="category-grid" role="group" aria-label="收支分类">
              <button
                v-for="option in categoryDisplay"
                :key="option"
                type="button"
                class="category-tile"
                :class="{ 'is-selected': form.category === option }"
                :aria-pressed="form.category === option"
                :disabled="inputDisabled"
                @click="selectCategory(option)"
              >
                <el-icon>
                  <component :is="categoryIcon(option)" />
                </el-icon>
                <span>{{ option }}</span>
              </button>
              <button
                type="button"
                class="category-tile category-tile--manage"
                aria-label="分类管理"
                title="分类管理"
                :disabled="inputDisabled"
                @click="openCategorySettings"
              >
                <el-icon><Setting /></el-icon>
                <span>自定义</span>
              </button>
            </div>
          </el-form-item>
        </template>

        <template v-else>
          <div
            v-if="accounts.length === 1"
            class="dialog-notice dialog-notice--inline"
            role="alert"
          >
            <el-icon class="dialog-notice__icon" :size="18">
              <WarningFilled />
            </el-icon>
            <div class="dialog-notice__copy">
              <strong>转账至少需要两个账户</strong>
              <p>请先新增另一个资金账户，再记录这笔转账。</p>
            </div>
            <el-button text type="primary" @click="goCreateAccount">
              去新增账户
            </el-button>
          </div>

          <div class="transfer-panel">
            <div class="transfer-row">
              <el-form-item label="转出账户" prop="account" class="transfer-account">
                <el-select
                  v-model="form.account"
                  placeholder="选择转出账户"
                  aria-label="转出账户"
                  @change="handleSourceAccountChange"
                >
                  <el-option
                    v-for="account in accounts"
                    :key="account.id"
                    :label="account.name"
                    :value="account.id"
                    :disabled="account.id === form.transferAccount"
                  />
                </el-select>
              </el-form-item>

              <button
                type="button"
                class="swap-button"
                :disabled="inputDisabled || form.transferAccount === undefined || accounts.length < 2"
                title="交换转出与转入账户"
                aria-label="交换转出与转入账户"
                @click="swapTransferAccounts"
              >
                <el-icon><SwitchIcon /></el-icon>
              </button>

              <el-form-item
                label="转入账户"
                prop="transferAccount"
                class="transfer-account"
              >
                <el-select
                  v-model="form.transferAccount"
                  placeholder="选择转入账户"
                  aria-label="转入账户"
                  :disabled="inputDisabled || accounts.length < 2"
                >
                  <el-option
                    v-for="account in accounts"
                    :key="account.id"
                    :label="account.name"
                    :value="account.id"
                    :disabled="account.id === form.account"
                  />
                </el-select>
              </el-form-item>
            </div>

            <div class="fixed-category" aria-label="转账分类">
              <el-icon><SwitchIcon /></el-icon>
              <span>{{ TRANSFER_CATEGORY }}</span>
              <small>转账分类固定</small>
            </div>
          </div>
        </template>
      </div>

      <Teleport :to="amountDockRef ?? 'body'" :disabled="!isMobile || !amountDockRef">
        <div class="entry-dock">
          <div class="dock-row">
            <div class="dock-note">
              <el-input
                v-model="form.note"
                class="dock-note__input"
                placeholder="点击输入备注"
                aria-label="备注（选填）"
                :disabled="inputDisabled"
                @keydown.enter.prevent="handleEnterSubmit"
              />
            </div>

            <el-form-item label="金额（元）" prop="amount" class="amount-item dock-amount">
              <div class="amount-display">
                <span class="amount-display__currency">¥</span>
                <input
                  ref="amountInputRef"
                  class="amount-input"
                  :value="form.amount"
                  :disabled="inputDisabled"
                  inputmode="none"
                  autocomplete="off"
                  maxlength="13"
                  placeholder="0.00"
                  aria-label="金额（元）"
                  @input="handleAmountInput"
                  @keydown.enter.prevent="handleEnterSubmit"
                />
              </div>
            </el-form-item>
          </div>

          <div class="meta-bar">
            <el-form-item
              v-if="form.kind !== 'transfer'"
              label="账户"
              prop="account"
              class="meta-bar__item meta-bar__account"
            >
              <el-select
                v-model="form.account"
                placeholder="选择账户"
                aria-label="资金账户"
                :disabled="inputDisabled"
              >
                <el-option
                  v-for="account in accounts"
                  :key="account.id"
                  :label="account.name"
                  :value="account.id"
                />
              </el-select>
            </el-form-item>

            <div v-else class="meta-bar__transfer" aria-label="转账账户">
              <el-icon><SwitchIcon /></el-icon>
              <span>{{ transferSummary }}</span>
            </div>

            <el-form-item label="记账时间" prop="date" class="meta-bar__item meta-bar__date">
              <el-date-picker
                v-model="form.date"
                type="datetime"
                value-format="YYYY-MM-DDTHH:mm:ss"
                :format="dateDisplayFormat"
                placeholder="选择记账时间"
                :clearable="false"
                :disabled="inputDisabled"
                aria-label="记账时间"
              />
            </el-form-item>
          </div>

          <div
            class="amount-keypad"
            :class="{ 'amount-keypad--edit': isEditing }"
            role="group"
            aria-label="金额数字键盘"
          >
            <button
              v-for="digit in ['1', '2', '3', '4', '5', '6', '7', '8', '9']"
              :key="digit"
              type="button"
              class="keypad-key keypad-digit"
              :disabled="inputDisabled"
              @click="appendAmountKey(digit)"
            >
              {{ digit }}
            </button>

            <button
              type="button"
              class="keypad-key keypad-backspace"
              aria-label="退格"
              title="退格"
              :disabled="inputDisabled"
              @click="backspaceAmount"
            >
              <el-icon><Back /></el-icon>
            </button>

            <button
              v-if="!isEditing"
              type="button"
              class="keypad-key keypad-again keypad-save-again"
              aria-label="保存并再记"
              title="保存并再记"
              :disabled="saveDisabled || saving"
              @click="submit('again')"
            >
              <el-icon v-if="saving && submitMode === 'again'" class="is-loading">
                <Loading />
              </el-icon>
              <span v-else>再记</span>
            </button>

            <button
              type="button"
              class="keypad-key keypad-save"
              :aria-label="isEditing ? '保存修改' : '保存'"
              :disabled="saveDisabled || saving"
              @click="submit('save')"
            >
              <el-icon v-if="saving && submitMode === 'save'" class="is-loading">
                <Loading />
              </el-icon>
              <span v-else class="keypad-save__label">
                {{ isEditing ? '保存修改' : '保存' }}
              </span>
            </button>

            <button
              type="button"
              class="keypad-key keypad-zero"
              :disabled="inputDisabled"
              @click="appendAmountKey('0')"
            >
              0
            </button>
            <button
              type="button"
              class="keypad-key keypad-dot"
              aria-label="小数点"
              :disabled="inputDisabled"
              @click="appendAmountKey('.')"
            >
              .
            </button>
          </div>
        </div>
      </Teleport>
    </el-form>

    <template #footer>
      <div
        ref="amountDockRef"
        class="amount-dock quick-form"
        :class="`quick-form--${form.kind}`"
      />
    </template>

    <CategorySettingsDialog
      v-if="form.kind !== 'transfer'"
      v-model="categorySettingsOpen"
      :kind="categorySettingsKind"
      :current-category="categorySettingsCurrent"
      @saved="handleCategorySaved"
    />
  </el-dialog>
</template>

<style scoped>
.transaction-form {
  min-width: 0;
}

.quick-form {
  --kind-accent: var(--hb-terracotta);
  --kind-accent-soft: var(--hb-terracotta-soft);
}

.quick-form--income {
  --kind-accent: var(--hb-teal);
  --kind-accent-soft: var(--hb-teal-soft);
}

.quick-form--transfer {
  --kind-accent: var(--hb-gold);
  --kind-accent-soft: #f7efdf;
}

.quick-form :deep(.el-form-item) {
  margin-bottom: 10px;
}

.quick-form :deep(.el-form-item__label) {
  padding-bottom: 4px;
  font-size: 12px;
  line-height: 1.4;
}

.quick-form :deep(.el-form-item__content) {
  min-width: 0;
}

.quick-form :deep(.el-form-item__error) {
  padding-top: 3px;
  font-size: 10px;
  line-height: 1.35;
}

/* Header: back / kind segment / category manager */
.entry-header {
  display: grid;
  grid-template-columns: 40px minmax(0, 1fr) 40px;
  gap: 10px;
  align-items: center;
  width: 100%;
}

.entry-header__back,
.entry-header__manage {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 38px;
  height: 38px;
  padding: 0;
  color: var(--hb-ink-soft);
  font-size: 18px;
  cursor: pointer;
  background: var(--hb-bg);
  border: 1px solid var(--hb-line);
  border-radius: 50%;
  appearance: none;
  -webkit-tap-highlight-color: transparent;
  transition:
    color 0.16s ease,
    background-color 0.16s ease,
    border-color 0.16s ease;
}

.entry-header__back:hover:not(:disabled),
.entry-header__manage:hover:not(:disabled) {
  color: var(--hb-ink);
  background: var(--hb-surface-raised);
  border-color: var(--hb-line-strong);
}

.entry-header__back:disabled,
.entry-header__manage:disabled {
  cursor: not-allowed;
  opacity: 0.38;
}

.entry-header__manage {
  justify-self: end;
}

.kind-switch {
  display: flex;
  width: 100%;
  max-width: 320px;
  gap: 3px;
  padding: 3px;
  margin: 0 auto;
  background: var(--hb-bg);
  border: 1px solid var(--hb-line);
  border-radius: 12px;
}

.kind-switch :deep(.el-radio-button) {
  flex: 1;
  min-width: 0;
}

.kind-switch :deep(.el-radio-button__inner) {
  display: block;
  width: 100%;
  padding: 8px 4px;
  color: var(--hb-ink-soft);
  font-size: 13px;
  font-weight: 500;
  background: transparent;
  border: 0;
  border-radius: 9px;
  box-shadow: none;
}

.kind-switch :deep(.el-radio-button__original-radio:checked + .el-radio-button__inner) {
  color: var(--kind-accent);
  font-weight: 600;
  background: var(--kind-accent-soft);
  box-shadow: inset 0 0 0 1px var(--kind-accent);
}

/* Middle: category grid or transfer accounts */
.entry-main {
  min-height: 180px;
}

.category-item {
  margin-bottom: 8px;
}

.category-item :deep(.el-form-item__content) {
  display: block;
}

.category-grid {
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));
  gap: 8px;
  width: 100%;
}

.category-tile {
  display: flex;
  flex-direction: column;
  gap: 5px;
  align-items: center;
  justify-content: center;
  min-width: 0;
  min-height: 58px;
  padding: 7px 3px;
  color: var(--hb-ink-soft);
  font: inherit;
  font-size: 12px;
  line-height: 1.2;
  cursor: pointer;
  background: var(--hb-bg);
  border: 1px solid transparent;
  border-radius: 12px;
  appearance: none;
  -webkit-tap-highlight-color: transparent;
  transition:
    color 0.16s ease,
    background-color 0.16s ease,
    border-color 0.16s ease;
}

.category-tile .el-icon {
  font-size: 20px;
}

.category-tile span {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.category-tile:hover:not(:disabled) {
  color: var(--hb-ink);
  background: var(--hb-surface-raised);
  border-color: var(--hb-line-strong);
}

.category-tile:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.category-tile.is-selected {
  color: var(--kind-accent);
  font-weight: 600;
  background: var(--kind-accent-soft);
  border-color: var(--kind-accent);
}

.category-tile--manage {
  color: var(--hb-ink-faint);
  background: transparent;
  border: 1px dashed var(--hb-line-strong);
}

.transfer-panel {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.transfer-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 38px minmax(0, 1fr);
  gap: 8px;
  align-items: start;
}

.transfer-account {
  min-width: 0;
  margin-bottom: 0;
}

.transfer-account :deep(.el-select) {
  width: 100%;
}

.transfer-account :deep(.el-form-item__error) {
  white-space: normal;
}

.swap-button {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 38px;
  height: 32px;
  margin-top: 20px;
  color: var(--hb-ink-soft);
  font-size: 16px;
  cursor: pointer;
  background: var(--hb-bg);
  border: 1px solid var(--hb-line-strong);
  border-radius: 10px;
  appearance: none;
  -webkit-tap-highlight-color: transparent;
}

.swap-button:hover:not(:disabled) {
  color: var(--kind-accent);
  background: var(--kind-accent-soft);
  border-color: var(--kind-accent);
}

.swap-button:disabled {
  cursor: not-allowed;
  opacity: 0.45;
}

.fixed-category {
  display: flex;
  gap: 6px;
  align-items: center;
  width: 100%;
  height: 36px;
  padding: 0 12px;
  color: var(--hb-ink-soft);
  font-size: 13px;
  background: var(--hb-bg);
  border: 1px solid var(--hb-line);
  border-radius: 10px;
}

.fixed-category small {
  margin-left: auto;
  color: var(--hb-ink-faint);
  font-size: 11px;
}

/* Bottom dock: note + amount / meta / keypad */
.entry-dock {
  display: flex;
  flex-direction: column;
  gap: 6px;
  width: 100%;
}

.dock-row {
  display: flex;
  gap: 10px;
  align-items: flex-end;
}

.dock-note {
  flex: 1;
  min-width: 0;
}

.dock-note :deep(.el-input__wrapper) {
  padding: 0 2px;
  background: transparent;
  border-radius: 0;
  box-shadow: none;
}

.dock-note :deep(.el-input__wrapper.is-focus) {
  box-shadow: 0 1px 0 0 var(--kind-accent);
}

.dock-note :deep(.el-input__inner) {
  height: 40px;
  color: var(--hb-ink);
  font-size: 14px;
}

.dock-note :deep(.el-input__inner::placeholder) {
  color: var(--hb-ink-faint);
}

.amount-item.dock-amount {
  flex: 0 1 52%;
  width: auto;
  min-width: 0;
  margin: 0;
}

.dock-amount :deep(.el-form-item__label) {
  display: none;
}

.dock-amount :deep(.el-form-item__content) {
  display: block;
  width: 100%;
}

.dock-amount :deep(.el-form-item__error) {
  position: static;
  padding-top: 2px;
  text-align: right;
}

.amount-display {
  display: flex;
  gap: 6px;
  align-items: center;
  justify-content: flex-end;
  width: 100%;
  padding: 0;
  background: transparent;
  border: 0;
  border-radius: 0;
}

.amount-display:focus-within {
  box-shadow: 0 1px 0 0 var(--kind-accent);
}

.amount-display__currency {
  flex: 0 0 auto;
  color: var(--hb-ink-faint);
  font-size: 18px;
  font-weight: 600;
}

.amount-input {
  flex: 1;
  width: 100%;
  min-width: 0;
  height: 48px;
  padding: 0;
  color: var(--hb-ink);
  font-family: inherit;
  font-size: clamp(28px, 7vw, 36px);
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.02em;
  text-align: right;
  background: transparent;
  border: 0;
  border-radius: 0;
  outline: none;
  appearance: none;
  -webkit-appearance: none;
}

.amount-input::placeholder {
  color: #c7bdad;
  font-weight: 500;
}

.amount-input:disabled {
  color: var(--hb-ink-soft);
  cursor: not-allowed;
}

.amount-input:focus-visible {
  outline: none;
}

.amount-item.is-error .amount-display {
  box-shadow: 0 1px 0 0 var(--hb-danger);
}

.meta-bar {
  display: flex;
  gap: 8px;
  align-items: flex-start;
  min-width: 0;
}

.meta-bar__item {
  flex: 0 0 auto;
  min-width: 0;
  margin: 0;
}

.meta-bar__account {
  flex: 1 1 auto;
}

.meta-bar :deep(.el-form-item__label) {
  display: none;
}

.meta-bar :deep(.el-form-item__content) {
  line-height: 1.1;
}

.meta-bar :deep(.el-form-item__error) {
  position: static;
  padding-top: 2px;
  font-size: 10px;
}

.meta-bar__account :deep(.el-select) {
  width: 100%;
}

.meta-bar__account :deep(.el-select__wrapper),
.meta-bar__date :deep(.el-input__wrapper) {
  min-height: 34px;
  font-size: 12px;
  background: var(--hb-bg);
  border-radius: 9px;
  box-shadow: inset 0 0 0 1px var(--hb-line);
}

.meta-bar__date :deep(.el-date-editor.el-input) {
  width: 156px;
}

.meta-bar__transfer {
  display: flex;
  flex: 1 1 auto;
  gap: 5px;
  align-items: center;
  min-width: 0;
  height: 34px;
  padding: 0 10px;
  color: var(--hb-ink-soft);
  font-size: 12px;
  background: var(--hb-bg);
  border: 1px solid var(--hb-line);
  border-radius: 9px;
}

.meta-bar__transfer span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.amount-keypad {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  grid-auto-rows: minmax(40px, auto);
  gap: 6px;
  width: 100%;
  margin: 0;
}

.keypad-key {
  display: flex;
  align-items: center;
  justify-content: center;
  min-width: 0;
  min-height: 40px;
  padding: 0;
  color: var(--hb-ink);
  font: inherit;
  font-size: 18px;
  font-weight: 500;
  cursor: pointer;
  background: var(--hb-bg);
  border: 1px solid var(--hb-line);
  border-radius: 10px;
  appearance: none;
  touch-action: manipulation;
  -webkit-tap-highlight-color: transparent;
  transition:
    color 0.12s ease,
    background-color 0.12s ease,
    border-color 0.12s ease,
    transform 0.12s ease;
}

.keypad-key:hover:not(:disabled) {
  color: var(--kind-accent);
  background: var(--kind-accent-soft);
  border-color: var(--kind-accent);
}

.keypad-key:active:not(:disabled) {
  background: var(--hb-bg-deep);
  transform: scale(0.98);
}

.keypad-key:disabled {
  cursor: not-allowed;
  opacity: 0.45;
}

.keypad-backspace {
  grid-row: 1;
  grid-column: 4;
  color: var(--hb-ink-soft);
  font-size: 16px;
}

.amount-keypad--edit .keypad-backspace {
  grid-row: 1 / span 2;
}

.keypad-again {
  grid-row: 2;
  grid-column: 4;
  font-size: 14px;
}

.keypad-save {
  grid-row: 3 / span 2;
  grid-column: 4;
  color: #fff;
  background: var(--kind-accent);
  background: color-mix(in srgb, var(--kind-accent) 72%, var(--hb-ink));
  border-color: transparent;
}

.keypad-save:hover:not(:disabled) {
  color: #fff;
  background: var(--kind-accent);
  background: color-mix(in srgb, var(--kind-accent) 72%, var(--hb-ink));
  border-color: transparent;
  filter: brightness(0.94);
}

.keypad-save:disabled {
  color: #fff;
  opacity: 0.6;
}

.keypad-save__label {
  font-size: 14px;
  font-weight: 600;
  letter-spacing: 0.12em;
  line-height: 1.1;
  writing-mode: vertical-rl;
}

.keypad-zero {
  grid-column: 1 / span 2;
}

.dialog-notice {
  display: flex;
  gap: 11px;
  align-items: center;
  margin-bottom: 14px;
  padding: 11px 13px;
  background: #fdf5e9;
  border: 1px solid #f0dfc5;
  border-radius: 12px;
}

.dialog-notice--inline {
  margin-bottom: 10px;
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

.amount-dock {
  display: none;
}

:global(.el-dialog.ledger-dialog.quick-entry-dialog) {
  --el-dialog-margin-top: 8vh;
}

:global(.el-dialog.quick-entry-dialog .el-dialog__header) {
  padding: 16px 18px 10px;
  margin-right: 0;
}

:global(.el-dialog.quick-entry-dialog .el-dialog__body) {
  max-height: calc(100vh - 180px);
  max-height: calc(100dvh - 180px);
  padding-top: 4px;
  overflow-x: hidden;
  overflow-y: auto;
}

:global(.el-dialog.quick-entry-dialog .el-dialog__footer) {
  display: none;
}

@media (max-width: 760px) {
  :global(.el-dialog.quick-entry-dialog.is-fullscreen) {
    display: flex;
    flex-direction: column;
    width: 100%;
    max-width: none;
    height: 100vh;
    height: 100dvh;
    margin: 0;
    padding: 0;
    overflow: hidden;
    border-radius: 0;
  }

  :global(.quick-entry-dialog.is-fullscreen .el-dialog__header) {
    flex: 0 0 auto;
    padding: calc(10px + env(safe-area-inset-top, 0px))
      calc(14px + env(safe-area-inset-right, 0px)) 10px
      calc(14px + env(safe-area-inset-left, 0px));
    margin: 0;
    border-bottom: 1px solid var(--hb-line);
  }

  :global(.quick-entry-dialog.is-fullscreen .el-dialog__body) {
    flex: 1;
    min-height: 0;
    max-height: none;
    padding: 10px calc(14px + env(safe-area-inset-right, 0px)) 12px
      calc(14px + env(safe-area-inset-left, 0px));
    overscroll-behavior-y: contain;
  }

  :global(.quick-entry-dialog.is-fullscreen .el-dialog__footer) {
    display: block;
    flex: 0 0 auto;
    padding: 8px calc(12px + env(safe-area-inset-right, 0px))
      calc(8px + env(safe-area-inset-bottom, 0px))
      calc(12px + env(safe-area-inset-left, 0px));
    background: var(--hb-surface);
    border-top: 1px solid var(--hb-line);
  }

  .amount-dock {
    display: block;
    width: 100%;
  }

  .entry-dock {
    gap: 7px;
  }

  .entry-main {
    min-height: 140px;
  }

  .category-grid {
    grid-template-columns: repeat(5, minmax(0, 1fr));
    gap: 6px;
  }

  .category-tile {
    min-height: 62px;
    padding: 6px 2px;
    font-size: 11.5px;
  }

  .category-tile .el-icon {
    font-size: 22px;
  }

  .amount-keypad {
    grid-auto-rows: clamp(40px, 6.6dvh, 54px);
  }

  .keypad-key {
    font-size: 19px;
  }

  .keypad-save {
    font-size: 14px;
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

@media (max-width: 370px) {
  .dock-row {
    gap: 6px;
  }

  .amount-item.dock-amount {
    flex-basis: 50%;
  }

  .amount-input {
    font-size: 26px;
  }

  .meta-bar__date :deep(.el-date-editor.el-input) {
    width: 138px;
  }

  .transfer-row {
    grid-template-columns: minmax(0, 1fr) 32px minmax(0, 1fr);
    gap: 6px;
  }

  .swap-button {
    width: 32px;
  }

  .fixed-category {
    flex-wrap: wrap;
    height: auto;
    padding: 7px 10px;
  }

  .fixed-category small {
    margin-left: 0;
  }
}

@media (min-width: 761px) {
  .meta-bar__date :deep(.el-date-editor.el-input) {
    width: 190px;
  }
}
</style>
