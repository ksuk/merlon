#!/bin/bash
set -euo pipefail

PROTO_DIR="proto"
GO_OUT="api/gen"

mkdir -p "$GO_OUT"

protoc \
  --go_out="$GO_OUT" --go_opt=paths=source_relative \
  --go-grpc_out="$GO_OUT" --go-grpc_opt=paths=source_relative \
  -I "$PROTO_DIR" \
  "$PROTO_DIR"/merlon/v1/types.proto \
  "$PROTO_DIR"/merlon/v1/scoring.proto \
  "$PROTO_DIR"/merlon/v1/monitoring.proto

echo "Generated Go proto stubs in $GO_OUT"
