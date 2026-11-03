import { nextTick, onBeforeUnmount, toValue, watch, type MaybeRefOrGetter } from 'vue'

/**
 * In-app update detection.
 *
 * Every production build stamps the same identifier into `index.html`
 * (`meta[name="hb-build-id"]`) and into the bundle (`__HB_BUILD_ID__`). While
 * the app runs it periodically re-fetches the same-origin entry document with
 * `cache: 'no-store'`; a different identifier means the server now deploys a
 * newer build. The entry HTML is the only version source. Before reloading, a
 * session marker records the target build, so an entry cached by a proxy or
 * WebView cannot trigger repeated automatic reloads; when that marker cannot be
 * stored, the automatic reload is skipped and the update waits for a manual
 * app restart.
 *
 * A detected update is only pending until it is safe to reload: no editor is
 * open (`useWebUpdateBlocker`), no mutation or confirmation is in flight
 * (`holdWebUpdate`), the page is visible and online, the native shell has not
 * paused us, and the URL/cookies/form state are untouched by this module. A
 * pending target is re-validated once it goes stale instead of being reloaded
 * blindly.
 */

const NATIVE_PAUSE_EVENT = 'household-bills:pause'
const NATIVE_RESUME_EVENT = 'household-bills:resume'

/**
 * Session-scoped marker of the build a reload was already attempted for. A
 * proxy or WebView that ignores revalidation can serve an old entry document
 * even though the detection fetch saw a new one. The marker limits the
 * automatic reload to one attempt per target build; if storage is unavailable
 * the reload is not attempted at all, so a stale entry cannot trap the app in a
 * reload loop (the user can still update by restarting the app). This is not
 * app data or a login record and is dropped at session end.
 */
const RELOAD_ATTEMPT_KEY = 'hb:auto-reload-target'

/** Upper bound for the entry document request. */
const UPDATE_CHECK_TIMEOUT_MS = 8_000
/** Throttle for visibility, online, and polling triggers. */
const CHECK_THROTTLE_MS = 30_000
/** Foreground stale-target revalidation floor; native resume checks at once. */
const FORCE_CHECK_MIN_INTERVAL_MS = 5_000
/** Background-safe foreground polling; the visibility trigger covers wake-ups. */
const FOREGROUND_CHECK_INTERVAL_MS = 120_000
/**
 * Age after which a pending target is no longer trusted at a safe point: the
 * user may have edited or been away for long enough that the server changed or
 * went away. A stale target is re-validated before any reload.
 */
const PENDING_FRESH_MS = 30_000

let starts = 0
let enabled = false
let protectionCount = 0
let pendingVersion: string | null = null
let pendingCheckedAt = 0
let nativePaused = false
let checking: Promise<void> | null = null
let lastCheckAt = 0
let reloadScheduled = false
let reloadTimer: number | null = null
let listenersInstalled = false
let nativeSignalsInstalled = false

function currentBuildId(): string {
  return typeof __HB_BUILD_ID__ === 'string' ? __HB_BUILD_ID__ : ''
}

function readReloadAttempt(): string | null {
  try {
    return window.sessionStorage.getItem(RELOAD_ATTEMPT_KEY)
  } catch {
    return null
  }
}

function writeReloadAttempt(version: string): boolean {
  try {
    window.sessionStorage.setItem(RELOAD_ATTEMPT_KEY, version)
    return true
  } catch {
    // Restricted storage cannot guarantee one reload per target; returning
    // false keeps the automatic reload off instead of risking a loop.
    return false
  }
}

function pruneReloadAttempt(): void {
  try {
    if (window.sessionStorage.getItem(RELOAD_ATTEMPT_KEY) === currentBuildId()) {
      window.sessionStorage.removeItem(RELOAD_ATTEMPT_KEY)
    }
  } catch {
    // Ignore restricted storage.
  }
}

function isVisible(): boolean {
  return document.visibilityState === 'visible'
}

function isOnline(): boolean {
  return navigator.onLine !== false
}

function canReload(): boolean {
  return (
    enabled &&
    pendingVersion !== null &&
    protectionCount === 0 &&
    !nativePaused &&
    isVisible() &&
    isOnline()
  )
}

function isPendingFresh(): boolean {
  return Date.now() - pendingCheckedAt <= PENDING_FRESH_MS
}

function cancelScheduledReload(): void {
  reloadScheduled = false
  if (reloadTimer !== null) {
    clearTimeout(reloadTimer)
    reloadTimer = null
  }
}

/**
 * Attempts a reload at a safe point. A stale pending target is re-validated
 * first: only a successful, fresh check can lead to a reload, and a failed
 * check clears the target and any queued reload. Reloading waits one Vue render
 * plus a macrotask so a busy tail of an action (response handling, a
 * confirmation closing, an editor opening right after a save) cannot race it.
 */
function maybeReload(): void {
  if (!canReload()) return
  if (!isPendingFresh()) {
    cancelScheduledReload()
    requestCheck('foreground')
    return
  }
  if (reloadScheduled || !currentBuildId()) return

  reloadScheduled = true

  void nextTick(() => {
    if (!reloadScheduled || reloadTimer !== null) return
    reloadTimer = setTimeout(() => {
      reloadTimer = null
      if (!reloadScheduled) return
      reloadScheduled = false
      if (!canReload()) return
      const target = pendingVersion
      if (target === null) return
      if (!isPendingFresh()) {
        requestCheck('foreground')
        return
      }
      // If this exact build was already reloaded into and the served entry is
      // still old (e.g. a proxy ignoring revalidation), stop rather than loop.
      if (readReloadAttempt() === target) return
      // Restricted storage cannot record the attempt, so an automatic reload
      // could loop on a stale entry; keep the app usable and let the user
      // restart to pick up the update instead.
      if (!writeReloadAttempt(target)) return
      // Same document, same URL: cookies, storage, and the current route are
      // preserved; only the cached entry and its assets are re-fetched.
      window.location.reload()
    }, 0)
  })
}

function acquireProtection(): void {
  protectionCount += 1
  // A protection registered in the same tick as a pending reload wins.
  cancelScheduledReload()
}

function releaseProtection(): void {
  if (protectionCount > 0) protectionCount -= 1
  if (protectionCount === 0) maybeReload()
}

/**
 * Registers an in-flight operation (a mutation, a confirmation) that must not
 * be interrupted by an automatic reload. The returned release is idempotent.
 */
export function holdWebUpdate(): () => void {
  acquireProtection()
  let released = false
  return () => {
    if (released) return
    released = true
    releaseProtection()
  }
}

/**
 * Keeps update protection registered while `active` is true: typically whether
 * an editor dialog is open. `flush: 'sync'` means an overlay opening or closing
 * takes effect in the same tick it happens, so a reload can never slip between
 * the click and the editor becoming visible.
 */
export function useWebUpdateBlocker(active: MaybeRefOrGetter<boolean>): void {
  let release: (() => void) | null = null

  const apply = (blocked: boolean): void => {
    if (blocked) {
      release ??= holdWebUpdate()
      return
    }
    release?.()
    release = null
  }

  watch(() => toValue(active), (blocked) => apply(Boolean(blocked)), {
    immediate: true,
    flush: 'sync',
  })

  onBeforeUnmount(() => {
    release?.()
    release = null
  })
}

function parseBuildId(html: string): string | null {
  // Parsing as a detached document handles any attribute order/quoting without
  // hand-written HTML matching, and never loads subresources.
  const doc = new DOMParser().parseFromString(html, 'text/html')
  const content = doc
    .querySelector('meta[name="hb-build-id"]')
    ?.getAttribute('content')
    ?.trim()
  return content ? content : null
}

async function fetchEntryBuildId(signal: AbortSignal): Promise<string | null> {
  const response = await fetch('/', {
    method: 'GET',
    cache: 'no-store',
    credentials: 'same-origin',
    headers: { Accept: 'text/html' },
    signal,
  })
  if (!response.ok) return null
  const contentType = response.headers.get('content-type') ?? ''
  if (contentType && !contentType.includes('text/html')) return null
  return parseBuildId(await response.text())
}

function setPending(version: string): void {
  pendingVersion = version
  pendingCheckedAt = Date.now()
}

function clearPending(): void {
  pendingVersion = null
  pendingCheckedAt = 0
  cancelScheduledReload()
}

/** How much throttling a check trigger accepts. */
type CheckPriority = 'throttled' | 'foreground' | 'immediate'

/**
 * Checks the entry document once. Failures (offline, timeout, unexpected
 * response) are silent: the app keeps running, any pending target is dropped,
 * and a later foreground trigger checks again. Concurrent triggers share a
 * single request.
 */
function requestCheck(priority: CheckPriority = 'throttled'): void {
  if (!enabled || nativePaused || !isVisible() || !isOnline() || !currentBuildId()) {
    return
  }
  if (checking) return

  const minInterval =
    priority === 'immediate'
      ? 0
      : priority === 'foreground'
        ? FORCE_CHECK_MIN_INTERVAL_MS
        : CHECK_THROTTLE_MS
  if (Date.now() - lastCheckAt < minInterval) return

  lastCheckAt = Date.now()
  // This round is now the authority: drop the previous target and any queued
  // reload so a protection release during the probe cannot reload from a
  // still-fresh but now unverified pending version. A successful probe
  // re-registers it before maybeReload runs.
  clearPending()
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), UPDATE_CHECK_TIMEOUT_MS)

  checking = (async () => {
    try {
      const serverVersion = await fetchEntryBuildId(controller.signal)
      if (serverVersion === null) {
        // The server currently serves an entry without a marker (legacy or a
        // deploy in progress): never auto-reload onto it.
        clearPending()
        return
      }
      if (serverVersion === currentBuildId()) {
        clearPending()
        return
      }
      setPending(serverVersion)
      maybeReload()
    } catch {
      // Offline or transient failure: keep the current app usable and forget
      // the target; a later foreground trigger will check again.
      clearPending()
    } finally {
      clearTimeout(timeout)
    }
  })().finally(() => {
    checking = null
  })
}

function handleVisibilityChange(): void {
  if (isVisible()) {
    // The native resume may arrive while the WebView still reports hidden, so
    // that attempt was dropped; this visibility transition must revalidate now
    // rather than wait for throttling. Single-flight dedupes both triggers.
    requestCheck('immediate')
  } else {
    cancelScheduledReload()
  }
}

function handleOnline(): void {
  requestCheck('immediate')
}

function handleNativePause(): void {
  nativePaused = true
  cancelScheduledReload()
}

function handleNativeResume(): void {
  nativePaused = false
  // Resume re-validates unconditionally; a stale target is never reloaded
  // directly, and a failed check drops it instead of breaking the page.
  requestCheck('immediate')
}

/**
 * Installs the native foreground/background signal listeners. Called before the
 * first startup await so an early `pause` (the shell may emit it right after
 * page load) is remembered; this does not enable detection by itself and is
 * idempotent.
 */
export function installNativeUpdateSignals(): void {
  if (nativeSignalsInstalled) return
  nativeSignalsInstalled = true
  window.addEventListener(NATIVE_PAUSE_EVENT, handleNativePause)
  window.addEventListener(NATIVE_RESUME_EVENT, handleNativeResume)
}

function installListeners(): void {
  if (listenersInstalled) return
  listenersInstalled = true
  installNativeUpdateSignals()
  document.addEventListener('visibilitychange', handleVisibilityChange)
  window.addEventListener('online', handleOnline)
  setInterval(() => {
    if (isVisible() && !nativePaused) requestCheck()
  }, FOREGROUND_CHECK_INTERVAL_MS)
}

/**
 * Starts detection. Idempotent and a no-op in development. Call only after the
 * session is bootstrapped and the router has performed its initial navigation,
 * so an update never interrupts the login restore or the boot route.
 */
export function startWebUpdates(): void {
  if (starts > 0) return
  starts += 1

  if (!import.meta.env.PROD || !currentBuildId()) return
  enabled = true
  pruneReloadAttempt()
  installListeners()
  maybeReload()
  requestCheck()
}
