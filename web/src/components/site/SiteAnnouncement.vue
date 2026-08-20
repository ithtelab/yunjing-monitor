<template>
  <SiteContentPanel :api-url="apiURL" field="announcement" :title="$t('nav-announcement')" />
</template>

<script setup>
import SiteContentPanel from './SiteContentPanel.vue'

defineProps({
  apiURL: {type: String, default: ''}
})
</script>
