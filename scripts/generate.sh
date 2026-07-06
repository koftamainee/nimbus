#!/bin/bash
set -euo pipefail

protoc --proto_path=proto \
    --go_out=quorum/gen --go_opt=paths=source_relative \
    --go-grpc_out=quorum/gen --go-grpc_opt=paths=source_relative \
    proto/quorum/v1/*.proto
