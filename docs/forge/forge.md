# forge (CLI)

CLI for interacting with a local `forged` daemon via Unix socket gRPC.

## Usage

```
forge <command> [args]
```

Connects to `/var/run/forge.sock` by default.

## Commands

### run

Run a container from an image.

```
forge run --image <img> --name <name> [--memory <limit>] [--cpus <n>] [--env K=V]... [-d] [-- cmd...]
```

| Flag | Description |
|---|---|
| `--image` | Image name or path |
| `--name` | Container name |
| `--memory` | Memory limit (e.g. `512m`, `1g`) |
| `--cpus` | CPU limit (e.g. `1.5`) |
| `--env` | Environment variable (repeatable) |
| `-d` / `--detach` | Run in background (no output on success) |

Trailing args after `--` are passed as the container command.

### ps

List containers.

```
forge ps [-a]
```

| Flag | Description |
|---|---|
| `-a` | Show all containers (default: only running) |

Output columns: `CONTAINER ID`, `NAME`, `STATUS`, `PID`, `CREATED`.

### stop

Stop a running container.

```
forge stop <name> [--timeout <sec>]
```

| Flag | Default | Description |
|---|---|---|
| `--timeout` | `10` | Grace period before SIGKILL |

### start

Resume a stopped container.

```
forge start <name>
```

### kill

Kill a container immediately (SIGKILL).

```
forge kill <name>
```

### logs

Show container logs.

```
forge logs <name> [--tail N] [-f]
```

| Flag | Description |
|---|---|
| `--tail` | Show last N lines |
| `-f` | Follow log output |

### rm

Remove a stopped container (cleans up cgroup and rootfs).

```
forge rm <name>
```
