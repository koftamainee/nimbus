# qctl

Low-level debug CLI for quorum-db KV. Connects directly via gRPC.

## Usage

```
qctl --addr <addr> <command> [args]
```

## Flags

| Flag | Default | Description |
|---|---|---|
| `--addr` | `localhost:9090` | quorum-db gRPC address |

## Commands

### put

Write a key-value pair.

```
qctl put <key> <value>
```

Prints the revision number on success.

### get

Read a key or prefix.

```
qctl get [--prefix] <key>
```

Without `--prefix`: reads a single key. With `--prefix`: reads all keys with the given prefix.

Output format: `key = value (rev N)`. Exits with code 1 if key not found.

### del

Delete a key.

```
qctl del <key>
```

### watch

Stream changes for a key.

```
qctl watch <key> [--rev N]
```

Output: `<PUT|DELETE> key = value (rev N)`. Press Ctrl+C to stop.

Optional `--rev N` starts watching from a specific revision.

## Redirect handling

If connected to a non-leader, qctl prints `redirect to leader: <id>` and exits with code 1.
