<script setup lang="ts">
withDefaults(defineProps<{ name: string; size?: number }>(), { size: 20 })

const paths: Record<string, string[]> = {
  overview: ['M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h7v7h-7z'],
  monitor: ['M4 4h16a1 1 0 0 1 1 1v11a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V5a1 1 0 0 1 1-1Z', 'M8 21h8M12 17v4M7 9h3l2 4 2-6 2 3h2'],
  events: ['M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9M10 21h4'],
  analytics: ['M4 3v17h17M8 15v-4M13 15V7M18 15V4'],
  devices: ['M6 6h12v12H6zM9 9h6v6H9zM9 2v4M15 2v4M9 18v4M15 18v4M2 9h4M2 15h4M18 9h4M18 15h4'],
  settings: ['m9 3-1 3-3 1-2 3 2 2v2l-2 2 2 3 3 1 1 2h6l1-2 3-1 2-3-2-2v-2l2-2-2-3-3-1-1-3Z', 'M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0'],
  users: ['M16 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2M10 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8ZM20 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75'],
  arrow: ['M5 12h14m-5-5 5 5-5 5'],
  arrowUp: ['M7 17 17 7M7 7h10v10'],
  refresh: ['M20 7v5h-5M4 17v-5h5', 'M6 6a8 8 0 0 1 13 3M5 15a8 8 0 0 0 13 3'],
  plus: ['M12 5v14M5 12h14'],
  chevron: ['m9 5 7 7-7 7'],
  logout: ['M9 4H4v16h5M14 8l4 4-4 4M8 12h10'],
  shield: ['m12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6l8-3Z', 'm8 12 3 3 5-6'],
  check: ['m5 12 4 4L19 6'],
  camera: ['M4 6h12v12H4a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1Zm12 4 5-3v10l-5-3'],
  eye: ['M2 12s4-7 10-7 10 7 10 7-4 7-10 7S2 12 2 12Z', 'M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0'],
  eyeOff: ['m3 3 18 18M10.5 5.2 12 5c6 0 10 7 10 7a21 21 0 0 1-3 3.8M6 6.2A22 22 0 0 0 2 12s4 7 10 7c2 0 3.7-.6 5.2-1.6M10 10a3 3 0 0 0 4 4'],
  layers: ['m12 3 10 5-10 5L2 8l10-5ZM2 12l10 5 10-5M2 16l10 5 10-5'],
  calendar: ['M5 5h14a1 1 0 0 1 1 1v14H4V6a1 1 0 0 1 1-1ZM8 3v4M16 3v4M4 10h16M8 14h2M14 14h2'],
  book: ['M12 6C8 3 3 4 3 4v15s5-1 9 2c4-3 9-2 9-2V4s-5-1-9 2Zm0 0v15'],
  hand: ['M8 12V6a2 2 0 0 1 4 0v5-8a2 2 0 0 1 4 0v8-5a2 2 0 0 1 4 0v8c0 5-3 8-7 8-2 0-4-1-6-4l-4-5a2 2 0 0 1 3-3l2 2Z'],
  pen: ['m15 4 5 5M4 20l5-1L21 7a2 2 0 0 0-4-4L5 15l-1 5Z'],
  lock: ['M6 10h12v11H6zM8 10V6a4 4 0 0 1 8 0v4M12 14v3'],
  info: ['M22 12a10 10 0 1 1-20 0 10 10 0 0 1 20 0ZM12 11v6M12 7h.01'],
}
</script>

<template>
  <svg class="app-icon" :width="size" :height="size" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
    <path v-for="(path, index) in paths[name] ?? paths.info" :key="index" :d="path" />
  </svg>
</template>
