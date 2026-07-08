<script setup lang="ts">
const props = defineProps<{
  totalSent: number
  totalConfirmed: number
  totalErrors: number
  tps: number
  uptimeSecs: number
  activeWorkers: number
}>()

const uptime = computed(() => {
  const m = Math.floor(props.uptimeSecs / 60)
  const s = props.uptimeSecs % 60
  return `${m}m ${s.toString().padStart(2, '0')}s`
})

const errorRate = computed(() => {
  const total = props.totalConfirmed + props.totalErrors
  if (total === 0) return '—'
  return ((props.totalErrors / total) * 100).toFixed(2) + '%'
})

const progress = computed(() => {
  if (props.totalSent === 0) return 0
  return Math.min(100, (props.totalConfirmed / props.totalSent) * 100)
})

const progressBar = computed(() => {
  const p = progress.value
  const filled = Math.round(p / 10)
  const empty = 10 - filled
  return '[' + '█'.repeat(filled) + '░'.repeat(empty) + ']'
})
</script>

<template>
  <div class="grid grid-cols-7 gap-px bg-[#222] mb-4">
    <div class="bg-black p-2">
      <div class="text-[11px] text-gray-600 uppercase tracking-[0.05em]">sent</div>
      <div class="text-sm text-gray-200 font-mono tabular-nums mt-0.5">{{ totalSent.toLocaleString() }}</div>
    </div>
    <div class="bg-black p-2">
      <div class="text-[11px] text-gray-600 uppercase tracking-[0.05em]">confirmed</div>
      <div class="text-sm text-green-400 font-mono tabular-nums mt-0.5">{{ totalConfirmed.toLocaleString() }}</div>
    </div>
    <div class="bg-black p-2">
      <div class="text-[11px] text-gray-600 uppercase tracking-[0.05em]">errors</div>
      <div class="text-sm text-red-400 font-mono tabular-nums mt-0.5">
        {{ totalErrors.toLocaleString() }}
        <span class="text-gray-600 text-[11px] ml-1">{{ errorRate }}</span>
      </div>
    </div>
    <div class="bg-black p-2">
      <div class="text-[11px] text-gray-600 uppercase tracking-[0.05em]">tps</div>
      <div class="text-sm text-yellow-400 font-mono tabular-nums mt-0.5">{{ tps.toFixed(0) }}</div>
    </div>
    <div class="bg-black p-2">
      <div class="text-[11px] text-gray-600 uppercase tracking-[0.05em]">workers</div>
      <div class="text-sm text-gray-200 font-mono tabular-nums mt-0.5">{{ activeWorkers }}</div>
    </div>
    <div class="bg-black p-2">
      <div class="text-[11px] text-gray-600 uppercase tracking-[0.05em]">uptime</div>
      <div class="text-sm text-gray-200 font-mono tabular-nums mt-0.5">{{ uptime }}</div>
    </div>
    <div class="bg-black p-2">
      <div class="text-[11px] text-gray-600 uppercase tracking-[0.05em]">progress</div>
      <div class="font-mono text-gray-200 mt-0.5">
        <span class="text-xs">{{ progressBar }}</span>
        <span class="text-[11px] text-gray-500 ml-1">{{ progress.toFixed(0) }}%</span>
      </div>
    </div>
  </div>
</template>
