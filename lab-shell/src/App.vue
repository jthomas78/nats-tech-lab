<script setup></script>

<template>
  <div class="shell">
    <header class="topbar">
      <router-link to="/" class="brand">
        <i class="pi pi-sitemap" />
        <span>NATS Tech Lab</span>
      </router-link>
      <span class="lab-muted">JetStream · KV · Postgres · CQRS — pattern evaluation for V3</span>
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
  align-items: baseline;
  gap: 0.75rem;
  padding-bottom: 0.75rem;
  border-bottom: 1px solid var(--lab-panel-border);
  margin-bottom: 1rem;
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
