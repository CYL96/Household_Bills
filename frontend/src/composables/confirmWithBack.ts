import { ElMessageBox, type ElMessageBoxOptions } from 'element-plus'
import { pushBackLayer } from './backNavigation'
import { holdWebUpdate } from './webUpdates'

/**
 * Shared destructive-action confirmation. A native back press cancels the
 * confirmation (never confirms it) so callers can keep the existing
 * `if (!confirmed) return` flow and cannot trigger a delete by accident.
 *
 * `ElMessageBox.close()` hides the box without settling its promise in this
 * Element Plus version, hence the explicit cancel signal raced against it.
 *
 * The open confirmation also blocks automatic page updates until it settles
 * (including cancellation) so the prompt cannot disappear mid-action.
 */
export async function confirmWithBack(
  message: string,
  title: string,
  options?: ElMessageBoxOptions,
): Promise<boolean> {
  const releaseUpdateHold = holdWebUpdate()

  let cancelActive = (): void => {}
  const cancelSignal = new Promise<'cancel'>((resolve) => {
    cancelActive = () => resolve('cancel')
  })

  const release = pushBackLayer(() => {
    cancelActive()
    ElMessageBox.close()
  })

  try {
    const outcome = await Promise.race([
      ElMessageBox.confirm(message, title, options).then(
        () => 'confirm' as const,
      ),
      cancelSignal,
    ])
    return outcome === 'confirm'
  } catch {
    return false
  } finally {
    release()
    releaseUpdateHold()
  }
}
