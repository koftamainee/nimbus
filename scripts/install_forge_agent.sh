#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

echo "=== Building forge-agent ==="
cd "$ROOT/forge"
cargo build --release --bin forge-agent 2>&1

echo "=== Installing to ~/.local/bin ==="
mkdir -p "$HOME/.local/bin"
cp target/release/forge-agent "$HOME/.local/bin/forge-agent"
chmod +x "$HOME/.local/bin/forge-agent"

echo "forge-agent installed."
echo "Usage: forge-agent --node-id <id> --quorum-addr http://<addr>:9090"
