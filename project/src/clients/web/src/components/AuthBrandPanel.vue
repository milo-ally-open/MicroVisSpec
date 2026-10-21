<script setup lang="ts">
import { RouterLink } from 'vue-router'
import AppIcon from './AppIcon.vue'
import BrandLockup from './BrandLockup.vue'
</script>

<template>
  <section class="auth-aside">
    <RouterLink class="brand brand--light" to="/" aria-label="云哨首页">
      <BrandLockup inverse />
    </RouterLink>
    <div class="auth-aside__copy">
      <span class="eyebrow eyebrow--light">云边协同 · 智能感知</span>
      <h1>让现场可见，<br>让响应有据。</h1>
      <p>连接边缘设备与现场数据，<br>在一个工作台掌握运行状态。</p>
      <div class="auth-flow" aria-label="从现场感知到事件处置">
        <div><AppIcon name="camera" :size="28" /><strong>现场感知</strong><small>边缘设备与视频</small></div>
        <span class="auth-flow__connector"></span>
        <div><AppIcon name="events" :size="28" /><strong>异常发现</strong><small>规则触发与证据</small></div>
        <span class="auth-flow__connector"></span>
        <div><AppIcon name="shield" :size="28" /><strong>协同处置</strong><small>跟进每一个事件</small></div>
      </div>
    </div>
    <div class="auth-aside__foot"><AppIcon name="shield" :size="18" />个人工作空间 · 数据按账号隔离</div>
  </section>
</template>
