#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BUILD="$ROOT_DIR/build"
NIMBUS() { sudo "$BUILD/nimbusadm" "$@"; }
CTL="$BUILD/nimbusctl --addr http://localhost:10102"
IMGDIR="$ROOT_DIR/demo/images/out"

info()  { echo -e "\033[1;34m==>\033[0;34m $1\033[0m"; }
ok()    { echo -e "  \033[1;32m✓\033[0m $1"; }
cmd()   { echo -e "\033[1;33m$\033[0m $1"; }

cd "$ROOT_DIR"

# ── 0. Start cluster ──
#cleanup() { NIMBUS stop 2>/dev/null || true; }
#trap cleanup EXIT

info "Starting Nimbus cluster (sudo required)..."
sudo -v  # cache sudo credential
cmd "sudo nimbusadm start"
NIMBUS start
sleep 4
ok "cluster started"

# ── 1. Upload images to registry ──
info "Uploading container images to registry..."
for img in tigerbeetle nats-server job-service worker; do
  cmd "curl -X PUT http://localhost:11111/images/\$img --data-binary @\$IMGDIR/\$img.tar"
  curl -sf -X PUT "http://localhost:11111/images/$img" \
    --data-binary "@$IMGDIR/$img.tar" > /dev/null
  ok "$img uploaded"
done

# ── 2. Deploy infrastructure ──
info "Deploying infrastructure containers..."

cmd "nimbusctl run tigerbeetle --image tigerbeetle"
$CTL run tigerbeetle --image tigerbeetle

cmd "nimbusctl run nats-server --image nats-server"
$CTL run nats-server --image nats-server
sleep 10

cmd "nimbusctl run job-service --image job-service --env NATS_ADDR=127.0.0.1:4222 --env TB_ADDR=127.0.0.1:3000"
$CTL run job-service --image job-service --env NATS_ADDR=127.0.0.1:4222 --env TB_ADDR=127.0.0.1:3000
sleep 2

ok "infrastructure deployed"

# ── 3. Check status ──
info "Checking container status..."
$CTL ps

# ── 4. Start frontend ──
pkill -f 'nuxt dev' 2>/dev/null || true
info "Starting frontend at http://localhost:3334 ..."
(cd "$ROOT_DIR/demo/web" && npx nuxt dev --port 3334 &)
sleep 4
ok "frontend ready"

# ── 5. Start workers ──
info "Starting workers (1 replica)..."
cmd "nimbusctl run worker --image worker --replicas 1 --env NATS_ADDR=127.0.0.1:4222 --env TB_ADDR=127.0.0.1:3000"
$CTL run worker --image worker --replicas 1 --env NATS_ADDR=127.0.0.1:4222 --env TB_ADDR=127.0.0.1:3000
sleep 3

$CTL ps

# ── 6. Generate transactions ──
info "Generating transactions..."
cmd "curl -X POST 'http://localhost:9090/start?count=5000&speed=100'"
curl -s -X POST 'http://localhost:9090/start?count=5000&speed=100'
echo ""

info "Observe: 1 worker processing ~100 tps on the dashboard."
info "Open http://localhost:3334 in a browser."
echo ""
read -p "Press Enter to scale to 10 workers..."

## ── 7. Scale workers ──
#info "Scaling to 10 workers..."
#cmd "nimbusctl scale worker --replicas 10"
#$CTL scale worker --replicas 10

#sleep 3
#
#$CTL ps

info "Observe: 10 workers processing ~1000 tps."
echo ""
read -p "Press Enter to scale down to 0 and stop demo..."

## ── 8. Stop workers ──
#info "Scaling down to 0..."
#$CTL scale worker --replicas 0
#sleep 1

## ── 9. Stop demo containers ──
#info "Stopping demo containers..."
#$CTL stop job-service || true
#$CTL stop nats-server || true
#$CTL stop tigerbeetle || true

info "Demo complete!"
echo ""
echo "If the frontend is still running: pkill -f 'nuxt dev'"
