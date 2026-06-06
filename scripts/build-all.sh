#!/usr/bin/env bash
# Build all service binaries.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

for svc_dir in "$ROOT"/services/*/; do
  svc="$(basename "$svc_dir")"
  echo "==> Building $svc"
  cd "$svc_dir" && go build ./cmd/server/...
done

echo "Done."
