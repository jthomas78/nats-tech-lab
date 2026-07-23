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

        <button class="sidebar-collapse-btn" type="button" @click="toggleCollapsed">
          <span class="chevrons">&laquo;</span>
        </button>
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
