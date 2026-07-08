# nimbusctl

CLI for container management. Communicates with `quorum-api` via REST (HTTP/JSON).

## Usage

```
nimbusctl --addr http://localhost:9096 <command> [args]
```

## Global flags

| Flag | Default | Description |
|---|---|---|
| `--addr` | `http://localhost:9096` | quorum-api REST address |

## Commands

### run

Create a container spec.

```
nimbusctl run <name> --image <img> [--replicas N] [--mem <mb>] [--cpus <n>] [--env K=V]... [--cmd arg]...
```

| Flag | Description |
|---|---|
| `--image` | Image name (must exist in nimbus-registry) |
| `--replicas` | Number of replicas (default 1) |
| `--mem` | Memory limit (e.g. `512m`, `1g`) |
| `--cpus` | CPU limit (e.g. `1.5`) |
| `--env` | Environment variable in `KEY=value` format (repeatable) |
| `--cmd` | Command arguments (repeatable) |

Also picks up `NIMBUS_ENV_*` environment variables from the shell.

### ps

List containers and their status across all nodes.

```
nimbusctl ps
```

Output columns: `NAME`, `NODE`, `STATUS`.

### rm

Remove one or more container specs. Stops and removes all replicas.

```
nimbusctl rm <name>...
```

### stop

Stop all replicas of a container.

```
nimbusctl stop <name>...
```

### nodes

List registered worker nodes.

```
nimbusctl nodes
```

Output columns: `NODE ID`, `HOSTNAME`.

### scale

Set replica count for a container.

```
nimbusctl scale <name> [--replicas N]
```

## Redirect handling

If quorum-api is not the Raft leader, it returns HTTP 503 with `{"leader": "<id>"}`. nimbusctl prints the leader address and exits with code 1.
