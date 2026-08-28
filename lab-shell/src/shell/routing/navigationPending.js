import { ref } from 'vue'

/*
  Is the shell currently waiting on a plugin's chunk? (Task 1b-6, BR-AS08.)

  A route contribution resolves its component through the loader, so a deep
  link into a plugin that has never been loaded spends real time — a network
  fetch — between the click and the first pixel. vue-router renders nothing at
  all during that window: the outgoing view stays, then the new one appears.
  For a shell whose whole premise is that features arrive at runtime, "nothing
  happened yet" is exactly the wrong reading of a slow remote.

  So the flag is raised on any navigation *into a plugin route* and lowered
  when the navigation settles either way. Shell-owned routes are excluded on
  purpose: they are in the host bundle and resolve within a frame, and a
  skeleton that flashes for 4ms is worse than no skeleton.
*/
export function createNavigationPending(router) {
  const pending = ref(false)
  /* What is being waited for, so the placeholder can name the contribution
     rather than saying "loading" into the void (the Loading artboard labels
     the region with the kind and the qualified id). Curated metadata only —
     never the remote's URL (BR-AS04). */
  const target = ref(null)

  router.beforeEach((to) => {
    pending.value = Boolean(to.meta?.pluginId)
    target.value = pending.value
      ? {
          pluginId: to.meta.pluginId,
          title: to.meta.title ?? '',
          contributionId: to.meta.contributionId ?? '',
        }
      : null
  })
  /* Both hooks, because a navigation that fails or is redirected still has to
     put the frame back — a stuck skeleton would be a worse lie than none. */
  const settle = () => {
    pending.value = false
    target.value = null
  }
  router.afterEach(settle)
  router.onError(settle)

  return { pending, target }
}
