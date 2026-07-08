<script setup lang="ts">
import { Line } from 'vue-chartjs'

const props = defineProps<{
  data: { time: string; value: number }[]
}>()

const chartData = computed(() => ({
  labels: props.data.map(d => d.time),
  datasets: [
    {
      label: 'Confirmed',
      data: props.data.map(d => d.value),
      borderColor: '#22c55e',
      pointRadius: 0,
      borderWidth: 1,
    },
  ],
}))

const chartOptions = {
  responsive: true,
  maintainAspectRatio: false,
  animation: false,
  scales: {
    x: {
      display: true,
      grid: { color: 'rgba(255,255,255,0.03)' },
      ticks: { color: '#666', maxTicksLimit: 8, font: { size: 10 } },
    },
    y: {
      beginAtZero: true,
      grid: { color: 'rgba(255,255,255,0.03)' },
      ticks: { color: '#666', font: { size: 10 } },
    },
  },
  plugins: {
    legend: { display: false },
    tooltip: { enabled: false },
  },
}
</script>

<template>
  <div class="bg-black border border-[#222] p-2">
    <div class="text-[11px] text-gray-600 uppercase tracking-[0.05em] mb-1">cumulative (120s)</div>
    <div class="h-36">
      <Line v-if="data.length" :data="chartData" :options="chartOptions" />
      <div v-else class="flex items-center justify-center h-full text-gray-700 text-xs">—</div>
    </div>
  </div>
</template>
