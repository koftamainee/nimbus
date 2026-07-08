#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

echo "=== Building Quorum (3 processes) ==="
cd "$ROOT/quorum"
go build -o "$ROOT/build/quorum-db" ./cmd/quorum-db/
go build -o "$ROOT/build/quorum-api" ./cmd/quorum-api/
go build -o "$ROOT/build/quorum-scheduler" ./cmd/quorum-scheduler/
go build -o "$ROOT/build/qctl" ./cmd/qctl/

echo "=== Building Nimbus ==="
cd "$ROOT/nimbus"
go build -o "$ROOT/build/nimbus-registry" ./cmd/nimbus-registry/
go build -o "$ROOT/build/nimbusctl" ./cmd/nimbusctl/
go build -o "$ROOT/build/nimbusadm" ./cmd/nimbusadm/

echo "=== Building Forge ==="
cd "$ROOT/forge"
cargo build --release --bin forge --bin forge-agent --bin forged 2>&1
mkdir -p "$ROOT/build"
cp target/release/forge "$ROOT/build/forge"
cp target/release/forge-agent "$ROOT/build/forge-agent"
cp target/release/forged "$ROOT/build/forged"

echo ""
echo "All binaries built in build/:"
ls -la "$ROOT/build/"
echo ""
echo "To start a test cluster:"
echo "  scripts/start-cluster.sh"
