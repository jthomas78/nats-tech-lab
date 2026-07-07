// Shared UniFi-aesthetic PrimeVue v4 preset, imported by BOTH lab-shell/
// and demos/*/frontend/ so styling stays in sync (see CLAUDE.md).
//
// Base: Aura (darkest built-in preset). Text tokens are the community
// reverse-engineered UniFi values from the plan (medium confidence).
// Background + accent tokens below are UniFi-flavoured placeholders — the
// plan calls for extracting the real values from a live UniFi instance via
// devtools (:root --ubnt-* / --unifi-*) and replacing them here.
// This file lives outside the apps' node_modules resolution scope, so it
// must stay dependency-free: apps pass in their own definePreset + Aura.
//
//   import { definePreset } from '@primevue/themes'
//   import Aura from '@primevue/themes/aura'
//   const preset = createUnifiPreset(definePreset, Aura)

export const createUnifiPreset = (definePreset, Aura) =>
  definePreset(Aura, {
  semantic: {
    primary: {
      50: '#e3f0ff',
      100: '#b8d8ff',
      200: '#8abeff',
      300: '#5ca4ff',
      400: '#3390ff',
      500: '#006fff', // UniFi-ish accent blue (placeholder)
      600: '#0063e6',
      700: '#0054c2',
      800: '#00459e',
      900: '#00306e',
      950: '#001d42',
    },
    colorScheme: {
      dark: {
        surface: {
          0: '#ffffff',
          50: '#dee0e3',
          100: '#b7bcc2',
          200: '#9aa1a9',
          300: '#737c87',
          400: '#4e5560',
          500: '#3a4049',
          600: '#2c3138',
          700: '#22262c', // panel / card background
          800: '#1a1e23', // content background
          900: '#14171b', // app background
          950: '#0f1114',
        },
        text: {
          color: '#DEE0E3', // primary text (proxmorph, 2-1 vote)
          mutedColor: '#B7BCC2', // secondary / label
        },
      },
    },
  },
})

// UniFi (like PrimeVue v4's default) toggles dark mode with a class on the
// document element. Call once at app startup; the lab defaults to dark.
export function enableDarkMode() {
  document.documentElement.classList.add('p-dark')
}

export const themeOptions = {
  darkModeSelector: '.p-dark',
}
