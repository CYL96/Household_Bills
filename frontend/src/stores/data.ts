import { ref } from 'vue'

/**
 * Incremented after any account or transaction mutation. Views watching it
 * reload their data so balances, summaries and lists never stay stale.
 */
export const dataVersion = ref(0)

export function markDataChanged(): void {
  dataVersion.value += 1
}
