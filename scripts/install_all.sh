#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$HOME/.local/bin"
mkdir -p "$BIN"

echo "=== Building Quorum binaries ==="
cd "$ROOT/quorum"
go build -o "$BIN/quorum" ./cmd/quorum/
go build -o "$BIN/qctl" ./cmd/qctl/
go build -o "$BIN/nimbusctl" ./cmd/nimbusctl/
chmod +x "$BIN/quorum" "$BIN/qctl" "$BIN/nimbusctl"

echo "=== Building Forge binaries ==="
cd "$ROOT/forge"
cargo build --release --bin forge --bin forge-agent 2>&1
cp target/release/forge "$BIN/forge"
cp target/release/forge-agent "$BIN/forge-agent"
chmod +x "$BIN/forge" "$BIN/forge-agent"

echo ""
echo "All binaries installed in $BIN:"
ls -la "$BIN/quorum" "$BIN/qctl" "$BIN/nimbusctl" "$BIN/forge" "$BIN/forge-agent"
echo ""
echo "Make sure $BIN is in your PATH:"
echo '  export PATH="$HOME/.local/bin:$PATH"'
