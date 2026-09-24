#!/usr/bin/env bash
# Every check this repository knows how to run, in the order that fails fastest.
#
# -count=1 throughout, and not as a habit. Some tests read files outside their
# own module — docker-compose.yaml, the Kubernetes manifests, the SQL schemas,
# the isolation policies — and Go's test cache does not track those. Without it,
# changing compose and running `go test ./...` reports a cached pass: the check
# appears to run and does nothing. A test suite that cannot fail is worse than no
# test suite, because somebody is relying on it.
#
# One e2e test compares each schema against its committed version, so it needs a
# git checkout rather than an exported tree. It skips, saying so, where git is
# not available.
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
# pkg included. It was not, and the comment immediately below — which says pkg is
# gated like everything else — was true of every step except this one. A file in
# it had been unformatted for as long as it had been here and nothing said so,
# which is what a check with a scope nobody re-reads looks like. Found by
# golangci-lint, whose gofmt runs per module and therefore had no such list.
unformatted="$(gofmt -l libs services e2e tools pkg 2>/dev/null)"
if [ -n "$unformatted" ]; then
  echo "    not gofmt'd:"; echo "$unformatted" | sed 's/^/      /'; fail=1
fi

# Every module in the workspace, pkg included. pkg used to be skipped here: it
# was a library dump of over a hundred packages from another product, most of
# which did not build. It now holds the two packages the platform imports and
# nothing else, and is gated like everything else.
# golangci-lint, when it is here.
#
# Skipped by name rather than silently, the way cargo is below: a linter that is
# absent and says nothing is a gate step that passes because it did not run.
#
# What it checks and what it deliberately does not is in .golangci.yml, beside
# the reasons. It runs per module because the workspace has thirty-five of them
# and the linter takes one module at a time.
# It also has to have been built with a Go at least as new as the one these
# modules target. golangci-lint refuses a module whose language version is above
# its own build's, and the released binaries lag — so the copy on PATH can be
# present, look fine, and fail every module with a message about toolchains.
# Checked here rather than discovered thirty-five times.
lint=""
want="$(go env GOVERSION | sed 's/^go//;s/\([0-9]*\.[0-9]*\).*/\1/')"
for candidate in "$(command -v golangci-lint 2>/dev/null)" "$(go env GOPATH)/bin/golangci-lint"; do
  [ -x "$candidate" ] || continue
  built="$("$candidate" --version 2>/dev/null | sed -n 's/.*built with go\([0-9]*\.[0-9]*\).*/\1/p')"
  if [ -n "$built" ] && [ "$(printf '%s\n%s\n' "$want" "$built" | sort -V | head -1)" = "$want" ]; then
    lint="$candidate"
    break
  fi
done
if [ -z "$lint" ]; then
  echo; echo "==> lint: no golangci-lint built with Go $want or newer, skipping."
  echo "    go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest"
fi

for dir in $(go list -m -f '{{.Dir}}' 2>/dev/null); do
  name="${dir#$ROOT/}"
  step "vet $name"  bash -c "cd '$dir' && go vet ./..."
  if [ -n "$lint" ]; then
    step "lint $name" bash -c "cd '$dir' && '$lint' run --timeout 10m ./..."
  fi
  step "test $name" bash -c "cd '$dir' && go test -count=1 ./..."
done

step "vet e2e (build-tagged)" bash -c "cd '$ROOT/e2e' && go vet -tags e2e ./..."

# The supervisor's console, which nothing compiled.
#
# The gate ran thirty-five Go modules and the Rust workspace, and never once
# built or typechecked either client. The two are compared against the platform
# by clients_test.go — procedure names, package prefixes, field names — and that
# comparison reads them as text, so it would go on passing against a console
# that does not compile.
#
# What it cost: the console sent a tenant header the gateway does not read and
# no credential at all, so every screen in it answered 401 from the day
# authorisation was added. Nothing here would have caught that either, and
# clients_auth_test.go is what does; this is the other half, so that the console
# is at least known to build.
#
# Skipped by name rather than silently, the way cargo and golangci-lint are: a
# check that is absent and says nothing is a gate step that passes because it
# did not run. npm ci is not run here — an install is not the gate's job, and a
# gate that reaches the network mid-run fails for reasons that have nothing to
# do with the change.
#
# GAVYA_REQUIRE_WEB_CHECK turns that skip into a refusal, and CI sets it.
# Skipping by name is honest on a laptop, where somebody may not have installed
# the console's dependencies and should not be stopped from running the Go
# checks. It is not honest under a green tick: CI ran for the first time on 24
# September 2026 and reported "all checks passed" without ever typechecking the
# console, because no step installed node_modules and the skip above said so to
# a log nobody reads. Every screen for twenty-eight services lives there, and
# exact.Fixed crosses the wire as a string — a field read as a number is exactly
# what svelte-check catches and nothing else does.
#
# So the requirement is declared here rather than assumed from the environment.
# A check that is arranged somewhere else and merely hoped for in here is one
# that goes quiet the day the arrangement is edited, which is the whole failure
# this variable exists to stop.
if command -v npm >/dev/null 2>&1 && [ -d "$ROOT/web/node_modules" ]; then
  step "web typecheck" bash -c "cd '$ROOT/web' && npm run --silent check"
elif [ -n "${GAVYA_REQUIRE_WEB_CHECK:-}" ]; then
  echo; echo "==> web typecheck"
  if command -v npm >/dev/null 2>&1; then
    echo "    FAILED: GAVYA_REQUIRE_WEB_CHECK is set and web/node_modules is absent."
    echo "    Whatever installs it before the gate did not run. The gate does not"
    echo "    install: an install mid-run fails for reasons unrelated to the change."
  else
    echo "    FAILED: GAVYA_REQUIRE_WEB_CHECK is set and npm is not on PATH."
  fi
  fail=1
elif command -v npm >/dev/null 2>&1; then
  echo; echo "==> web: node_modules is absent, skipping the console's typecheck"
  echo "    run 'npm install' in web/ to include it"
else
  echo; echo "==> web: npm not found, skipping the console's typecheck"
fi

# The collection bench is not checked here at all.
#
# flutter analyze and flutter test exist and have never run in this
# environment, because there is no Dart toolchain in it. Said out loud rather
# than left as an absence: the bench carries the same class of defect the
# console did, and the only thing standing over it is clients_auth_test.go,
# which reads its source as text.
if command -v flutter >/dev/null 2>&1; then
  step "mobile analyze" bash -c "cd '$ROOT/mobile' && flutter analyze"
  step "mobile test" bash -c "cd '$ROOT/mobile' && flutter test"
else
  echo; echo "==> mobile: flutter not found, skipping the bench's analyze and tests"
fi

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
    *%s*)
      # The repository suites tagged dbintegration read a plain DSN to a
      # database that already holds every schema, and apply nothing themselves.
      # Nothing provisioned one, so from the day they were written nothing ran
      # them. Provision it here, once, from the same template the e2e harness
      # uses, and run every module that carries such a suite.
      if url="$(cd "$ROOT/e2e" && go run ./cmd/provision -dsn "$TEST_DATABASE_DSN" -root "$ROOT")"; then
        # services, tools and libs: the backup round trip lives under tools, and
        # a glob that named only services would have left it unrun, which is the
        # exact failure this step was added to fix. libs was added for the same
        # reason one step later — tenantdb's settings are read back off a real
        # connection, and a suite nothing runs proves nothing.
        for dir in $(grep -rl --include='*_test.go' '^//go:build dbintegration' \
                       "$ROOT/services" "$ROOT/tools" "$ROOT/libs" \
                     | xargs -n1 dirname | sort -u); do
          rel="${dir#$ROOT/}"
          # The backup suite creates and drops databases of its own, so it needs
          # the template rather than the one provisioned database.
          step "dbintegration $rel" bash -c \
            "cd '$dir' && TEST_DATABASE_URL='$url' TEST_DATABASE_DSN='$TEST_DATABASE_DSN' \
             go test -count=1 -tags dbintegration ."
        done
      else
        echo "    FAILED: provision the dbintegration database"; fail=1
      fi
      step "e2e" bash -c "cd '$ROOT/e2e' && go test -count=1 -tags e2e -timeout 30m ./..." ;;
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
