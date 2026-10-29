<script setup lang="ts">
import { computed } from 'vue'

type StatTone = 'teal' | 'terracotta' | 'ink' | 'balance'

const props = withDefaults(
  defineProps<{
    label: string
    amount: string
    hint?: string
    tone?: StatTone
  }>(),
  {
    hint: '',
    tone: 'ink',
  },
)

const isNegative = computed(() => props.amount.trim().startsWith('−'))
</script>

<template>
  <article class="stat-card" :class="`stat-card--${tone}`">
    <div class="stat-card__head">
      <span class="stat-card__label">{{ label }}</span>
      <span class="stat-card__marker" aria-hidden="true"></span>
    </div>
    <p class="stat-card__value money-number" :class="{ 'is-negative': isNegative }">
      {{ amount }}
    </p>
    <p v-if="hint" class="stat-card__hint">{{ hint }}</p>
  </article>
</template>

<style scoped>
.stat-card {
  position: relative;
  display: flex;
  flex-direction: column;
  min-height: 150px;
  padding: 20px 22px;
  overflow: hidden;
  background: var(--hb-surface);
  border: 1px solid rgba(216, 205, 188, 0.72);
  border-radius: var(--hb-radius-lg);
  box-shadow: var(--hb-shadow-sm);
}

.stat-card::after {
  content: "";
  position: absolute;
  right: -30px;
  bottom: -48px;
  width: 130px;
  height: 130px;
  border-radius: 50%;
  background: var(--stat-wash, rgba(15, 118, 110, 0.06));
  pointer-events: none;
}

.stat-card__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.stat-card__label {
  color: var(--hb-ink-soft);
  font-size: 13px;
  font-weight: 500;
  letter-spacing: 0.03em;
}

.stat-card__marker {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--stat-accent, var(--hb-teal));
  box-shadow: 0 0 0 4px var(--stat-wash, rgba(15, 118, 110, 0.08));
}

.stat-card__value {
  position: relative;
  z-index: 1;
  margin-top: 14px;
  color: var(--hb-ink);
  font-size: clamp(24px, 2.5vw, 30px);
  font-weight: 600;
  letter-spacing: -0.035em;
  line-height: 1.15;
  overflow-wrap: anywhere;
}

.stat-card__value.is-negative {
  color: var(--hb-danger);
}

.stat-card__hint {
  position: relative;
  z-index: 1;
  margin-top: auto;
  padding-top: 10px;
  color: var(--hb-ink-faint);
  font-size: 12px;
}

.stat-card--teal {
  --stat-accent: var(--hb-teal);
  --stat-wash: rgba(15, 118, 110, 0.08);
}

.stat-card--terracotta {
  --stat-accent: var(--hb-terracotta);
  --stat-wash: rgba(189, 106, 60, 0.09);
}

.stat-card--balance {
  --stat-accent: var(--hb-gold);
  --stat-wash: rgba(181, 138, 58, 0.09);
}

.stat-card--ink {
  --stat-accent: #5f6b5f;
  --stat-wash: rgba(95, 107, 95, 0.07);
}

@media (max-width: 640px) {
  .stat-card {
    min-height: 132px;
    padding: 17px 18px;
  }

  .stat-card__value {
    margin-top: 10px;
    font-size: 25px;
  }
}
</style>
