# forge-agent

Orchestration agent on each worker node. No root privileges needed — delegates all runtime operations to `forged` via Unix socket gRPC.

## Usage

```
forge-agent --node-id <id> --db-cluster <id=addr,...> [--forge-socket <path>]
```

## Flags

| Flag | Default | Description |
|---|---|---|
| `--node-id` | (required) | Worker node identifier (e.g. `worker1`) |
| `--db-cluster` | (required) | Comma-separated `id=addr` pairs for quorum-db |
| `--forge-socket` | `/var/run/forge.sock` | forged Unix socket path |

## How it works

### Startup

1. Connects to quorum-db (follows leader redirects)
2. Registers node: `Put /nodes/<id>` with hostname info
3. Starts heartbeat loop in a background task

### Heartbeat loop

Every 5 seconds: `Put /nodes/<id>/heartbeat` with current timestamp.

### Assignment watch

Watches `/nodes/<id>/assignments/` for changes:

- **PUT** (assignment created):
  1. Parse the assignment JSON (contains `container_name` and `spec`)
  2. Call `forged.Run()` with the spec
  3. Write status to `/containers/{container_name}/status` in KV:
     - On success: `{"status": "running", "node_id": "<id>"}`
     - On failure: `{"status": "error", "error": "<message>", "node_id": "<id>"}`

- **DELETE** (assignment removed):
  1. Call `forged.Stop()` (SIGTERM → 10s → SIGKILL) via `tokio::spawn`
  2. Call `forged.Remove()`
  3. Delete status from KV at `/containers/{container_name}/status`

### Reconnection

If the KV watch stream disconnects or the Raft leader changes, forge-agent reconnects to the cluster and resumes watching.
