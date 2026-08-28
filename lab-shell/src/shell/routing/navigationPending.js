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

  router.beforeEach((to) => {
    pending.value = Boolean(to.meta?.pluginId)
  })
  /* Both hooks, because a navigation that fails or is redirected still has to
     put the frame back — a stuck skeleton would be a worse lie than none. */
  router.afterEach(() => {
    pending.value = false
  })
  router.onError(() => {
    pending.value = false
  })

  return pending
}
