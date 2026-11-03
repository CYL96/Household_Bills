/// <reference types="vite/client" />

/**
 * Identifier of the production build this bundle was compiled from. Injected
 * by `vite.config.ts`; also written into `index.html` so the running page can
 * detect a newer entry without any API.
 */
declare const __HB_BUILD_ID__: string

/**
 * Synchronous back-navigation hook called by the Android shell. Returns
 * 'handled' when the page consumed the gesture, 'exit' when the shell should
 * leave the app. Absent in a plain browser, where browser history applies.
 */
interface Window {
  __hbBack?: () => 'handled' | 'exit'
}
