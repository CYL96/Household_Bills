/// <reference types="vite/client" />

/**
 * Synchronous back-navigation hook called by the Android shell. Returns
 * 'handled' when the page consumed the gesture, 'exit' when the shell should
 * leave the app. Absent in a plain browser, where browser history applies.
 */
interface Window {
  __hbBack?: () => 'handled' | 'exit'
}
