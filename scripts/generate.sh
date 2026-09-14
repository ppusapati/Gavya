#!/usr/bin/env bash
# Regenerate protobuf + connectrpc bindings for all services.
# Requires: buf (https://buf.build/docs/installation)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

for svc_dir in "$ROOT"/services/*/; do
  svc="$(basename "$svc_dir")"
  proto_dir="$svc_dir/internal/transport/proto"
  if [ -f "$proto_dir/buf.yaml" ]; then
    echo "==> Generating $svc"
    cd "$proto_dir" && buf generate
  fi
done

echo "Done."
