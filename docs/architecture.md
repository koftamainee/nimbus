# Architecture

![Architecture diagram](../assets/architecture.png)

## Layers

Three layers: **Nimbus** (user-facing), **Quorum** (control plane), **Forge** (workers).

### Nimbus layer

User-facing tools that run on the admin machine.

| Binary | Role |
|---|---|
| `nimbusadm` | Cluster lifecycle manager — starts/stops all processes |
| `nimbusctl` | CLI for container management (run, ps, rm, scale) — talks to quorum-api via REST |
| `nimbus-registry` | HTTP image server — stores and serves container image tarballs |

### Quorum layer

Control plane. Three processes run on each master node. All persistent state lives in `quorum-db` (Raft-replicated KV).

| Binary | Role |
|---|---|
| `quorum-db` | From-scratch Raft consensus + linearizable KV storage (gRPC) |
| `quorum-api` | REST/HTTP proxy to quorum-db's KV service |
| `quorum-scheduler` | Leader-based reconciliation loop — watches specs and heartbeats, creates/deletes assignments |
| `qctl` | Low-level debug CLI for quorum-db KV operations |

### Forge layer

Workers. Each worker runs a privileged daemon and an orchestration agent.

| Binary | Role |
|---|---|
| `forged` | Privileged daemon (root) — manages containers via Linux namespaces, cgroups v2, pivot_root |
| `forge-agent` | Orchestration agent — watches assignments, calls forged, writes status to KV |
| `forge` | CLI for local interaction with forged (Unix socket gRPC) |

## Communication

| Connection | Protocol |
|---|---|
| nimbusctl → quorum-api | REST (HTTP/JSON) |
| quorum-db ↔ quorum-db (Raft) | gRPC |
| quorum-api → quorum-db | gRPC |
| quorum-scheduler → quorum-db | gRPC |
| forge-agent → quorum-db | gRPC |
| forge-agent → forged | gRPC (Unix socket) |
| forge → forged | gRPC (Unix socket) |
| forged → nimbus-registry | HTTP |

## Data flow

1. User runs `nimbusctl run` → REST call to `quorum-api` → writes container spec to `quorum-db` KV
2. `quorum-scheduler` watches `/containers/` → picks a node → writes assignment to `/nodes/<id>/assignments/`
3. `forge-agent` watches its assignments → calls `forged.Run()` → writes status back to KV
4. `forged` pulls image from `nimbus-registry`, creates cgroup, forks into namespaces
5. User runs `nimbusctl ps` → reads container statuses via `quorum-api` → `quorum-db`
