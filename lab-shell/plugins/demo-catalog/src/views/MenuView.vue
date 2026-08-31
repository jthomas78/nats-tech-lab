<script setup>
import Button from 'primevue/button'
import Tag from 'primevue/tag'

import { demos } from '../demos'

function launch(demo) {
  window.open(demo.launchUrl, '_blank')
}
</script>

<template>
  <div class="menu">
    <article
      v-for="demo in demos"
      :key="demo.id"
      class="lab-panel demo-card"
    >
      <div class="demo-info">
        <h2>{{ demo.title }}</h2>
        <p class="lab-muted">
          {{ demo.description }}
        </p>
        <div class="tags">
          <Tag
            v-for="tag in demo.tags"
            :key="tag"
            :value="tag"
            severity="secondary"
          />
        </div>
      </div>
      <div class="demo-actions">
        <router-link :to="{ name: 'demo-catalog/intro', params: { id: demo.id } }">
          <Button
            label="Intro"
            size="small"
            severity="secondary"
            outlined
          />
        </router-link>
        <Button
          label="Launch"
          size="small"
          icon="pi pi-external-link"
          @click="launch(demo)"
        />
      </div>
    </article>
  </div>
</template>

<style scoped>
.menu {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}
.demo-card {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 1rem;
}
.demo-info h2 {
  margin: 0 0 0.35rem;
  font-size: 1.05rem;
}
.demo-info p {
  margin: 0 0 0.6rem;
}
.tags {
  display: flex;
  gap: 0.35rem;
}
.demo-actions {
  display: flex;
  gap: 0.5rem;
  flex-shrink: 0;
}
</style>
