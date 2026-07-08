<script setup lang="ts">
const {
  stats,
  tpsHistory,
  cumulativeHistory,
  eventLog,
  sseConnected,
  lastUpdated,
  connectSSE,
  startGeneration,
  stopGeneration,
} = useStats()

const generating = ref(false)

onMounted(() => {
  connectSSE()
})

const handleStart = async () => {
  generating.value = true
  await startGeneration(20000, 200)
}

const handleStop = async () => {
  generating.value = false
  await stopGeneration()
}

watch(() => stats.value.total_confirmed, (val, old) => {
  if (val > 0 && val === stats.value.total_sent && old > 0) {
    generating.value = false
  }
})
</script>

<template>
  <div class="min-h-screen p-3">
    <div class="flex items-center justify-between mb-3">
      <div class="flex items-center gap-3">
        <span class="text-sm text-gray-200 font-mono">nimbus/demo</span>
        <span
          class="inline-block w-2 h-2"
          :class="sseConnected ? 'bg-green-500' : 'bg-red-500'"
        />
        <span class="text-[11px] text-gray-600 font-mono">
          {{ sseConnected ? 'connected' : 'disconnected' }}
        </span>
        <span v-if="lastUpdated" class="text-[11px] text-gray-700 font-mono">
          last: {{ lastUpdated }}
        </span>
      </div>
      <div class="flex gap-px">
        <button
          v-if="!generating"
          @click="handleStart"
          class="px-3 py-1.5 bg-green-900 hover:bg-green-800 text-green-300 text-xs font-mono transition-colors"
        >
          start
        </button>
        <button
          v-else
          @click="handleStop"
          class="px-3 py-1.5 bg-red-900 hover:bg-red-800 text-red-300 text-xs font-mono transition-colors"
        >
          stop
        </button>
      </div>
    </div>

    <StatsCards
      :total-sent="stats.total_sent"
      :total-confirmed="stats.total_confirmed"
      :total-errors="stats.total_errors"
      :tps="stats.tps"
      :uptime-secs="stats.uptime_secs"
      :active-workers="stats.active_workers"
    />

    <div class="grid grid-cols-2 gap-px mb-4">
      <TpsChart :data="tpsHistory" />
      <CumulativeChart :data="cumulativeHistory" />
    </div>

    <div class="bg-black border border-[#222] p-2">
      <div class="text-[11px] text-gray-600 uppercase tracking-[0.05em] mb-1">events</div>
      <div class="h-24 overflow-y-auto font-mono text-[11px] leading-[1.6]">
        <div v-if="eventLog.length === 0" class="text-gray-700">—</div>
        <div
          v-for="(line, i) in eventLog.slice(0, 50)"
          :key="i"
          class="text-gray-600"
        >{{ line }}</div>
      </div>
    </div>
  </div>
</template>
