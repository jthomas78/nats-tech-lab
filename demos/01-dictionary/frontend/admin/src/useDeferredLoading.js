import { onUnmounted, ref, watch } from 'vue'

// Deferred loading flag (2026-08-29).
//
// PrimeVue's DataTable mask is wrapped in a Vue transition whose leave
// animation runs for `--p-mask-transition-duration` — 0.3s in this theme. The
// mask therefore stays on screen for ~300ms AFTER `loading` goes false, and
// because the leave keyframes animate only `background`, the spinner icon
// inside holds full opacity for that whole tail and then vanishes. A panel
// that fetches in 40ms showed a ~340ms spinner: the overlay outlived the work
// by roughly 7x, and read as a flash rather than as progress.
//
// Shortening the theme's mask duration was the other option and was rejected —
// it is a global token that also drives real modal overlays, where the fade is
// wanted. The fix belongs at the point where a *table* decides to show a mask
// at all.
//
// So: bind the table to this instead of to the raw flag. A load that finishes
// inside the threshold never raises the mask, so there is nothing to fade out
// and the fast case shows nothing at all. A genuinely slow load still gets its
// overlay, only ~DELAY_MS late — which is the trade this pattern makes
// everywhere it is used, and is invisible next to the wait it is reporting.
//
// The threshold is above the ~100ms that reads as instant and below the ~1s
// where an unacknowledged wait starts to feel broken.
const DELAY_MS = 180

export function useDeferredLoading(source, delayMs = DELAY_MS) {
  const visible = ref(false)
  let timer = null

  function clear() {
    if (timer) {
      clearTimeout(timer)
      timer = null
    }
  }

  watch(
    source,
    (on) => {
      clear()
      if (!on) {
        // Down goes through immediately. Holding the mask for the rest of the
        // threshold would reintroduce exactly the tail this exists to remove.
        visible.value = false
        return
      }
      timer = setTimeout(() => {
        visible.value = true
        timer = null
      }, delayMs)
    },
    // Panels whose flag starts true (the mount fetch) must arm on creation,
    // not on the first change — otherwise their one slow load is the only one
    // that never reports itself.
    { immediate: true },
  )

  onUnmounted(clear)

  return visible
}
