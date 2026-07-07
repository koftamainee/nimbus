#!/usr/bin/env bash
set -euo pipefail

# Test: minimal busybox-based container image
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
IMG_DIR="$ROOT/build/data/images"
mkdir -p "$IMG_DIR"

echo "=== Creating test image tar (busybox) ==="
# Use docker to export a minimal busybox rootfs, or create a minimal one
if command -v docker &>/dev/null; then
    docker pull busybox:latest 2>&1 | tail -1
    docker export "$(docker create busybox)" > "$IMG_DIR/busybox.tar"
    echo "busybox.tar: $(wc -c < "$IMG_DIR/busybox.tar") bytes"
else
    # Create a minimal rootfs with busybox static binary
    TMP=$(mktemp -d)
    cd "$TMP"
    mkdir -p bin etc dev proc sys tmp
    if command -v busybox &>/dev/null; then
        cp "$(which busybox)" bin/
        for applet in sh sleep echo ls cat; do
            ln -sf busybox "bin/$applet"
        done
    else
        # Download busybox static binary
        curl -sL https://busybox.net/downloads/binaries/1.35.0-x86_64-linux-musl/busybox -o bin/busybox
        chmod +x bin/busybox
        for applet in sh sleep echo ls cat; do
            ln -sf busybox "bin/$applet"
        done
    fi
    tar cf "$IMG_DIR/busybox.tar" .
    rm -rf "$TMP"
    echo "busybox.tar: $(wc -c < "$IMG_DIR/busybox.tar") bytes"
fi

echo ""
echo "Image ready at $IMG_DIR/busybox.tar"
echo "Push to HTTP server:"
echo "  curl -X PUT --data-binary @$IMG_DIR/busybox.tar http://127.0.0.1:9091/images/busybox"
