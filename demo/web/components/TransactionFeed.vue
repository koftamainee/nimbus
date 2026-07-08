<script setup lang="ts">
defineProps<{
  transactions: { id: string; debit_account: string; credit_account: string; amount: number }[]
}>()
</script>

<template>
  <div class="bg-gray-800 rounded-xl p-4 border border-gray-700">
    <div class="text-sm text-gray-400 mb-2">📝 Live Transaction Feed</div>
    <div class="h-48 overflow-y-auto space-y-1">
      <div v-if="transactions.length === 0" class="text-gray-500 text-sm text-center py-8">
        Waiting for transactions...
      </div>
      <div
        v-for="tx in transactions.slice(-20).reverse()"
        :key="tx.id"
        class="text-xs font-mono text-gray-300 bg-gray-750 px-2 py-1 rounded"
      >
        <span class="text-green-400">✅</span>
        {{ tx.id }}
        <span class="text-yellow-400">{{ tx.debit_account }}</span>
        →
        <span class="text-blue-400">{{ tx.credit_account }}</span>
        <span class="text-purple-400">${{ tx.amount }}</span>
      </div>
    </div>
  </div>
</template>
