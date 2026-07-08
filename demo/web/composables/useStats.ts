interface Stats {
  total_sent: number
  total_confirmed: number
  total_errors: number
  tps: number
  active_workers: number
  started_at: string
  uptime_secs: number
}

interface TxEvent {
  id: string
  debit_account: string
  credit_account: string
  amount: number
  currency: string
}

let source: EventSource | null = null

export const useStats = () => {
  const stats = ref<Stats>({
    total_sent: 0,
    total_confirmed: 0,
    total_errors: 0,
    tps: 0,
    active_workers: 0,
    started_at: '',
    uptime_secs: 0,
  })

  const tpsHistory = ref<{ time: string; value: number }[]>([])
  const cumulativeHistory = ref<{ time: string; value: number }[]>([])
  const eventLog = ref<string[]>([])
  const sseConnected = ref(false)
  const lastUpdated = ref('')

  const statsUrl = 'http://localhost:9090'

  const log = (msg: string) => {
    const t = new Date().toLocaleTimeString()
    eventLog.value.unshift(`[${t}] ${msg}`)
    if (eventLog.value.length > 100) eventLog.value.pop()
  }

  const connectSSE = () => {
    if (source) source.close()
    source = new EventSource(`${statsUrl}/api/events`)

    source.onopen = () => {
      sseConnected.value = true
      log('sse connected')
    }

    source.addEventListener('stats', (event: MessageEvent) => {
      const data = JSON.parse(event.data) as Stats
      stats.value = data
      lastUpdated.value = new Date().toLocaleTimeString()
      const now = new Date().toLocaleTimeString()

      tpsHistory.value.push({ time: now, value: data.tps })
      if (tpsHistory.value.length > 60) tpsHistory.value.shift()

      cumulativeHistory.value.push({ time: now, value: data.total_confirmed })
      if (cumulativeHistory.value.length > 120) cumulativeHistory.value.shift()
    })

    source.onerror = () => {
      sseConnected.value = false
    }
  }

  const startGeneration = async (count = 10000, speed = 100) => {
    log(`start count=${count} speed=${speed}`)
    try {
      await fetch(`${statsUrl}/start?count=${count}&speed=${speed}`, {
        method: 'POST',
      })
    } catch {
      log('start failed')
    }
  }

  const stopGeneration = async () => {
    log('stop')
    try {
      await fetch(`${statsUrl}/stop`, { method: 'POST' })
    } catch {
      log('stop failed')
    }
  }

  return {
    stats,
    tpsHistory,
    cumulativeHistory,
    eventLog,
    sseConnected,
    lastUpdated,
    connectSSE,
    startGeneration,
    stopGeneration,
  }
}
