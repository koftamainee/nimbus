#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BUILD="$ROOT/build"
DATA="$ROOT/build/data"

mkdir -p "$DATA"/{node1,node2,node3,images}
mkdir -p "$BUILD"

cleanup() {
    echo "Shutting down..."
    for pid in "$DATA"/node1/pid "$DATA"/node2/pid "$DATA"/node3/pid; do
        [ -f "$pid" ] && kill "$(cat "$pid")" 2>/dev/null || true
    done
    wait
}
trap cleanup EXIT

echo "=== Starting 3-node Quorum cluster ==="
"$BUILD/quorum" \
    --id node1 --addr :9090 \
    --initial-cluster "node1=:9090,node2=:9092,node3=:9094" \
    --data-dir "$DATA/node1" \
    --http-addr :9091 \
    --image-dir "$DATA/images" &
echo $! > "$DATA/node1/pid"

"$BUILD/quorum" \
    --id node2 --addr :9092 \
    --initial-cluster "node1=:9090,node2=:9092,node3=:9094" \
    --data-dir "$DATA/node2" \
    --http-addr :9093 \
    --image-dir "$DATA/images" &
echo $! > "$DATA/node2/pid"

"$BUILD/quorum" \
    --id node3 --addr :9094 \
    --initial-cluster "node1=:9090,node2=:9092,node3=:9094" \
    --data-dir "$DATA/node3" \
    --http-addr :9095 \
    --image-dir "$DATA/images" &
echo $! > "$DATA/node3/pid"

echo "Waiting for cluster..."
sleep 3

echo "=== Cluster nodes ==="
"$BUILD/qctl" --addr :9090 get --prefix /nodes/ || echo "(no nodes yet)"

echo ""
echo "Cluster is running!"
echo "  qctl :9090     - low-level KV"
echo "  nimbusctl :9090 - orchestration CLI"
echo "  forge-agent    - run on each node"
echo ""
echo "Press Ctrl+C to stop."
wait
