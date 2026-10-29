<script setup lang="ts">
import type { Component } from 'vue'
import { Collection } from '@element-plus/icons-vue'

defineProps<{
  title: string
  description?: string
  icon?: Component
}>()
</script>

<template>
  <div class="empty-state">
    <div class="empty-state__icon" aria-hidden="true">
      <el-icon :size="22">
        <component :is="icon ?? Collection" />
      </el-icon>
    </div>
    <h3 class="empty-state__title">{{ title }}</h3>
    <p v-if="description" class="empty-state__description">{{ description }}</p>
    <div v-if="$slots.actions" class="empty-state__actions">
      <slot name="actions" />
    </div>
  </div>
</template>

<style scoped>
.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 34px 20px 36px;
  text-align: center;
}

.empty-state__icon {
  display: grid;
  width: 52px;
  height: 52px;
  margin-bottom: 14px;
  place-items: center;
  color: var(--hb-teal);
  background: var(--hb-teal-faint);
  border: 1px solid #d7e9e4;
  border-radius: 16px;
}

.empty-state__title {
  color: var(--hb-ink);
  font-size: 16px;
  font-weight: 600;
}

.empty-state__description {
  max-width: 400px;
  margin-top: 7px;
  color: var(--hb-ink-soft);
  font-size: 13px;
  line-height: 1.7;
}

.empty-state__actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 10px;
  margin-top: 18px;
}
</style>
