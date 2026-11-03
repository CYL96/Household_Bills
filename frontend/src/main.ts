import { createApp, nextTick } from 'vue'
import ElementPlus from 'element-plus'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import 'element-plus/dist/index.css'
import App from './App.vue'
import router from './router'
import { installBackBridge } from './composables/backNavigation'
import {
  holdWebUpdate,
  installNativeUpdateSignals,
  startWebUpdates,
} from './composables/webUpdates'
import { bootstrapAuth } from './stores/auth'
import { ensureCategories } from './stores/categories'
import './styles/main.css'

// The native shell may ask for a back step before the first navigation
// settles, so install the hook as soon as the module loads.
installBackBridge()

// Install the native foreground/background listeners before the first await:
// the shell can emit `pause` right after the page finishes loading, while auth
// bootstrap and the router are still pending, and WebView visibility alone is
// not trustworthy. Updates stay blocked until the app is mounted below, so an
// early `resume` cannot reload the boot screen or lose the restored session.
installNativeUpdateSignals()
const releaseStartupUpdateHold = holdWebUpdate()

const app = createApp(App)
app.use(ElementPlus, { locale: zhCn })

async function startApp(): Promise<void> {
  try {
    // Resolve the session before the router guard performs its initial navigation.
    try {
      await bootstrapAuth()
    } catch {
      // bootstrapAuth records failures in authState.bootError; keep the app usable.
    }
    // Preload the shared category snapshot; dialogs still force a refresh when
    // they open, and the store ignores responses that outlive their identity.
    await nextTick()
    void ensureCategories()
    app.use(router)
    await router.isReady()
    app.mount('#app')
    // Only now is the restored session mounted and the current deep link routed;
    // an update check can never interrupt boot or bounce the user to '/'.
    startWebUpdates()
  } finally {
    releaseStartupUpdateHold()
  }
}

void startApp()
