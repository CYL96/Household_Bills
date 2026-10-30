<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { Check } from '@element-plus/icons-vue'
import type { InputInstance } from 'element-plus'
import type { CategoryEntry, CategoryKind } from '../stores/categories'
import {
  CATEGORY_ICON_GROUPS,
  CATEGORY_ICON_OPTIONS,
  DEFAULT_CATEGORY_ICON,
  DEFAULT_CATEGORY_ICON_GROUP,
  categoryIcon,
  type CategoryIconGroupId,
  type CategoryIconId,
} from '../utils/categoryIcons'

const props = defineProps<{
  kind: CategoryKind
  entry?: CategoryEntry
  existingNames: string[]
  disabled?: boolean
  fullscreen?: boolean
}>()

const emit = defineEmits<{
  (event: 'apply', entry: CategoryEntry): void
  (event: 'cancel'): void
}>()

const MAX_CATEGORY_NAME_LENGTH = 10
const ICON_INPUT_NAME = 'category-editor-icon'

const panelRef = ref<HTMLElement | null>(null)
const nameInputRef = ref<InputInstance | null>(null)
const name = ref('')
const icon = ref<CategoryIconId>(DEFAULT_CATEGORY_ICON)
const activeIconGroup = ref<CategoryIconGroupId>(DEFAULT_CATEGORY_ICON_GROUP)
const errorMessage = ref('')

const isEditing = computed(() => props.entry !== undefined)
const submitLabel = computed(() =>
  isEditing.value ? '应用修改' : '添加到列表',
)
const nameLength = computed(() => Array.from(name.value).length)
const legacyLongName = computed(
  () =>
    props.entry !== undefined &&
    Array.from(props.entry.name).length > MAX_CATEGORY_NAME_LENGTH &&
    name.value.trim() === props.entry.name,
)
const overLimit = computed(
  () =>
    nameLength.value > MAX_CATEGORY_NAME_LENGTH &&
    (props.entry === undefined || name.value.trim() !== props.entry.name),
)
const nameDescribedBy = computed(() => {
  const ids = ['category-editor-name-hint']
  if (errorMessage.value) ids.push('category-editor-name-error')
  return ids.join(' ')
})
const existingNameSet = computed(() => {
  const ownName = props.entry?.name.trim()
  const names = props.existingNames
    .map((existing) => existing.trim())
    .filter((existing) => existing.length > 0 && existing !== ownName)
  return new Set(names)
})
const visibleIconOptions = computed(() =>
  CATEGORY_ICON_OPTIONS.filter(
    (option) => option.group === activeIconGroup.value,
  ),
)
const selectedIconLabel = computed(
  () =>
    CATEGORY_ICON_OPTIONS.find((option) => option.id === icon.value)?.label ??
    '通用',
)

function resolveEntryIcon(entry: CategoryEntry | undefined): CategoryIconId {
  if (!entry) return DEFAULT_CATEGORY_ICON
  if (entry.icon) return entry.icon
  // Entries without an explicit icon keep their built-in icon. Resolve it
  // back to a catalog id so renaming or re-submitting cannot drop it.
  const fallback = categoryIcon(entry.name)
  const option = CATEGORY_ICON_OPTIONS.find(
    (candidate) => candidate.component === fallback,
  )
  return option?.id ?? DEFAULT_CATEGORY_ICON
}

function resolveIconGroup(id: CategoryIconId): CategoryIconGroupId {
  return (
    CATEGORY_ICON_OPTIONS.find((option) => option.id === id)?.group ??
    DEFAULT_CATEGORY_ICON_GROUP
  )
}

function resetForm(): void {
  name.value = props.entry?.name ?? ''
  icon.value = resolveEntryIcon(props.entry)
  activeIconGroup.value = resolveIconGroup(icon.value)
  errorMessage.value = ''
}

function focusNameInput(): void {
  void nextTick(() => {
    nameInputRef.value?.focus()
    panelRef.value?.scrollIntoView?.({ block: 'nearest' })
  })
}

onMounted(() => {
  resetForm()
  focusNameInput()
})

function handleNameInput(): void {
  if (errorMessage.value) errorMessage.value = ''
}

function rejectName(message: string): void {
  errorMessage.value = message
  focusNameInput()
}

function handleCancel(): void {
  if (props.disabled) return
  emit('cancel')
}

function submit(): void {
  if (props.disabled) return
  const trimmed = name.value.trim()
  if (!trimmed) {
    rejectName('请输入分类名称')
    return
  }
  if (trimmed.includes('\u0000')) {
    rejectName('分类名称不能包含空字符')
    return
  }
  const renaming = props.entry === undefined || props.entry.name !== trimmed
  if (
    renaming &&
    Array.from(trimmed).length > MAX_CATEGORY_NAME_LENGTH
  ) {
    rejectName(`分类名称最多 ${MAX_CATEGORY_NAME_LENGTH} 个字符`)
    return
  }
  if (existingNameSet.value.has(trimmed)) {
    rejectName('该分类已存在（包括已隐藏分类），请换个名称')
    return
  }

  errorMessage.value = ''
  if (props.entry) {
    emit('apply', {
      ...props.entry,
      name: trimmed,
      icon: icon.value,
    })
    return
  }
  emit('apply', {
    name: trimmed,
    visible: true,
    icon: icon.value,
  })
}

function handleNameKeydown(event: Event | KeyboardEvent): void {
  if (!('isComposing' in event) || event.isComposing) return
  event.preventDefault()
  submit()
}
</script>

<template>
  <section
    ref="panelRef"
    class="category-editor"
    :class="{ 'is-fullscreen': fullscreen }"
  >
    <div class="category-editor__field">
      <div class="category-editor__label-row">
        <label class="category-editor__label" for="category-editor-name">
          分类名称
        </label>
        <span
          id="category-editor-name-hint"
          class="category-editor__hint"
          :class="{ 'is-over': overLimit }"
        >
          已输入 {{ nameLength }} / {{ MAX_CATEGORY_NAME_LENGTH }} 个字符
        </span>
      </div>
      <el-input
        id="category-editor-name"
        ref="nameInputRef"
        v-model="name"
        size="large"
        clearable
        placeholder="例如：宠物、旅行"
        aria-label="分类名称"
        :disabled="disabled"
        :aria-describedby="nameDescribedBy"
        @input="handleNameInput"
        @keydown.enter="handleNameKeydown"
      />
      <p v-if="legacyLongName" class="category-editor__hint-note">
        当前名称超过 {{ MAX_CATEGORY_NAME_LENGTH }} 个字符，保持不变时仍可修改图标；改名后需控制在
        {{ MAX_CATEGORY_NAME_LENGTH }} 个字符内。
      </p>
      <p
        v-if="errorMessage"
        id="category-editor-name-error"
        class="category-editor__error"
        role="alert"
      >
        {{ errorMessage }}
      </p>
    </div>

    <fieldset class="category-editor__icons">
      <legend class="category-editor__label">分类图标</legend>
      <div class="category-icon-groups" role="group" aria-label="图标分组">
        <button
          v-for="group in CATEGORY_ICON_GROUPS"
          :key="group.id"
          type="button"
          class="category-icon-group"
          :class="{ 'is-active': activeIconGroup === group.id }"
          :aria-pressed="activeIconGroup === group.id"
          :disabled="disabled"
          @click="activeIconGroup = group.id"
        >
          {{ group.label }}
        </button>
      </div>
      <p class="category-editor__selection" aria-live="polite">
        当前选择：<strong>{{ selectedIconLabel }}</strong>
      </p>
      <div class="category-icon-grid">
        <label
          v-for="option in visibleIconOptions"
          :key="option.id"
          class="category-icon-option"
          :class="{ 'is-selected': icon === option.id }"
        >
          <input
            v-model="icon"
            class="category-icon-option__input"
            type="radio"
            :name="ICON_INPUT_NAME"
            :value="option.id"
            :disabled="disabled"
          />
          <span class="category-icon-option__icon" aria-hidden="true">
            <el-icon><component :is="option.component" /></el-icon>
          </span>
          <span class="category-icon-option__label">{{ option.label }}</span>
          <span class="category-icon-option__state" aria-hidden="true">
            {{ icon === option.id ? '已选' : '可选' }}
          </span>
          <span
            v-if="icon === option.id"
            class="category-icon-option__check"
            aria-hidden="true"
          >
            <el-icon><Check /></el-icon>
          </span>
        </label>
      </div>
    </fieldset>

    <div class="category-editor__actions">
      <el-button :disabled="disabled" @click="handleCancel">取消</el-button>
      <el-button type="primary" :disabled="disabled" @click="submit">
        {{ submitLabel }}
      </el-button>
    </div>
  </section>
</template>

<style scoped>
.category-editor {
  display: flex;
  flex-direction: column;
  gap: 16px;
  min-width: 0;
}

.category-editor__field {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.category-editor__label-row {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 12px;
  align-items: baseline;
  justify-content: space-between;
}

.category-editor__label {
  padding: 0;
  color: var(--hb-ink);
  font-size: 13px;
  font-weight: 600;
  line-height: 1.4;
}

.category-editor__hint {
  color: var(--hb-ink-faint);
  font-size: 11.5px;
  font-variant-numeric: tabular-nums;
}

.category-editor__hint.is-over {
  color: var(--hb-danger);
  font-weight: 600;
}

.category-editor__hint-note {
  margin: 0;
  color: var(--hb-ink-faint);
  font-size: 11.5px;
  line-height: 1.6;
}

.category-editor__error {
  margin: 0;
  color: var(--hb-danger);
  font-size: 12px;
  line-height: 1.5;
}

.category-editor__icons {
  min-width: 0;
  margin: 0;
  padding: 0;
  border: 0;
}

.category-editor__selection {
  margin: 0 0 10px;
  color: var(--hb-ink-soft);
  font-size: 11.5px;
  line-height: 1.5;
}

.category-editor__selection strong {
  color: var(--hb-teal-dark);
  font-weight: 600;
}

.category-editor__actions {
  position: sticky;
  bottom: 0;
  display: flex;
  gap: 8px;
  justify-content: flex-end;
  padding-top: 10px;
  background: var(--hb-surface-raised);
}

.category-icon-groups {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin: 10px 0;
}

.category-icon-group {
  padding: 4px 10px;
  color: var(--hb-ink-soft);
  font: inherit;
  font-size: 11.5px;
  font-weight: 500;
  line-height: 1.5;
  white-space: nowrap;
  cursor: pointer;
  background: var(--hb-bg);
  border: 1px solid var(--hb-line);
  border-radius: 999px;
  appearance: none;
  -webkit-tap-highlight-color: transparent;
  transition:
    color 0.16s ease,
    background-color 0.16s ease,
    border-color 0.16s ease;
}

.category-icon-group:hover:not(:disabled) {
  background: var(--hb-surface-raised);
  border-color: var(--hb-line-strong);
}

.category-icon-group.is-active {
  color: var(--hb-teal-dark);
  background: var(--hb-teal-faint);
  border-color: var(--hb-teal);
}

.category-icon-group:focus-visible {
  outline: 2px solid var(--hb-teal);
  outline-offset: 2px;
}

.category-icon-group:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

.category-icon-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(84px, 1fr));
  gap: 8px;
  max-height: min(36vh, 320px);
  padding: 2px;
  overflow-y: auto;
  overscroll-behavior: contain;
  scrollbar-gutter: stable;
}

.category-icon-option {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 5px;
  align-items: center;
  justify-content: center;
  min-width: 0;
  min-height: 82px;
  padding: 10px 6px 8px;
  color: var(--hb-ink-soft);
  font-size: 11.5px;
  line-height: 1.3;
  text-align: center;
  cursor: pointer;
  background: var(--hb-bg);
  border: 1px solid var(--hb-line);
  border-radius: 14px;
  -webkit-tap-highlight-color: transparent;
  transition:
    color 0.16s ease,
    background-color 0.16s ease,
    border-color 0.16s ease,
    box-shadow 0.16s ease;
}

.category-icon-option:hover {
  background: var(--hb-surface-raised);
  border-color: var(--hb-line-strong);
}

.category-icon-option.is-selected {
  color: var(--hb-teal-dark);
  background: var(--hb-teal-faint);
  border-color: var(--hb-teal);
  box-shadow: inset 0 0 0 1px var(--hb-teal);
}

.category-icon-option:has(.category-icon-option__input:focus-visible) {
  outline: 2px solid var(--hb-teal);
  outline-offset: 2px;
}

.category-icon-option__input {
  position: absolute;
  width: 1px;
  height: 1px;
  margin: -1px;
  padding: 0;
  overflow: hidden;
  clip: rect(0 0 0 0);
  clip-path: inset(50%);
  white-space: nowrap;
  border: 0;
}

.category-icon-option__icon {
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--hb-teal);
}

.category-icon-option.is-selected .category-icon-option__icon {
  color: var(--hb-teal-dark);
}

.category-icon-option__icon :deep(.el-icon) {
  font-size: 24px;
}

.category-icon-option__label {
  max-width: 100%;
  overflow: hidden;
  color: var(--hb-ink);
  font-weight: 500;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.category-icon-option.is-selected .category-icon-option__label {
  color: var(--hb-teal-dark);
}

.category-icon-option__state {
  color: var(--hb-ink-faint);
  font-size: 10px;
  line-height: 1;
}

.category-icon-option.is-selected .category-icon-option__state {
  color: var(--hb-teal-dark);
  font-weight: 600;
}

.category-icon-option__check {
  position: absolute;
  top: 5px;
  right: 5px;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 16px;
  height: 16px;
  color: #fff;
  font-size: 11px;
  background: var(--hb-teal);
  border-radius: 50%;
}

.category-editor.is-fullscreen {
  min-height: 100%;
}

.category-editor.is-fullscreen .category-editor__field,
.category-editor.is-fullscreen .category-editor__actions {
  flex: 0 0 auto;
}

.category-editor.is-fullscreen .category-editor__icons {
  flex: 1 0 auto;
}

.category-editor.is-fullscreen .category-icon-grid {
  max-height: none;
}

@media (max-width: 480px) {
  .category-editor {
    gap: 14px;
  }

  .category-icon-grid {
    grid-template-columns: repeat(auto-fill, minmax(76px, 1fr));
    gap: 6px;
  }

  .category-icon-option {
    min-height: 76px;
    border-radius: 12px;
  }
}
</style>
