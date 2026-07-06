# ☁️ Nimbus


[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

**Nimbus** is a distributed container orchestrator: **Quorum** — control plane on Go with a from-scratch Raft consensus implementation, and **Forge** — container runtime on Rust with direct Linux namespaces/cgroups work.

Does not aim to replace Kubernetes — it's a deliberately trimmed but honestly built system where every line of code is written and understood by the author, not taken from ready-made consensus libraries or container runtimes.

## 🧩 Components

### Quorum (`/quorum`, Go)

Control plane and distributed KV store for cluster state, built on a from-scratch Raft consensus implementation.

| Feature                   | Description                                                                                                                              |
|---------------------------|------------------------------------------------------------------------------------------------------------------------------------------|
| **Raft from scratch**     | Leader election, log replication, safety guarantees                                                                                      |
| **Persistence**           | Write-ahead log (WAL) with snapshots for durability, crash recovery via replay                                                           |
| **gRPC API**              | Accepts commands from clients, serves desired state to Forge agents on each node                                                         |
| **KV store**              | In-memory map with monotonically increasing revisions, prefix range queries, conditional transactions (compare-and-swap), event streaming |
| **Linearizable reads**    | ReadIndex mechanism ensures reads reflect the latest committed state                                                                     |
| **Round-robin scheduler** | Container placement across nodes                                  |
| **Reconciliation loop**   | Compares desired state with actual state auto-replans on node failure                                            |

### Forge (`/forge`, Rust) — 🚧 WIP

Container runtime and node agent, running on every worker machine.

| Feature | Description |
|---------|-------------|
| **Container isolation** | Creates PID/mount/UTS namespaces, sets cgroups v2 limits, unpacks image (tar + manifest) and runs the process inside an isolated environment |
| **Overlay filesystem** | Layered image storage — second phase; flat `pivot_root` without layers is acceptable at first |

---

## 🚀 Quick start

### Requirements

- **Go 1.26+** (for Quorum)
- **Rust** (for Forge — Planned)
- Linux (namespaces/cgroups require Linux kernel)

### Build & install

```bash
# Clone
git clone https://github.com/koftamainee/nimbus.git
cd nimbus

# Build quorum + qctl binaries to ~/.local/bin/
./scripts/install_quorum.sh
```

Or manually:

```bash
cd quorum
go build -o ~/.local/bin/quorum ./cmd/quorum
go build -o ~/.local/bin/qctl   ./cmd/qctl
```

### Run a single node

```bash
quorum \
  --id node0 \
  --addr :9090 \
  --initial-cluster "node0=127.0.0.1:9090" \
  --data-dir ./data
```

# Run a multi-node-cluster
```bash
# node0
quorum --id node0 --addr :9090 \
  --initial-cluster "node0=127.0.0.1:9090,node1=127.0.0.1:9091,node2=127.0.0.1:9092" \
  --data-dir ./data/node0

# node1
quorum --id node1 --addr :9091 \
  --initial-cluster "node0=127.0.0.1:9090,node1=127.0.0.1:9091,node2=127.0.0.1:9092" \
  --data-dir ./data/node1

# node2
quorum --id node2 --addr :9092 \
  --initial-cluster "node0=127.0.0.1:9090,node1=127.0.0.1:9091,node2=127.0.0.1:9092" \
  --data-dir ./data/node2
```

### Run a 3-node cluster
You can use custom script for development / quick testing. It will run and manage N nodes cluster (3 by default). All data put in /tmp
```bash
./scripts/cluster.sh start 5

# Or open each node in a separate terminal tabs:
./scripts/cluster.sh start -t

# Check status:
./scripts/cluster.sh status

# Stop all nodes:
./scripts/cluster.sh stop

# Clean data directories:
./scripts/cluster.sh clean
```

### Use the CLI

```bash
# Write a key
qctl --addr :9090 put mykey myvalue

# Read a key
qctl --addr :9090 get mykey

# Read with prefix scan
qctl --addr :9090 get --prefix myprefix

# Delete a key
qctl --addr :9090 del mykey

# Watch for changes
qctl --addr :9090 watch mykey --rev 1
```

NOTE: you should specify address of note, that is currently Leader. If you using `scripts/cluster.sh` it will tell you current Leader node, otherwise you can check logs or try random node address: qctl will provide a hint to current Leader.

---

## 🛠️ CLI Reference

### `qctl` — client

| Command | Description |
|---------|-------------|
| `qctl put <key> <value>` | Write a key-value pair |
| `qctl get [--prefix] <key>` | Read a key (with optional prefix scan) |
| `qctl del <key>` | Delete a key |
| `qctl watch <key> [--rev N]` | Watch for changes (server-streaming) |

NOTE: revision 0 (default) in `qctl watch` means to just watch in realtime, no previous revisions will be displayed

---

### `quorum` — server

```bash
quorum --id <id> --addr <addr> --initial-cluster <id=addr,...> [--data-dir <dir>]
```

| Flag | Required | Default | Description |
|------|----------|---------|-------------|
| `--id` | Yes | — | Unique node identifier (e.g., `node0`) |
| `--addr` | Yes | — | Listen address (e.g., `:9090`) |
| `--initial-cluster` | Yes | — | Comma-separated `id=addr` pairs for all nodes |
| `--data-dir` | No | `./data` | Directory for WAL and data files |

---

## 🗺️ Roadmap

Each stage delivers a self-contained, presentable result — even if work stops after it, there's something to show.

- [x] **Stage 1** — Quorum as a standalone distributed KV store with Raft consensus, without containers
- [ ] **Stage 2** — Forge as a standalone CLI: `forge run --image foo.tar --memory 512m`, works locally without orchestration
- [ ] **Stage 3** — Forge connects to Quorum: Quorum stores desired state, Forge polls it and starts/stops containers (naive scheduler)
- [ ] **Stage 4** — Reconciliation loop: self-healing on node failure, automatic container replanning
- [ ] **Stage 5** — OverlayFS for image layers in Forge, network namespace + simple bridge for inter-container networking on a single node

---

## 🚫 Conscious out of scope

To keep the project completable and not turn into an endless attempt to replicate Kubernetes entirely, the following are deliberately excluded:

- Pod-to-pod networking (CNI) — containers run in host network
- RBAC, admission controllers, webhooks, API versioning
- Rolling updates, autoscaling, priority classes, affinity/taints
- Persistent volumes, full storage orchestration
- Full OCI image compatibility — using a simplified custom format
- Web UI — interaction via CLI/gRPC client

---

## 🤝 Contributing

Pull requests are welcome!

## 📄 License

Apache 2.0 — see [LICENSE](LICENSE) file.
