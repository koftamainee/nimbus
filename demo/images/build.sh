#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DEMO_DIR="$(dirname "$SCRIPT_DIR")"
OUTPUT_DIR="$SCRIPT_DIR/out"
BIN_DIR="$SCRIPT_DIR/bin"
mkdir -p "$OUTPUT_DIR" "$BIN_DIR"

info()  { echo -e "\033[1;34m==>\033[0;34m $1\033[0m"; }
ok()    { echo -e "  \033[1;32m✓\033[0m $1"; }

dl_binary() {
    local url=$1 dest=$2
    local attempts=0
    while [ $attempts -lt 3 ]; do
        attempts=$((attempts + 1))
        docker run --rm busybox:latest wget -q -O - "$url" > "$dest" 2>/dev/null
        local size
        size=$(stat -c%s "$dest" 2>/dev/null)
        if [ "$size" -gt 1000 ]; then
            return 0
        fi
        echo "  retry $attempts/3 (got ${size:-0} bytes)"
        sleep 2
    done
    echo "  failed to download"
    return 1
}

prepare_rootfs() {
    local base_image="debian:bookworm-slim"
    local cache_image="forge-rootfs-debian"
    if ! docker image inspect "$cache_image" &>/dev/null; then
        info "Building glibc rootfs cache image..."
        docker build --platform=linux/amd64 -t "$cache_image" -f - . <<'DOCKERFILE' 2>&1 | tail -3
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
DOCKERFILE
        ok "rootfs cache image built"
    else
        info "Using cached rootfs image $cache_image"
    fi
    local cid
    cid=$(docker create --platform=linux/amd64 "$cache_image")
    docker export "$cid" | tar -C "$1" -xf -
    docker rm "$cid" > /dev/null 2>&1
}

package_image() {
    local name=$1
    local binary=$2
    local entry=${3:-$name}
    local extra_dir=${4:-}

    info "Packaging $name..."

    local tmpdir
    tmpdir=$(mktemp -d)
    mkdir -p "$tmpdir/rootfs"

    prepare_rootfs "$tmpdir/rootfs"

    cp "$BIN_DIR/$binary" "$tmpdir/rootfs/$name"
    chmod +x "$tmpdir/rootfs/$name"

    if [ -n "$extra_dir" ] && [ -d "$BIN_DIR/$extra_dir" ]; then
        cp -r "$BIN_DIR/$extra_dir/"* "$tmpdir/rootfs/"
        find "$tmpdir/rootfs" -type f -name '*.sh' -exec chmod +x {} \;
    fi

    cat > "$tmpdir/manifest.toml" << EOF
entrypoint = ["/${entry}"]
EOF

    tar cf "$OUTPUT_DIR/$name.tar" -C "$tmpdir" manifest.toml rootfs
    rm -rf "$tmpdir"
    ok "$OUTPUT_DIR/$name.tar ($(du -h "$OUTPUT_DIR/$name.tar" | cut -f1))"
}

# ── 1. Build job-service (Go) ──
info "Building job-service binary..."
cd "$DEMO_DIR/job-service"
CGO_ENABLED=0 go build -o "$BIN_DIR/job-service" .
ok "job-service binary built"

# ── 2. Build worker (Rust) ──
info "Building worker binary..."
cd "$DEMO_DIR/worker"
cargo build --release --quiet 2>/dev/null || cargo build --release
cp target/release/worker "$BIN_DIR/worker"
ok "worker binary built"

# ── 3. Download nats-server ──
info "Downloading nats-server..."
if [ ! -f "$BIN_DIR/nats-server" ] || [ ! -s "$BIN_DIR/nats-server" ]; then
    dl_binary "https://github.com/nats-io/nats-server/releases/download/v2.10.22/nats-server-v2.10.22-linux-amd64.tar.gz" \
              "/tmp/nats.tar.gz"
    tar xzf "/tmp/nats.tar.gz" --wildcards -O "*/nats-server" > "$BIN_DIR/nats-server"
    chmod +x "$BIN_DIR/nats-server"
    rm -f "/tmp/nats.tar.gz"
    ok "nats-server downloaded"
else
    ok "nats-server already cached"
fi

# ── 4. Download tigerbeetle ──
info "Downloading tigerbeetle..."
if [ ! -f "$BIN_DIR/tigerbeetle" ] || [ ! -s "$BIN_DIR/tigerbeetle" ]; then
    dl_binary "https://github.com/tigerbeetle/tigerbeetle/releases/download/0.17.9/tigerbeetle-x86_64-linux.zip" \
              "/tmp/tb.zip"
    unzip -o "/tmp/tb.zip" -d "$BIN_DIR" > /dev/null 2>&1
    chmod +x "$BIN_DIR/tigerbeetle" 2>/dev/null || true
    rm -f "/tmp/tb.zip"
    ok "tigerbeetle downloaded"
else
    ok "tigerbeetle already cached"
fi

# ── 5. Create tigerbeetle init.sh ──
mkdir -p "$BIN_DIR/tigerbeetle-extra"
cat > "$BIN_DIR/tigerbeetle-extra/init.sh" << 'SCRIPT'
#!/bin/sh
set -e
DATA_DIR="${DATA_DIR:-/data}"
DATA_FILE="$DATA_DIR/db.tigerbeetle"
mkdir -p "$DATA_DIR"
if [ ! -f "$DATA_FILE" ]; then
    /tigerbeetle format --cluster=0 --replica=0 --replica-count=1 "$DATA_FILE"
fi
exec /tigerbeetle start --addresses=0.0.0.0:3000 "$DATA_FILE"
SCRIPT
chmod +x "$BIN_DIR/tigerbeetle-extra/init.sh"

echo ""
package_image "job-service"   "job-service"   "job-service"   ""
package_image "worker"        "worker"        "worker"        ""
package_image "nats-server"   "nats-server"   "nats-server"   ""
package_image "tigerbeetle"   "tigerbeetle"   "init.sh"       "tigerbeetle-extra"

echo ""
info "All images built!"
ls -lh "$OUTPUT_DIR"/*.tar