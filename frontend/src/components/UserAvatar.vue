<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { User } from '../api/types'
import { displayInitial, displayName } from '../utils/user'

const props = withDefaults(
  defineProps<{
    user: User
    size?: number
  }>(),
  { size: 36 },
)

const failed = ref(false)

const imageSource = computed(() => {
  const version = props.user.avatar_version ?? 0
  if (version <= 0) return ''
  return `/api/users/${props.user.id}/avatar/?v=${version}`
})

const showImage = computed(() => Boolean(imageSource.value) && !failed.value)
const label = computed(() => `${displayName(props.user)}的头像`)
const initial = computed(() => displayInitial(props.user))
const style = computed(() => ({
  width: `${props.size}px`,
  height: `${props.size}px`,
  fontSize: `${Math.max(12, Math.round(props.size * 0.38))}px`,
}))

watch(
  () => [props.user.id, props.user.avatar_version],
  () => {
    failed.value = false
  },
)
</script>

<template>
  <span
    class="user-avatar"
    :style="style"
    :data-testid="`user-avatar-${user.id}`"
    :data-avatar-version="user.avatar_version"
    :aria-label="showImage ? undefined : label"
  >
    <img
      v-if="showImage"
      :src="imageSource"
      :alt="label"
      data-testid="user-avatar-image"
      @error="failed = true"
    />
    <span v-else aria-hidden="true" data-testid="user-avatar-initial">{{ initial }}</span>
  </span>
</template>

<style scoped>
.user-avatar {
  display: grid;
  flex: 0 0 auto;
  overflow: hidden;
  place-items: center;
  color: var(--hb-teal-dark);
  background: var(--hb-teal-soft);
  border: 1px solid #d2e8e2;
  border-radius: 50%;
  font-weight: 600;
  line-height: 1;
  user-select: none;
}

.user-avatar img {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: cover;
}
</style>
