#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

echo "============================================"
echo "  Nimbus Integration Test"
echo "============================================"

# 1. Build all binaries
echo ""
echo ">>> Step 1: Build all binaries"
scripts/build-all.sh

# 2. Kill any leftover cluster
pkill -f "build/quorum" 2>/dev/null || true
sleep 1

# 3. Start cluster
echo ""
echo ">>> Step 2: Start 3-node cluster"
scripts/start-cluster.sh &
CLUSTER_PID=$!
sleep 5

# 4. Push test image
echo ""
echo ">>> Step 3: Push test image (busybox)"
scripts/prepare-image.sh 2>&1 | tail -1
curl -sf -X PUT --data-binary @build/data/images/busybox.tar http://127.0.0.1:9091/images/busybox > /dev/null && echo "  image pushed"

# 5. Start forge-agent on node3 (leader)
echo ""
echo ">>> Step 4: Start forge-agent on node3"
echo "  (requires sudo - will prompt for password)"
sudo forge-agent --node-id node3 --quorum-addr http://127.0.0.1:9090 &
AGENT_PID=$!
sleep 2

# 6. Register all nodes via nimbusctl
echo ""
echo ">>> Step 5: Deploy test container"
nimbusctl --addr :9094 run test-nginx --image busybox --replicas 2

sleep 3

# 7. Check state
echo ""
echo ">>> Step 6: Check state"
echo ""
echo "--- Containers ---"
nimbusctl --addr :9094 ps
echo ""
echo "--- Nodes ---"
nimbusctl --addr :9094 nodes
echo ""
echo "--- Assignments ---"
build/qctl --addr :9094 get --prefix /nodes/node3/assignments/
echo ""

# Cleanup trap
trap "echo 'Stopping...'; kill $AGENT_PID 2>/dev/null; kill $CLUSTER_PID 2>/dev/null; wait" EXIT

echo "============================================"
echo "  Test running. Press Ctrl+C to stop."
echo "============================================"
wait
