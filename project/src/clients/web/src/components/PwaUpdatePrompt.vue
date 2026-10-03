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
  border: 1px solid #d7e0e8;
  border-radius: 0.75rem;
  background: #fff;
  box-shadow: 0 0.75rem 2rem rgb(15 35 55 / 16%);
  color: #10243a;
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
  border: 1px solid #126c72;
  border-radius: 0.5rem;
  background: #126c72;
  color: #fff;
  font: inherit;
  cursor: pointer;
}

button.secondary {
  border-color: #cbd5df;
  background: #fff;
  color: #10243a;
}
</style>
