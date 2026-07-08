# quorum-api

REST/HTTP proxy to quorum-db's KV service. Translates HTTP requests to gRPC calls.

## Usage

```
quorum-api --db-cluster <id=addr,...> --rest-addr <addr>
```

## Flags

| Flag | Description |
|---|---|
| `--db-cluster` | Comma-separated `id=addr` pairs for quorum-db nodes |
| `--rest-addr` | REST API listen address (default `:9096`) |

## REST API

All endpoints proxy to quorum-db's KV service.

### Containers

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/v1/containers` | Create a container spec |
| `GET` | `/api/v1/containers` | List all container specs and statuses |
| `GET` | `/api/v1/containers/{name}` | Get a single container spec |
| `DELETE` | `/api/v1/containers/{name}` | Remove a container spec |
| `POST` | `/api/v1/containers/{name}/stop` | Stop all replicas |
| `POST` | `/api/v1/containers/{name}/scale` | Change replica count |

### Nodes

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/nodes` | List registered worker nodes |

## Leader redirection

If the local quorum-db is not the Raft leader, returns HTTP 503 with body `{"leader": "<id>"}`.
