<script setup>
import { useRoute } from 'vue-router'

const route = useRoute()
</script>

<template>
  <div class="lab-panel">
    <h2>Nothing here</h2>
    <p class="lab-muted">
      No plugin claims <code>{{ route.fullPath }}</code>. It may belong to a plugin that is
      disabled, incompatible with this shell, or not visible to you.
    </p>
    <router-link to="/">
      Back to Home
    </router-link>
  </div>
</template>
