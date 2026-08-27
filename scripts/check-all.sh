#!/usr/bin/env bash
# Every check this repository knows how to run, in the order that fails fastest.
#
# -count=1 throughout, and not as a habit. Some tests read files outside their
# own module — docker-compose.yaml, the SQL schemas, the isolation policies — and
# Go's test cache does not track those. Without it, changing compose and running
# `go test ./...` reports a cached pass: the check appears to run and does
# nothing. A test suite that cannot fail is worse than no test suite, because
# somebody is relying on it.
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

fail=0
step() {
  echo
  echo "==> $1"
  shift
  if ! "$@"; then
    echo "    FAILED: $*"
    fail=1
  fi
}

echo "=== formatting"
unformatted="$(gofmt -l libs services e2e 2>/dev/null)"
if [ -n "$unformatted" ]; then
  echo "    not gofmt'd:"; echo "$unformatted" | sed 's/^/      /'; fail=1
fi

for dir in $(go list -m -f '{{.Dir}}' 2>/dev/null | grep -v '/pkg$'); do
  name="${dir#$ROOT/}"
  step "vet $name"  bash -c "cd '$dir' && go vet ./..."
  step "test $name" bash -c "cd '$dir' && go test -count=1 ./..."
done

step "vet e2e (build-tagged)" bash -c "cd '$ROOT/e2e' && go vet -tags e2e ./..."

if command -v cargo >/dev/null 2>&1; then
  step "rust build" bash -c "cd '$ROOT/ml' && cargo build --workspace --offline"
  step "rust test"  bash -c "cd '$ROOT/ml' && cargo test --workspace --offline"
  step "go<->rust contract" bash -c \
    "cd '$ROOT/libs/integrity' && go test -count=1 -tags mlintegration ./mlclient/..."
else
  echo; echo "==> rust: cargo not found, skipping the ML tier and the contract tests"
fi

# The end-to-end suite needs a database. The DSN carries a single %s where each
# service's own database name goes; without it every service lands in one
# database and the suite still passes, which is worse than failing.
if [ -n "${TEST_DATABASE_DSN:-}" ]; then
  case "$TEST_DATABASE_DSN" in
    *%s*) step "e2e" bash -c "cd '$ROOT/e2e' && go test -count=1 -tags e2e -timeout 30m ./..." ;;
    *) echo; echo "==> e2e: TEST_DATABASE_DSN has no %s in it."
       echo "    Every service would share one database and the suite would pass anyway."
       fail=1 ;;
  esac
else
  echo; echo "==> e2e: set TEST_DATABASE_DSN to run it, e.g."
  echo "    TEST_DATABASE_DSN='postgres://user@host:5432/%s?sslmode=disable'"
fi

echo
if [ "$fail" -eq 0 ]; then echo "all checks passed"; else echo "SOME CHECKS FAILED"; fi
exit "$fail"
