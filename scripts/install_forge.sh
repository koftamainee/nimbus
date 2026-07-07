#!/usr/bin/env bash
set -euo pipefail

BINDIR="${HOME}/.local/bin"
FORGE_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

mkdir -p "${BINDIR}"

echo "building forge..."
cargo build --release --manifest-path "${FORGE_ROOT}/forge/Cargo.toml"
cp "${FORGE_ROOT}/forge/target/release/forge" "${BINDIR}/forge"

echo "installed to ${BINDIR}/forge"
ls -la "${BINDIR}/forge"
