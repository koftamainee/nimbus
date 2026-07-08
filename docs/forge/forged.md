# forged

Privileged daemon that manages containers on a worker node. Requires root. Listens on a Unix socket and implements the `ForgeRuntime` gRPC service.

## Usage

```
sudo forged --data-dir <dir> --socket <path> --registry-addr <url>
```

## Flags

| Flag | Default | Description |
|---|---|---|
| `--data-dir` | `/var/lib/forge` | Data directory (containers, images, logs) |
| `--socket` | `/var/run/forge.sock` | Unix socket path |
| `--registry-addr` | `http://127.0.0.1:11111` | nimbus-registry URL for pulling images |

## gRPC methods (ForgeRuntime)

### Run

Extract image tarball, create cgroup, fork into namespaces (PID, mount, IPC, UTS), pivot_root into the container rootfs.

```
RunRequest {
    image: string          // image name in registry
    name: string           // container name
    memory: string         // memory limit (e.g. "512m", "1g")
    cpus: double           // CPU limit
    env: repeated string   // environment variables (KEY=value)
    cmd: repeated string   // command arguments
}
```

Returns `RunResponse { name }`.

### Start

Resume a stopped container.

```
StartRequest { name: string }
```

Returns `StartResponse { name }`.

### Stop

Send SIGTERM to the container process, wait for the timeout, then send SIGKILL if still running.

```
StopRequest {
    name: string
    timeout: int32        // grace period in seconds
}
```

### Kill

Send SIGKILL immediately.

```
KillRequest { name: string }
```

### Remove

Clean up cgroup and remove container rootfs.

```
RemoveRequest { name: string }
```

### Inspect

Return container state, PID, exit code, and timestamps.

```
InspectRequest { name: string }
```

Returns `InspectResponse { name, status, pid, exit_code, created_at, started_at, finished_at, image }`.

### List

List all containers (optionally including stopped ones).

```
ListRequest { all: bool }
```

Returns `ListResponse { containers: repeated InspectResponse }`.

### Logs

Stream stdout/stderr logs (JSON-per-line format).

```
LogsRequest {
    name: string
    tail: int32           // number of recent lines (0 = all)
}
```

Returns `LogsResponse { stdout, stderr }`.

## Container lifecycle

1. **Run** — image is pulled from registry, extracted to `{data_dir}/containers/{name}/rootfs/`, cgroup created, process forked into namespaces with pivot_root
2. **Running** — container stdout/stderr are logged as JSON-per-line to `{data_dir}/containers/{name}/log`
3. **Stop/Kill** — signals are sent to the container PID; on Stop, SIGTERM is followed by SIGKILL after timeout
4. **Remove** — cgroup is cleaned up, rootfs is deleted
