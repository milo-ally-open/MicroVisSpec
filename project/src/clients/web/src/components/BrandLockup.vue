<script setup lang="ts">
withDefaults(defineProps<{
  compact?: boolean
  inverse?: boolean
}>(), {
  compact: false,
  inverse: false,
})
</script>

<template>
  <span class="brand-lockup" :class="{ 'brand-lockup--compact': compact, 'brand-lockup--inverse': inverse }">
    <span class="brand-lockup__logo" aria-hidden="true">
      <img src="/logo.png" alt="">
    </span>
    <span class="brand-lockup__copy">
      <strong>云哨</strong>
      <small>边缘智能识别，云端统一处置</small>
    </span>
  </span>
</template>
