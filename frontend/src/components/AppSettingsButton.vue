<script setup lang="ts">
import { Setting } from '@element-plus/icons-vue'

/*
 * Capability hint only: the native shell appends this exact token to its
 * WebView user-agent. The app validates the frame, gesture, origin and state
 * on its own when it intercepts the link.
 */
const APP_WEBVIEW_UA_TOKEN = 'HouseholdBillsApp/1'

const isAppWebView =
  typeof navigator !== 'undefined' &&
  navigator.userAgent.split(/\s+/).includes(APP_WEBVIEW_UA_TOKEN)
</script>

<template>
  <a
    v-if="isAppWebView"
    class="app-settings-link"
    href="household-bills://settings"
    aria-label="应用设置"
    title="应用设置"
    data-testid="app-settings-entry"
  >
    <el-icon :size="20" aria-hidden="true"><Setting /></el-icon>
  </a>
</template>

<style scoped>
.app-settings-link {
  display: grid;
  flex: 0 0 auto;
  width: 44px;
  height: 44px;
  place-items: center;
  color: var(--hb-ink-soft);
  background: var(--hb-surface);
  border: 1px solid var(--hb-line);
  border-radius: 12px;
  -webkit-tap-highlight-color: transparent;
  transition:
    color 0.18s ease,
    background-color 0.18s ease,
    border-color 0.18s ease;
}

.app-settings-link:hover {
  color: var(--hb-teal-dark);
  background: var(--hb-surface-raised);
  border-color: var(--hb-line-strong);
}

.app-settings-link:active {
  background: var(--hb-teal-faint);
}

.app-settings-link:focus-visible {
  outline: 2px solid var(--hb-teal);
  outline-offset: 2px;
}
</style>
