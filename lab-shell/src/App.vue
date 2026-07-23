<script setup>
import AppShell from '@ui-shell/AppShell.vue'
</script>

<template>
  <AppShell>
    <template #brand>
      <router-link to="/" class="brand">
        <i class="pi pi-sitemap" />
        <span>NATS Tech Lab</span>
      </router-link>
    </template>
    <template #breadcrumb>
      <span class="lab-muted">JetStream · KV · Postgres · CQRS — pattern evaluation for V3</span>
    </template>

    <router-view />
  </AppShell>
</template>

<style scoped>
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
