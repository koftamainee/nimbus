#!/usr/bin/env bash
set -euo pipefail

BINDIR="${HOME}/.local/bin"
QUORUM_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

mkdir -p "${BINDIR}"

echo "building quorum..."
go build -C "${QUORUM_ROOT}/quorum" -o "${BINDIR}/quorum" ./cmd/quorum
echo "building qctl..."
go build -C "${QUORUM_ROOT}/quorum" -o "${BINDIR}/qctl"   ./cmd/qctl

echo "installed to ${BINDIR}:"
ls -la "${BINDIR}/quorum" "${BINDIR}/qctl"
