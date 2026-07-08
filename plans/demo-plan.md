# Financial Transactions Demo Plan

## Цель

Показать работу Nimbus как оркестратора: деплой контейнеров, масштабирование, наблюдаемость. Демо симулирует обработку финансовых транзакций с использованием TigerBeetle (БД), NATS (очередь) и пула воркеров. Фронтенд с графиками для наглядности.

## 🏗️ Архитектура

```
Host (одна машина, containers share host network — нет CLONE_NEWNET)

┌──────────┐   ┌──────────┐   ┌────────────────┐   ┌───────────────┐
│ tiger-   │   │  nats-   │   │  job-service   │   │  web (host)    │
│ beetle   │   │  server  │   │  :9090 HTTP    │   │  :3333 dev     │
│ :3000    │   │  :4222   │   │  + SSE /stats  │   │  Vue+Nuxt 3    │
└────┬─────┘   └────┬─────┘   └───────┬────────┘   └───────┬───────┘
     │              │                  │                    │
     │              │   ┌──────────┐   │                    │
     │              │   │worker:1  │   │◄─── polling ───────┘
     │              │   │(Rust)    │   │
     │              │   │NATS sub  │   │
     │◄─────────────┼───┤TB client │   │
     │              │   └──────────┘   │
     │              │   ┌──────────┐   │
     │              │   │worker:N  │   │
     │              │   └──────────┘   │
     ▼              ▼                  ▼
  stateful       message queue     txs sent/confirmed
  (rootfs)                          stats endpoint
```

**Почему работает:** `isolation.rs:203` не использует `CLONE_NEWNET` → все контейнеры в host network, общаются через `localhost`. Воркеры не слушают порты → реплицируются без конфликтов.

## 📁 Структура файлов

```
demo/
├── job-service/
│   ├── main.go              # HTTP :9090 + SSE + stats
│   ├── handler_stats.go     # /stats, /api/transactions, /api/events (SSE)
│   ├── handler_control.go   # POST /start, POST /stop
│   ├── go.mod
│   └── go.sum
├── worker/
│   ├── Cargo.toml
│   └── src/
│       └── main.rs          # NATS sub → TigerBeetle transfer
├── web/                     # Vue+Nuxt 3 frontend
│   ├── pages/
│   │   └── index.vue        # Дашборд
│   ├── components/
│   │   ├── TpsChart.vue            # TPS line chart (60s window)
│   │   ├── CumulativeChart.vue     # Cumulative tx chart
│   │   ├── StatsCards.vue          # Total, confirmed, errors, TPS
│   │   └── TransactionFeed.vue     # Live scrolling feed
│   ├── composables/
│   │   └── useStats.ts     # SSE + reactive state management
│   ├── app.vue
│   ├── nuxt.config.ts
│   └── package.json
├── images/
│   ├── build.sh             # Сборка всех container images
│   ├── Dockerfile.job-service
│   ├── Dockerfile.worker
│   ├── Dockerfile.tigerbeetle
│   └── Dockerfile.nats
└── demo.sh                  # Пошаговый сценарий
```

## 📡 Компоненты

| Компонент | Язык | Порт | Реплицируется | Описание |
|-----------|------|------|---------------|----------|
| **tigerbeetle** | Zig (binary) | 3000 | ❌ | БД двойной записи |
| **nats-server** | Go (binary) | 4222 | ❌ | Message queue |
| **job-service** | Go | 9090 | ❌ | Генерация транзакций + REST API + SSE |
| **worker** | Rust | — | ✅ | Обработка транзакций |
| **web** | Vue+Nuxt 3 | 3333 | ❌ (host) | Дашборд с графиками |

## 🔄 Data Flow

1. `job-service` генерирует транзакции:
   ```json
   {"id":"tx-001","debit_account":"acc:001","credit_account":"acc:002","amount":100,"currency":"USD"}
   ```
2. Публикует в NATS topic `tx.pending`
3. `worker` подписан на `tx.pending`, получает задачу
4. Worker создаёт transfer в TigerBeetle
5. Worker публикует результат в NATS `tx.completed`
6. `job-service` слушает `tx.completed`, обновляет stats
7. Фронтенд получает stats через SSE с `job-service:9090/api/events`
8. Chart.js рисует графики в реальном времени

## 📊 Фронтенд (web/)

**Дашборд (одна страница `index.vue`):**

```
┌────────────────────────────────────────────────────────┐
│  ⚡ Nimbus — Financial Transactions Demo               │
│  Total: 5,000  |  Confirmed: 4,998  |  TPS: 142       │
│  Workers: 5 🟢 |  Errors: 2          │  Uptime: 34s   │
├──────────────────────┬───────────────────────────────┤
│  📈 TPS over time    │  📈 Cumulative transactions   │
│  (line chart, 60s)   │  (line chart)                  │
├──────────────────────┴───────────────────────────────┤
│  Live Feed                                            │
│  ✅ tx-042  acc:001 → acc:002   $100   ⏱ 2ms        │
│  ✅ tx-043  acc:003 → acc:001   $250   ⏱ 1ms        │
│  ❌ tx-044  acc:999 → acc:002   FAIL   invalid_acc   │
│  ... auto-scrolling                                   │
└────────────────────────────────────────────────────────┘
```

**Real-time:** SSE (`/api/events`) вместо polling. EventSource в браузере — встроенная поддержка, автопереподключение при разрыве.

## 🐳 Container Images

Каждый образ — tar архив с rootfs + `manifest.toml`:

```
tigerbeetle.tar/
├── manifest.toml      # entrypoint = ["/init.sh"]
├── tigerbeetle         # static binary (Zig)
└── init.sh             # format → start

nats-server.tar/
├── manifest.toml      # entrypoint = ["/nats-server"]
└── nats-server         # static binary (Go, CGO_ENABLED=0)

job-service.tar/
├── manifest.toml      # entrypoint = ["/job-service"]
└── job-service         # static binary (Go, CGO_ENABLED=0)

worker.tar/
├── manifest.toml      # entrypoint = ["/worker"]
└── worker              # static binary (Rust, musl target)
```

## 📋 Порядок реализации

1. **job-service** (Go) — основа, от него зависит SSE
2. **worker** (Rust) — NATS + TigerBeetle client
3. **Container images** — сборка всех образов
4. **Frontend** (Vue+Nuxt) — дашборд с графиками
5. **Demo script** — пошаговый сценарий

## 🎬 Сценарий демки (~7-10 мин)

```bash
# 1. Build images
cd demo/images && ./build.sh

# 2. Upload to registry
for img in tigerbeetle nats-server job-service worker; do
  curl -X PUT "http://localhost:9091/images/$img" --data-binary @"$img.tar"
done

# 3. Start cluster
nimbusadm up

# 4. Deploy infrastructure
nimbusctl run tigerbeetle --image tigerbeetle
nimbusctl run nats-server --image nats-server
nimbusctl run job-service --image job-service --env NATS_ADDR=localhost:4222

# 5. Deploy 1 worker
nimbusctl run worker --image worker --replicas 1
nimbusctl ps

# 6. Start frontend (on host)
cd demo/web && npm run dev -- --port 3333 &
# Open http://localhost:3333

# 7. Generate transactions
curl -X POST localhost:9090/start?count=20000

# 8. Observe: 1 worker, ~50 TPS
# 9. Scale to 10 workers
nimbusctl scale worker --replicas 10
nimbusctl ps

# 10. Observe: 10 workers, ~500 TPS
# 11. Scale down
nimbusctl scale worker --replicas 2

# 12. Clean up
nimbusctl scale worker --replicas 0
nimbusctl stop tigerbeetle nats-server job-service
```

## ⚠️ Технические заметки

**TigerBeetle:**
- `init.sh` внутри контейнера делает `tigerbeetle format --cluster=0 --replica=0 /data/db.tb` перед `start`
- Данные живут в rootfs → при `rm` теряются. Для демки ок

**Rust worker + TigerBeetle:**
- TigerBeetle Rust client требует `libtb_client.a` — нужен на этапе линковки
- Варианты: (1) официальный `tb_client` crate + .a файл, (2) чистый Rust client через TCP

**Фронтенд:**
- Работает на хосте (`npm run dev`), не в контейнере
- Обращается к `job-service` по `localhost:9090`
- job-service отдаёт `Access-Control-Allow-Origin: *`

**Порты (host network):**
| Сервис | Порт |
|--------|------|
| TigerBeetle | 3000 |
| NATS | 4222 |
| job-service | 9090 |
| Frontend | 3333 |
| quorum REST API | 9096 |
| nimbus-registry | 9091 |
