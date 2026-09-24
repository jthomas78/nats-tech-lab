// NavList.vue lives in shared/ui-shell/, which has no node_modules or test
// runner of its own (see shared/unifi-theme/LAYOUT.md) — admin is the app
// that drives its grouped `{ group, sections }` shape, so its spec runs here
// under admin's Vitest, reached through the same `@ui-shell` alias the app
// uses.
import { RouterLinkStub, mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import NavList from '@ui-shell/NavList.vue'

const GROUPED = [
  { items: [{ key: 'overview', label: 'Overview' }] },
  {
    group: 'Platform',
    sections: [
      { items: [{ key: 'accounts', label: 'Accounts' }] },
      { eyebrow: 'Organizations', items: [{ key: 'shippers', label: 'Shippers' }] },
    ],
  },
  {
    group: 'System',
    sections: [{ eyebrow: 'NATS', items: [{ key: 'streams', label: 'Streams' }] }],
  },
]

function labelsOf(wrapper) {
  return wrapper.findAll('.nav-item').map((b) => b.text())
}

describe('NavList', () => {
  describe('flat sections (the pre-grouping shape)', () => {
    // seafreight-app still passes a single ungrouped section, and admin's
    // Overview stays ungrouped above both groups — neither may grow a
    // group banner it didn't ask for.
    it('renders items with no group toggle', () => {
      const wrapper = mount(NavList, {
        props: {
          sections: [{ items: [{ key: 'fleet', label: 'Fleet' }, { key: 'port', label: 'Port' }] }],
          modelValue: 'fleet',
        },
      })

      expect(labelsOf(wrapper)).toEqual(['Fleet', 'Port'])
      expect(wrapper.findAll('.nav-group-toggle')).toHaveLength(0)
    })

    it('renders an eyebrow only for a section that has one', () => {
      const wrapper = mount(NavList, {
        props: {
          sections: [{ items: [{ key: 'a', label: 'A' }] }, { eyebrow: 'NATS', items: [{ key: 'b', label: 'B' }] }],
          modelValue: 'a',
        },
      })

      expect(wrapper.findAll('.eyebrow').map((e) => e.text())).toEqual(['NATS'])
    })
  })

  describe('grouped sections', () => {
    it('renders a toggle per group, in array order, above its sections', () => {
      const wrapper = mount(NavList, { props: { sections: GROUPED, modelValue: 'overview' } })

      expect(wrapper.findAll('.nav-group-toggle').map((b) => b.text())).toEqual(['Platform', 'System'])
      expect(labelsOf(wrapper)).toEqual(['Overview', 'Accounts', 'Shippers', 'Streams'])
    })

    // The indent that makes a group's contents read as nested under its
    // banner is `.is-grouped`'s padding. It must land only on grouped
    // bodies — an ungrouped section has no banner to sit under, and the
    // collapsed icon rail zeroes the same padding to keep icons centred.
    it('marks only grouped bodies indentable', () => {
      const wrapper = mount(NavList, { props: { sections: GROUPED, modelValue: 'overview' } })

      const bodies = wrapper.findAll('.nav-group-body')
      expect(bodies).toHaveLength(3)
      expect(bodies.map((b) => b.classes().includes('is-grouped'))).toEqual([false, true, true])
    })

    it('starts every group expanded', () => {
      const wrapper = mount(NavList, { props: { sections: GROUPED, modelValue: 'overview' } })

      for (const toggle of wrapper.findAll('.nav-group-toggle')) {
        expect(toggle.attributes('aria-expanded')).toBe('true')
        expect(toggle.classes()).toContain('is-open')
      }
      expect(wrapper.findAll('.nav-group-body.is-collapsed')).toHaveLength(0)
    })

    it('collapses only the clicked group, leaving the others alone', async () => {
      const wrapper = mount(NavList, { props: { sections: GROUPED, modelValue: 'overview' } })
      const [platform, system] = wrapper.findAll('.nav-group-toggle')

      await platform.trigger('click')

      expect(platform.attributes('aria-expanded')).toBe('false')
      expect(platform.classes()).not.toContain('is-open')
      expect(system.attributes('aria-expanded')).toBe('true')
      expect(wrapper.findAll('.nav-group-body.is-collapsed')).toHaveLength(1)
    })

    it('re-expands on a second click', async () => {
      const wrapper = mount(NavList, { props: { sections: GROUPED, modelValue: 'overview' } })
      const platform = wrapper.find('.nav-group-toggle')

      await platform.trigger('click')
      await platform.trigger('click')

      expect(platform.attributes('aria-expanded')).toBe('true')
      expect(wrapper.findAll('.nav-group-body.is-collapsed')).toHaveLength(0)
    })

    it('never marks an ungrouped section collapsed', async () => {
      // Overview has no group toggle of its own, so nothing can collapse it —
      // if the normalization ever leaked a group id onto it, clicking a real
      // group's toggle could hide it.
      const wrapper = mount(NavList, { props: { sections: GROUPED, modelValue: 'overview' } })

      for (const toggle of wrapper.findAll('.nav-group-toggle')) await toggle.trigger('click')

      expect(wrapper.find('.nav-item').text()).toBe('Overview')
      expect(wrapper.findAll('.nav-group-body.is-collapsed')).toHaveLength(2)
    })
  })

  describe('selection', () => {
    it('marks the modelValue item active wherever it is nested', () => {
      const wrapper = mount(NavList, { props: { sections: GROUPED, modelValue: 'shippers' } })

      const active = wrapper.findAll('.nav-item').filter((b) => b.classes().includes('active'))
      expect(active).toHaveLength(1)
      expect(active[0].text()).toBe('Shippers')
      expect(active[0].attributes('aria-pressed')).toBe('true')
    })

    it('emits update:modelValue with the clicked item key', async () => {
      const wrapper = mount(NavList, { props: { sections: GROUPED, modelValue: 'overview' } })

      await wrapper.findAll('.nav-item')[3].trigger('click')

      expect(wrapper.emitted('update:modelValue')).toEqual([['streams']])
    })
  })

  /*
    Phase 17 (D17-6, amendment A5). Three additions, and the point of every
    one of them is that the existing consumers above are untouched: admin and
    seafreight pass no `to`, no `section.id` and no marker slot, and go on
    rendering buttons with `aria-pressed` exactly as they did.
  */
  describe('link mode', () => {
    const LINKED = [
      {
        eyebrow: 'Features',
        items: [
          { key: 'a/nav', label: 'Vessels', to: { name: 'a/vessels' } },
          { key: 'b/nav', label: 'Berths', to: { name: 'b/berths' } },
        ],
      },
    ]
    const mountLinked = (sections = LINKED) =>
      mount(NavList, {
        props: { sections },
        global: { stubs: { RouterLink: RouterLinkStub } },
      })

    it('renders a link, not a button, when an item carries `to`', () => {
      const items = mountLinked().findAllComponents(RouterLinkStub)

      expect(items).toHaveLength(2)
      expect(items[0].props('to')).toEqual({ name: 'a/vessels' })
      expect(mountLinked().findAll('button.nav-item')).toHaveLength(0)
    })

    it('needs no modelValue, and announces no selection', () => {
      const wrapper = mountLinked()

      expect(wrapper.find('.nav-item').attributes('aria-pressed')).toBeUndefined()
    })

    it('emits nothing when a link is clicked, because the router decides', async () => {
      const wrapper = mountLinked()

      await wrapper.findAll('.nav-item')[0].trigger('click')

      expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    })

    it('still renders a button for an item with no `to`, in the same list', () => {
      const wrapper = mountLinked([
        { items: [{ key: 'a/nav', label: 'Vessels', to: { name: 'a/vessels' } }] },
        { items: [{ key: 'plain', label: 'Plain' }] },
      ])

      expect(wrapper.findAllComponents(RouterLinkStub)).toHaveLength(1)
      expect(wrapper.findAll('button.nav-item')).toHaveLength(1)
    })
  })

  describe('the marker slot', () => {
    it('draws whatever the consumer puts there, per item', () => {
      const wrapper = mount(NavList, {
        props: { sections: GROUPED, modelValue: 'overview' },
        slots: { marker: '<i class="mark">{{ params.item.key }}</i>' },
      })

      expect(wrapper.findAll('.mark').map((m) => m.text())).toEqual([
        'overview',
        'accounts',
        'shippers',
        'streams',
      ])
    })

    it('draws nothing for a consumer that passes no slot', () => {
      const wrapper = mount(NavList, { props: { sections: GROUPED, modelValue: 'overview' } })

      expect(wrapper.findAll('.mark')).toHaveLength(0)
    })
  })

  describe('section identity (amendment A5)', () => {
    // The fallback is the test, not an implementation detail: admin and
    // seafreight pass no id and must keep their sections.
    it('renders a section that carries no id, keyed off its eyebrow', () => {
      const wrapper = mount(NavList, {
        props: {
          sections: [
            { eyebrow: 'One', items: [{ key: 'a', label: 'A' }] },
            { eyebrow: 'Two', items: [{ key: 'b', label: 'B' }] },
          ],
          modelValue: 'a',
        },
      })

      expect(wrapper.findAll('.eyebrow').map((e) => e.text())).toEqual(['One', 'Two'])
      expect(labelsOf(wrapper)).toEqual(['A', 'B'])
    })

    it('keeps a section when only its eyebrow is renamed, because the id is what identifies it', async () => {
      const wrapper = mount(NavList, {
        props: {
          sections: [{ id: 'ops', eyebrow: 'Operations', items: [{ key: 'a', label: 'A' }] }],
          modelValue: 'a',
        },
      })
      const before = wrapper.find('.nav-group').element

      await wrapper.setProps({
        sections: [{ id: 'ops', eyebrow: 'Ops', items: [{ key: 'a', label: 'A' }] }],
      })

      expect(wrapper.find('.eyebrow').text()).toBe('Ops')
      expect(wrapper.find('.nav-group').element).toBe(before)
    })

    it('treats two sections with different ids as different sections', async () => {
      const wrapper = mount(NavList, {
        props: {
          sections: [{ id: 'sea', eyebrow: 'Ops', items: [{ key: 'a', label: 'A' }] }],
          modelValue: 'a',
        },
      })
      const before = wrapper.find('.nav-group').element

      await wrapper.setProps({
        sections: [{ id: 'road', eyebrow: 'Ops', items: [{ key: 'a', label: 'A' }] }],
      })

      expect(wrapper.find('.nav-group').element).not.toBe(before)
    })
  })
})
