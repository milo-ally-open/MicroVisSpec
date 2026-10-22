<script setup lang="ts">
import { useRegisterSW } from 'virtual:pwa-register/vue'

const { offlineReady, needRefresh, updateServiceWorker } = useRegisterSW()
</script>

<template>
  <section
    v-if="needRefresh || offlineReady"
    class="pwa-notice"
    role="status"
    aria-live="polite"
  >
    <p v-if="needRefresh">新版本已准备就绪，更新后即可使用。</p>
    <p v-else>应用页面已缓存，可在离线时打开页面框架。</p>

    <div class="pwa-notice__actions">
      <button v-if="needRefresh" type="button" @click="updateServiceWorker()">
        立即更新
      </button>
      <button type="button" class="secondary" @click="offlineReady = false; needRefresh = false">
        {{ needRefresh ? '稍后' : '知道了' }}
      </button>
    </div>
  </section>
</template>

<style scoped>
.pwa-notice {
  position: fixed;
  z-index: 1000;
  right: 1rem;
  bottom: 1rem;
  max-width: min(28rem, calc(100vw - 2rem));
  padding: 1rem;
  border: 1px solid #e4dfd5;
  border-radius: 6px;
  background: #fffdfa;
  box-shadow: 0 0.75rem 2rem rgb(28 25 23 / 12%);
  color: #1c1917;
}

.pwa-notice p {
  margin: 0 0 0.75rem;
}

.pwa-notice__actions {
  display: flex;
  justify-content: flex-end;
  gap: 0.5rem;
}

button {
  min-height: 2.5rem;
  padding: 0.5rem 0.875rem;
  border: 1px solid #1c1917;
  border-radius: 999px;
  background: #1c1917;
  color: #faf8f4;
  font: inherit;
  cursor: pointer;
}

button.secondary {
  border-color: #ded7ca;
  background: #fffdfa;
  color: #5c544b;
}
</style>
