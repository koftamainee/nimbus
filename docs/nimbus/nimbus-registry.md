# nimbus-registry

HTTP image registry. Stores and serves container image tarballs.

## Usage

```
nimbus-registry --addr <addr> --image-dir <dir>
```

## Flags

| Flag | Default | Description |
|---|---|---|
| `--addr` | `:9091` | Listen address |
| `--image-dir` | `./images` | Directory for image storage |

## API

### GET /images/{name}

Download an image tarball.

```
curl http://localhost:11111/images/forge-test-python -o forge-test-python.tar
```

### PUT /images/{name}

Upload an image tarball.

```
curl -X PUT http://localhost:11111/images/forge-test-python --data-binary @forge-test-python.tar
```

Returns JSON with `name`, `path`, and `size`.
