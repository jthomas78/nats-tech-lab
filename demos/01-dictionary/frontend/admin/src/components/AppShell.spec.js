// AppShell.vue lives in shared/ui-shell/, which has no node_modules or test
// runner of its own (see shared/unifi-theme/LAYOUT.md) — admin's Vitest drives
// it through the same `@ui-shell` alias the app uses, exactly as
// NavList.spec.js already does for the other shared shell component.
//
// What these specs protect is the design-system rule in CLAUDE.md ("Frontend
// Design System" → the shell's collapse control): every app gets ONE collapse
// control, from this component, in the same place, with the same glyph and the
// same accessible name. An app that wants a different rail toggle is a change
// to this file, not a per-app fork — so the assertions below are deliberately
// about placement and semantics rather than pixel styling.
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import AppShell from '@ui-shell/AppShell.vue'

function mountShell() {
  return mount(AppShell, {
    slots: {
      sidebar: '<div class="nav-item">Overview</div>',
      default: '<p>page</p>',
    },
  })
}

describe('AppShell collapse control', () => {
  it('renders exactly one collapse control, and only when a sidebar exists', () => {
    const withSidebar = mountShell()
    expect(withSidebar.findAll('.sidebar-collapse-btn')).toHaveLength(1)

    // No #sidebar slot means no rail, so no rail toggle to strand.
    const noSidebar = mount(AppShell, { slots: { default: '<p>page</p>' } })
    expect(noSidebar.find('.sidebar').exists()).toBe(false)
    expect(noSidebar.find('.sidebar-collapse-btn').exists()).toBe(false)
  })

  it('sits at the BOTTOM of the rail, as the sidebar\'s last child', () => {
    // Placement is the rule, not an accident of markup order: it was moved to
    // the top of the rail once and the empty band that left between the topbar
    // and the first nav group is why it came back down here.
    const wrapper = mountShell()
    const children = [...wrapper.find('.sidebar').element.children]

    expect(children.at(-1).className).toContain('sidebar-foot')
    expect(children.at(-1).querySelector('.sidebar-collapse-btn')).not.toBeNull()
    expect(children.at(0).className).toContain('nav-scroll')
  })

  it('draws an inline SVG panel-toggle icon, never a text glyph', () => {
    // PrimeIcons has no panel-left/panel-right, and the `«` glyph this
    // replaced announced as "left-pointing double angle quotation mark".
    const btn = mountShell().find('.sidebar-collapse-btn')

    expect(btn.find('svg').exists()).toBe(true)
    expect(btn.text()).toBe('')
    expect(btn.find('svg').attributes('aria-hidden')).toBe('true')
  })

  it('names itself for assistive tech, and keeps aria-expanded in step', async () => {
    const wrapper = mountShell()
    const btn = wrapper.find('.sidebar-collapse-btn')

    expect(btn.attributes('type')).toBe('button')
    expect(btn.attributes('aria-label')).toBe('Collapse sidebar')
    expect(btn.attributes('aria-expanded')).toBe('true')
    expect(wrapper.find('.sidebar').classes()).not.toContain('collapsed')

    await btn.trigger('click')

    expect(btn.attributes('aria-label')).toBe('Expand sidebar')
    expect(btn.attributes('aria-expanded')).toBe('false')
    expect(wrapper.find('.sidebar').classes()).toContain('collapsed')
  })

  it('mirrors the icon by moving its filled bar, not by flipping the whole glyph', async () => {
    // `transform: scaleX(-1)` on the old text chevron; the icon instead swaps
    // which side the filled bar and divider sit on, so the rounded rect's
    // corners stay put.
    const wrapper = mountShell()
    const bar = () => wrapper.find('.sidebar-collapse-btn svg rect:nth-of-type(2)')
    const divider = () => wrapper.find('.sidebar-collapse-btn svg path')

    expect(bar().attributes('x')).toBe('2.9')
    expect(divider().attributes('d')).toBe('M6.2 2.6v10.8')

    await wrapper.find('.sidebar-collapse-btn').trigger('click')

    expect(bar().attributes('x')).toBe('11.1')
    expect(divider().attributes('d')).toBe('M9.8 2.6v10.8')
  })
})
