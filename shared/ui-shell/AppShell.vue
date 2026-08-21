<script setup>
// Shared app shell — topbar + collapsible sidebar + main content.
// See shared/unifi-theme/LAYOUT.md for the contract and
// shared/unifi-theme/app-shell-reference.html for the canonical markup this
// is extracted from.
//
// Deliberately dependency-free of vue-router and vue-i18n — not all 4 apps
// that consume this component have either. Anything app-specific (locale
// switcher, fleet-context select, connection status tag) is supplied via
// the #topbar-right slot instead of being baked in here.
import { ref } from 'vue'
import { isDark, toggleTheme } from '@unifi-theme/preset.js'

const dark = ref(isDark())

function handleToggleTheme() {
  toggleTheme()
  dark.value = isDark()
}

const collapsed = ref(false)

function toggleCollapsed() {
  collapsed.value = !collapsed.value
}
</script>

<template>
  <div class="app">
    <header class="topbar">
      <div class="brandmark">
        <slot name="brand" />
      </div>

      <div class="crumb">
        <slot name="breadcrumb" />
      </div>

      <div class="topbar-right">
        <slot name="topbar-right" />
        <button
          type="button"
          class="icon-btn"
          :aria-label="dark ? 'Switch to light mode' : 'Switch to dark mode'"
          @click="handleToggleTheme"
        >
          <i :class="dark ? 'pi pi-sun' : 'pi pi-moon'" />
        </button>
      </div>
    </header>

    <div class="shell">
      <div v-if="$slots.sidebar" class="sidebar" :class="{ collapsed }">
        <div class="nav-scroll">
          <slot name="sidebar" />
        </div>

        <!-- Collapse control sits at the BOTTOM of the rail, right-aligned —
             `.nav-scroll { flex: 1 }` above it does the pushing. The glyph is a
             panel-toggle icon drawn inline rather than a PrimeIcon, since
             PrimeIcons has no panel-left/panel-right equivalent; the filled bar
             moves to the far side to indicate which way it opens. -->
        <div class="sidebar-foot">
          <button
            class="sidebar-collapse-btn"
            type="button"
            :aria-label="collapsed ? 'Expand sidebar' : 'Collapse sidebar'"
            :aria-expanded="!collapsed"
            @click="toggleCollapsed"
          >
            <svg
              width="15"
              height="15"
              viewBox="0 0 16 16"
              fill="none"
              stroke="currentColor"
              stroke-width="1.3"
              aria-hidden="true"
            >
              <rect x="1.6" y="2.6" width="12.8" height="10.8" rx="2.4" />
              <path :d="collapsed ? 'M9.8 2.6v10.8' : 'M6.2 2.6v10.8'" />
              <rect
                :x="collapsed ? 11.1 : 2.9"
                y="3.9"
                width="2"
                height="8.2"
                rx="1"
                fill="currentColor"
                stroke="none"
                opacity="0.55"
              />
            </svg>
          </button>
        </div>
      </div>

      <div class="main">
        <div class="main-inner">
          <slot />
        </div>
        <div
          v-if="$slots.footer"
          class="main-footer"
        >
          <slot name="footer" />
        </div>
      </div>
    </div>
  </div>
</template>

<style>
@import './app-shell.css';
</style>
