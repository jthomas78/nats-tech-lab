/*
  D17-6, boundary 2 — a router-backed entry renders a REAL link.

  `NavList.vue`'s own spec lives in demo 01's admin app, because that is the
  app that drives its grouped shape and `shared/ui-shell/` has no runner of
  its own. That app has no `vue-router`, so it can only stub the link. This
  file is the other half: the shell DOES have a router, so these specs mount
  the component against a real one and assert the thing a stub cannot —
  a real `<a href>`, which is what makes open-in-new-tab and the browser's own
  active-route behaviour work. A button that calls `router.push` would pass
  every stubbed spec and fail every one of these.
*/
import NavList from '@ui-shell/NavList.vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

const Blank = { template: '<div />' }

const routerFor = async (start) => {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: Blank },
      { path: '/demo-04/lesson-01', name: 'demo-04/lesson-01', component: Blank },
      { path: '/demo-04/lesson-02', name: 'demo-04/lesson-02', component: Blank },
    ],
  })
  router.push(start)
  await router.isReady()
  return router
}

const SECTIONS = [
  {
    id: 'features',
    eyebrow: 'Features',
    items: [
      { key: 'demo-04/one', label: 'Lesson 01', to: { name: 'demo-04/lesson-01' } },
      { key: 'demo-04/two', label: 'Lesson 02', to: { name: 'demo-04/lesson-02' } },
    ],
  },
]

const mountAt = async (start) =>
  mount(NavList, { props: { sections: SECTIONS }, global: { plugins: [await routerFor(start)] } })

describe('D17-6 — NavList draws a real link in link mode', () => {
  it('renders an anchor with a real href, not a button', async () => {
    const wrapper = await mountAt('/')

    const links = wrapper.findAll('a.nav-item')
    expect(links).toHaveLength(2)
    expect(links[0].attributes('href')).toBe('/demo-04/lesson-01')
    expect(wrapper.findAll('button.nav-item')).toHaveLength(0)
  })

  it('lets the router say which entry is active, and marks only that one', async () => {
    const wrapper = await mountAt('/demo-04/lesson-02')

    const active = wrapper.findAll('.nav-item').filter((a) => a.classes().includes('active'))
    expect(active).toHaveLength(1)
    expect(active[0].text()).toBe('Lesson 02')
  })

  it('moves the mark when the route changes, without being told', async () => {
    const router = await routerFor('/demo-04/lesson-01')
    const wrapper = mount(NavList, { props: { sections: SECTIONS }, global: { plugins: [router] } })

    await router.push('/demo-04/lesson-02')
    await wrapper.vm.$nextTick()

    const active = wrapper.findAll('.nav-item').filter((a) => a.classes().includes('active'))
    expect(active.map((a) => a.text())).toEqual(['Lesson 02'])
  })

  it('claims no selection of its own, so nothing needs modelValue', async () => {
    const wrapper = await mountAt('/demo-04/lesson-01')

    expect(wrapper.find('.nav-item').attributes('aria-pressed')).toBeUndefined()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('draws a marker per item when the shell supplies the slot', async () => {
    const wrapper = mount(NavList, {
      props: { sections: SECTIONS },
      slots: { marker: '<span class="dot">{{ params.item.key }}</span>' },
      global: { plugins: [await routerFor('/')] },
    })

    expect(wrapper.findAll('a.nav-item .dot').map((d) => d.text())).toEqual([
      'demo-04/one',
      'demo-04/two',
    ])
  })
})

/*
  D17-6, boundary 2 again — a root link.

  `/` is the text-prefix of every path in the shell, so a rail that decided
  "active" by string would mark Home on every page. The router does not: it
  decides from the matched route RECORD, and the shell's routes are flat
  siblings. These specs hold that, because the day it stops being true the
  rail goes wrong quietly and on every screen.
*/
describe('D17-6 — a root link is not active on the pages under it', () => {
  const WITH_ROOT = [
    {
      id: 'shell',
      items: [
        { key: 'shell/home', label: 'Home', to: '/' },
        { key: 'demo-04/one', label: 'Lesson 01', to: { name: 'demo-04/lesson-01' } },
      ],
    },
  ]
  const mountRoot = async (start) =>
    mount(NavList, { props: { sections: WITH_ROOT }, global: { plugins: [await routerFor(start)] } })

  const activeLabels = (wrapper) =>
    wrapper.findAll('.nav-item').filter((a) => a.classes().includes('active')).map((a) => a.text())

  it('marks it when you are ON it', async () => {
    expect(activeLabels(await mountRoot('/'))).toEqual(['Home'])
  })

  it('leaves it unmarked on a page merely under it', async () => {
    expect(activeLabels(await mountRoot('/demo-04/lesson-01'))).toEqual(['Lesson 01'])
  })
})

describe('D17-6 — an item may decide its own active state', () => {
  /*
    The router matches route RECORDS, so a link to `/demo-04/lesson-01` is
    dark on `/demo-04/lesson-01/step-2` — the two are sibling records. The
    rail before phase 17 matched descendants by hand, so an item is allowed
    to carry the answer and the router's own matching stands aside.
  */
  const sectionsWith = (active) => [
    {
      id: 'features',
      eyebrow: 'Features',
      items: [
        { key: 'one', label: 'Lesson 01', to: { name: 'demo-04/lesson-01' }, active },
        { key: 'two', label: 'Lesson 02', to: { name: 'demo-04/lesson-02' } },
      ],
    },
  ]

  const mountWith = async (active, start) =>
    mount(NavList, {
      props: { sections: sectionsWith(active) },
      global: { plugins: [await routerFor(start)] },
    })

  it('marks an item that says it is active, on a page the router would not match', async () => {
    /* Standing on `/`, so the router would light neither. Lesson 02 said
       nothing and stays dark; Lesson 01 said yes and is lit. */
    const wrapper = await mountWith(true, '/')

    expect(wrapper.findAll('a.nav-item.active').map((a) => a.text())).toEqual(['Lesson 01'])
  })

  it('un-marks an item that says it is NOT active, even standing on it', async () => {
    const wrapper = await mountWith(false, '/demo-04/lesson-01')

    expect(wrapper.findAll('a.nav-item.active')).toHaveLength(0)
  })

  it('leaves the router in charge of an item that said nothing', async () => {
    const wrapper = await mountWith(false, '/demo-04/lesson-02')

    expect(wrapper.findAll('a.nav-item.active').map((a) => a.text())).toEqual(['Lesson 02'])
  })
})
