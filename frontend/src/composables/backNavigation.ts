import { onBeforeUnmount, watch } from 'vue'
import router from '../router'

export type NativeBackResult = 'handled' | 'exit'

type BackHandler = () => void

/**
 * Overlays register here in the order they become visible. The native shell
 * asks for exactly one back step, so only the most recently opened layer is
 * consumed; closing it re-exposes the layer underneath.
 */
const backLayers: BackHandler[] = []

export function pushBackLayer(handler: BackHandler): () => void {
  backLayers.push(handler)
  let released = false
  return () => {
    if (released) return
    released = true
    const index = backLayers.lastIndexOf(handler)
    if (index >= 0) backLayers.splice(index, 1)
  }
}

/**
 * Keeps a layer registered while `active` is true. `flush: 'sync'` releases it
 * in the same tick as the overlay closes, so the next back press sees the
 * remaining visible layers rather than a stale handler.
 */
export function useBackLayer(
  active: () => boolean,
  handler: BackHandler,
): void {
  let release: (() => void) | null = null

  watch(
    active,
    (open) => {
      if (open) {
        if (!release) release = pushBackLayer(handler)
        return
      }
      release?.()
      release = null
    },
    { immediate: true, flush: 'sync' },
  )

  onBeforeUnmount(() => {
    release?.()
    release = null
  })
}

function isProtectedAppPath(path: unknown): boolean {
  if (typeof path !== 'string') return false
  // Vue Router stores fullPath strings here; reject external/ambiguous values.
  if (!path.startsWith('/') || path.startsWith('//')) return false
  if (
    path === '/login' ||
    path.startsWith('/login?') ||
    path.startsWith('/login#') ||
    path.startsWith('/login/')
  ) {
    return false
  }
  try {
    return router
      .resolve(path)
      .matched.some((record) => record.meta.requiresAuth === true)
  } catch {
    return false
  }
}

/**
 * No overlay is open: step through in-app history, fall back to the overview
 * from a history-less entry page, and only exit from the dashboard or login.
 * Returning from login never rewinds into a guarded route, which would bounce
 * through the auth guard back to login.
 */
function handlePageBack(): NativeBackResult {
  const current = router.currentRoute.value
  if (!current.matched.length) return 'exit'
  if (current.name === 'login') return 'exit'

  if (isProtectedAppPath(window.history.state?.back)) {
    router.back()
    return 'handled'
  }

  if (current.name !== 'dashboard') {
    void router.replace('/')
    return 'handled'
  }

  return 'exit'
}

/**
 * Installs the synchronous hook the Android shell calls through
 * `evaluateJavascript`. Old APKs never call it, so web behavior is unchanged.
 */
export function installBackBridge(): void {
  if (typeof window === 'undefined') return
  window.__hbBack = (): NativeBackResult => {
    const top = backLayers[backLayers.length - 1]
    if (top) {
      top()
      return 'handled'
    }
    return handlePageBack()
  }
}
