#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

echo "=== Building nimbusctl ==="
cd "$ROOT/quorum"
go build -o /tmp/nimbusctl ./cmd/nimbusctl/

echo "=== Installing to ~/.local/bin ==="
mkdir -p "$HOME/.local/bin"
mv /tmp/nimbusctl "$HOME/.local/bin/nimbusctl"
chmod +x "$HOME/.local/bin/nimbusctl"

echo "nimbusctl installed. Usage: nimbusctl --addr <addr> <command>"
