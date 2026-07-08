# nimbusadm

Cluster lifecycle manager. Reads a YAML config, starts all cluster processes with `sudo`, and cleans them up on Ctrl+C.

## Usage

```
nimbusadm -f nimbus.yaml
```

Only the `start` command (default). Press Ctrl+C to stop all processes gracefully (SIGTERM → 10s timeout → SIGKILL).

## Flags

| Flag | Default | Description |
|---|---|---|
| `-f` | `nimbus.yaml` | Config file path |

## Config format (`nimbus.yaml`)

```yaml
binary_dir: ./build
data_dir: ./build/data

masters:
  - id: node1
    db_addr: :10101
    api_addr: :10102
  - id: node2
    db_addr: :10201
    api_addr: :10202
  - id: node3
    db_addr: :10301
    api_addr: :10302

worker_count: 3

registry:
  addr: :11111

forged:
  socket: /var/run/forge.sock
  registry_addr: http://127.0.0.1:11111
```

### Fields

| Field | Description |
|---|---|
| `binary_dir` | Directory containing compiled binaries |
| `data_dir` | Runtime data directory (logs, WAL, images, container rootfs) |
| `masters` | List of master nodes with their IDs and addrs |
| `worker_count` | Number of workers (auto-generates worker1, worker2, …) |
| `workers` | Explicit worker list (alternative to `worker_count`) |
| `registry.addr` | nimbus-registry listen address |
| `forged.socket` | forged Unix socket path |
| `forged.registry_addr` | Registry URL for forged to pull images |

## What it starts

For each master node:
- `quorum-db` — with `--id`, `--addr`, `--initial-cluster`, `--data-dir`
- `quorum-api` — with `--db-cluster`, `--rest-addr`
- `quorum-scheduler` — with `--db-addr`, `--id`

Once per cluster:
- `nimbus-registry` — with `--addr`, `--image-dir`
- `forged` — with `--data-dir`, `--registry-addr`

For each worker:
- `forge-agent` — with `--node-id`, `--db-cluster`

## Process management

- Each process gets its own log file in `{data_dir}/logs/{name}.log`
- PID files are written to `{data_dir}/run/{name}.pid`
- On shutdown, processes are killed in reverse order (SIGTERM to process groups)
- If processes don't exit within 10 seconds, SIGKILL is sent
