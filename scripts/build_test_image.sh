#!/usr/bin/env bash
set -euo pipefail

IMAGE_NAME="forge-test-python"
OUTPUT_TAR="${1:-${IMAGE_NAME}.tar}"

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

echo "creating test image: $OUTPUT_TAR"

echo "building minimal python rootfs..."
docker build --platform=linux/amd64 -t forge-test-python-builder -f - . <<'DOCKERFILE' 2>&1 | tail -3
FROM alpine:3.21
RUN apk add --no-cache python3
DOCKERFILE

mkdir -p "$TMPDIR/rootfs"
CID=$(docker create --platform=linux/amd64 forge-test-python-builder)
docker export "$CID" | tar -C "$TMPDIR/rootfs" -xf -
docker rm "$CID" > /dev/null

cat > "$TMPDIR/manifest.toml" <<'EOF'
entrypoint = ["python3", "-m", "http.server"]
cmd = ["8000"]
env = ["PATH=/usr/bin:/bin:/usr/sbin:/sbin", "PYTHONUNBUFFERED=1"]
workdir = "/data"
EOF

mkdir -p "$TMPDIR/rootfs/data"
cat > "$TMPDIR/rootfs/data/index.html" <<'EOF'
<!DOCTYPE html>
<html>
<head><title>Forge Test Server</title></head>
<body>
<h1>Hello from Forge!</h1>
<p>This is a test container running inside the Nimbus orchestrator.</p>
</body>
</html>
EOF

tar cf "$OUTPUT_TAR" -C "$TMPDIR" manifest.toml rootfs

echo "done: $(ls -lh "$OUTPUT_TAR" | awk '{print $5}')"
