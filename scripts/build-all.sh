#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

echo "=== Building Quorum ==="
cd "$ROOT/quorum"
go build -o "$ROOT/build/quorum" ./cmd/quorum/
go build -o "$ROOT/build/qctl" ./cmd/qctl/
go build -o "$ROOT/build/nimbusctl" ./cmd/nimbusctl/

echo "=== Building Forge ==="
cd "$ROOT/forge"
cargo build --release --bin forge --bin forge-agent 2>&1
mkdir -p "$ROOT/build"
cp target/release/forge "$ROOT/build/forge"
cp target/release/forge-agent "$ROOT/build/forge-agent"

echo ""
echo "All binaries built in build/:"
ls -la "$ROOT/build/"
echo ""
echo "To start a test cluster:"
echo "  scripts/start-cluster.sh"
