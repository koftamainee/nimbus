# quorum-db

From-scratch Raft consensus implementation providing linearizable KV storage. One instance per master node.

## Usage

```
quorum-db --id <id> --addr <addr> --initial-cluster <id=addr,...> --data-dir <dir>
```

## Flags

| Flag | Description |
|---|---|
| `--id` | Node identifier (e.g. `node1`) |
| `--addr` | Listen address (e.g. `:10101`) |
| `--initial-cluster` | Comma-separated `id=addr` pairs for all nodes (including self) |
| `--data-dir` | Data directory for WAL |

## gRPC services

### RaftService

Internal consensus protocol. Used between quorum-db nodes only.

- `RequestVote` — election phase
- `AppendEntries` — log replication + heartbeats

### KVService

Data operations. Used by quorum-api, quorum-scheduler, forge-agent.

- `Put` — write a key-value pair
- `Get` — read a single key
- `Delete` — delete a key
- `Txn` — transactional read-write
- `Range` — key-range scan (supports prefix queries)

### WatchService

Streaming key change notifications.

- `Watch` — stream changes for a key or prefix

## Leader redirection

If a node is not the leader, write operations return `codes.Unavailable` with message `leader: <id>`. Clients should retry on the indicated leader.
