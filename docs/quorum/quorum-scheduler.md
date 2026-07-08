# quorum-scheduler

Leader-based reconciliation loop. Runs on every master but only active on the Raft leader (via `/internal/scheduler/lease` key).

## Usage

```
quorum-scheduler --db-addr <addr> --id <id>
```

## Flags

| Flag | Description |
|---|---|
| `--db-addr` | Local quorum-db address (e.g. `:10101`) |
| `--id` | Node id (e.g. `node1`) |

## How it works

### Event sources

1. Watch `/containers/` — spec changes trigger reconcile
2. Watch `/nodes/` — heartbeats stream in real time (via NodeManager)
3. Fallback ticker every 30s

### Reconcile loop

1. Read desired state (`Range /containers/`)
2. Compare with current assignments (`Range /nodes/<id>/assignments/<name>`)
3. If replicas < desired → create assignment on least-loaded alive node (anti-affinity: prefers nodes not already hosting a replica of the same container)
4. If replicas > desired → remove excess (highest index first)
5. Reschedule containers from dead nodes

### NodeManager

Embedded component that monitors node liveness:

- Watches `/nodes/` for heartbeat updates
- A node is considered dead if heartbeat is older than 10s
- Dead node check runs every 10s
