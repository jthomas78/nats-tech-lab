<script setup>
import Button from 'primevue/button'
import { ref } from 'vue'
import { isDark, toggleTheme } from '@unifi-theme/preset.js'

const dark = ref(isDark())

function handleToggleTheme() {
  toggleTheme()
  dark.value = isDark()
}
</script>

<template>
  <div class="shell">
    <header class="topbar">
      <router-link to="/" class="brand">
        <i class="pi pi-sitemap" />
        <span>NATS Tech Lab</span>
      </router-link>
      <span class="lab-muted topbar-desc">JetStream · KV · Postgres · CQRS — pattern evaluation for V3</span>
      <Button
        :icon="dark ? 'pi pi-sun' : 'pi pi-moon'"
        :aria-label="dark ? 'Switch to light mode' : 'Switch to dark mode'"
        text
        rounded
        size="small"
        class="theme-toggle"
        @click="handleToggleTheme"
      />
    </header>
    <main>
      <router-view />
    </main>
  </div>
</template>

<style scoped>
.shell {
  max-width: 1080px;
  margin: 0 auto;
  padding: 0.75rem;
}
.topbar {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding-bottom: 0.75rem;
  border-bottom: 1px solid var(--lab-panel-border);
  margin-bottom: 1rem;
}
.topbar-desc {
  flex: 1;
}
.theme-toggle {
  margin-left: auto;
}
.brand {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  color: var(--p-text-color);
  text-decoration: none;
  font-size: 1rem;
  font-weight: 600;
  letter-spacing: 0.02em;
}
.brand i {
  color: var(--lab-accent);
}
</style>
