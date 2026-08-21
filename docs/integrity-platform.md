# Dairy Integrity Platform

This document describes the integrity layer added on top of the Gavya dairy ERP:
what it is for, the guarantees it makes, and where each guarantee is enforced.

## The problem

A cooperative already has a system that computes what it pays producers. Replacing
it outright means asking finance to trust a new number on day one, with nothing to
check it against. Nobody sensibly agrees to that.

So the platform runs in **shadow mode**. It imports the incumbent system's records
and its settlement results, independently recomputes those settlements from the
same underlying collections, and reports every difference with a reason. Nothing is
paid from a shadow computation. The deliverable is not a payment run — it is a
defensible account of where the two systems agree, where they differ, and why.

That framing drives every design decision below. In particular it means the
platform must never quietly discard a record, never guess when evidence is
insufficient, and never let a model decide money.

## Six properties

Everything here exists to hold one of these.

| Property | Where it is enforced |
|---|---|
| **Append-only history** | No `UPDATE` to a record's content anywhere. The only permitted write to an existing row is marking it superseded. |
| **Bitemporality** | Every authoritative record carries `valid_from`/`valid_to` (true of the world) and `recorded_at`/`superseded_at` (known to the platform). |
| **Derivation provenance** | Every record carries a `RecordOrigin`: `NATIVE`, `IMPORTED` or `DERIVED`, with the source system, import batch, source record and payload hash for imports. |
| **Deterministic money** | Fixed-point arithmetic with an explicit rounding mode and a recorded rounding trail. No floats touch a currency amount. |
| **Replayable idempotent ingestion** | Device generation + capture session + sequence. Redelivering a record any number of times admits it exactly once. |
| **Database-enforced tenant isolation** | `tenant_id` on every table and in every query, with constraints scoped per tenant. |

## Architecture

```
Field devices ──▶ ingestion-service ──▶ observation-service ──▶ pooling-service
                  (idempotency,          (bitemporal facts,      (FMMO fixture,
                   quarantine)            uncertainty)            exact allocation)
                                              │                       │
Incumbent   ──▶ canonical-service ────────────┴───────────────────────┤
system          (identity mapping,                                    │
                 collection slots)                                    ▼
                                                     shadow-settlement-service
                                                     (deterministic divergence
                                                      classification)
                                                              │
                            ┌─────────────────────────────────┘
                            │  advisory only, over the network
                            ▼
                    Rust ML tier (ml/)
        anomaly · uncertainty · reconciliation · divergence
```

### How services talk to each other

Every service serves its procedures over **Connect unary JSON**: a `POST` to
`/<fully.qualified.Service>/<Method>` with a JSON body and a JSON reply. Both
directions of traffic — Go to Go, and Go to the Rust ML tier — look identical on
the wire.

The request and response types are plain Go structs rather than generated
protobuf messages, so Connect's built-in codecs (which require `proto.Message`)
cannot be used. `libs/integrity/connectjson` adapts a handler method to an HTTP
endpoint; `libs/integrity/svcclient` is the calling side.

`svcclient` is deliberately not `mlclient`. A failed ML call is advisory and the
caller degrades to its deterministic path; a failed call to another Go service is
a real failure. So `svcclient` surfaces the callee's own Connect code rather than
flattening everything to "unavailable", and it retries only transient codes —
retrying a procedure that already had an effect risks doing it twice, so the
default is no retry at all.

The code-to-status mapping is explicit in both, because clients retry 5xx and give
up on 4xx: a validation failure returned as 500 would be retried several times
before failing, and an overloaded service returning 400 would never be retried.

### The Go/Rust boundary

All ML and AI runs in Rust, in separate processes, reached only over the network.
There is no cgo, no shared memory, no in-process model loading. A procedure is a
Connect unary JSON `POST` to `/<fully.qualified.Service>/<Method>`.

The Go side is `libs/integrity/mlclient`. The Rust side is the `ml/` cargo
workspace, sharing a runtime crate `mlcore` for the wire envelope, the error
vocabulary and the serve loop.

**Every ML call is advisory.** If the ML tier is slow, unreachable or not deployed
at all, the platform still produces a complete and authoritative answer — it simply
stops offering the advisory signal. The clearest case is divergence: the
deterministic classifier in Go assigns `MATCH`, `ROUNDING_DIFFERENCE`,
`INPUT_DIFFERENCE`, `POLICY_DIFFERENCE` and `RECOVERY_DIFFERENCE` on its own, and
the ML tier is consulted *only* about what comes back `UNEXPLAINED`. Its answer is
stored beside the authoritative verdict, never in place of it.

Two encoding traps on this boundary are worth knowing about, because both invert
meaning silently:

- JSON cannot carry an infinity. `serde_json` writes a non-finite float as `null`,
  and Go decodes `null` into `float64` as `0.0`. An unbounded anomaly band would
  arrive as `[0, 0]` — an infinitely *tight* band. Infinite effective degrees of
  freedom would arrive as *zero* degrees of freedom. Both are `Option<f64>` on the
  Rust side and `*float64` on the Go side, with `nil` meaning unbounded/infinite.
- HTTP status determines whether Go retries. `insufficient_evidence` is a 422, not
  a 5xx, so a genuinely under-determined input is not retried three times.

`libs/integrity/mlclient/contract_integration_test.go` starts the real Rust
binaries and drives them through the real Go client. Unit tests on either side
cannot catch a renamed field or a value that does not survive the encoding.

## Services

### `ingestion-service` — replayable ingestion

Field devices go offline, get reflashed, reinstalled and retried, and each of those
restarts their sequence counter. Without an identity epoch, a restarted counter is
indistinguishable from a replay of records already sent, and the platform would
either drop real collections or admit duplicates.

- **DeviceGeneration** gives each epoch its own sequence space.
- **CaptureSession** binds a run of captures to one generation.
- `(device, generation, session, sequence)` is the idempotency key, enforced by a
  unique index so a race resolves to one row without application-level locking.

Outcomes are `ACCEPTED`, `DUPLICATE_REPLAY` or `QUARANTINED`. A record whose
identity is ambiguous is **quarantined, never dropped** — the payload is kept in
full for an operator to release. Dropping a producer's collection over a
sequence-number dispute is the worst available outcome.

Quarantine reasons: `TRANSPORT_IDENTITY_CONFLICT`, `SEQUENCE_REGRESSION`,
`UNTRUSTED_SESSION_IDENTITY`, `STALE_GENERATION`, `SESSION_NOT_ACCEPTING`.

### `canonical-service` — identity and collection slots

**ExternalIdentity** maps an incumbent system's identifier to a platform entity.
It is bitemporal because identifiers get reused: a cooperative retires member
number P-001 and reissues it years later. A database exclusion constraint
(`EXCLUDE USING gist`) enforces that mappings for one identifier never overlap in
valid time, so a record from any period resolves to exactly one producer while
reuse over time stays legal. Resolution therefore *requires* an instant to ask
about; there is no timeless answer.

**CollectionIdentityPolicy** declares per tenant what makes two records the same
collection — producer and date, or producer, date and shift, or with a centre.
This cannot be a platform constant: too coarse silently merges two real
collections into one payment, too fine lets a duplicate entry be paid twice.

Collisions resolve by `FIRST_WINS`, `LAST_WINS`, `HIGHEST_QUALITY` or `MANUAL`.
Anything the policy cannot separate — a tie on quality, two claims recorded at the
same instant — becomes a **`COLLECTION_SLOT_CONFLICT`** rather than a guess, and a
conflicted slot names no holder, so a settlement cannot quietly consume a
collection under dispute.

Native and imported claims occupy **separate slots**. In shadow mode both exist for
the same collection by design; forcing them into one slot would report an entire
import as conflict.

### `observation-service` — bitemporal facts

An Observation is one measured fact about a subject at a point in valid time.
Subject references are **typed columns**, not a polymorphic `subject_type`/
`subject_id` pair, so they can be foreign-keyed and type-checked.

Quantitative observations carry an `uncertainty_model_id` and the computed
standard and expanded uncertainty, obtained from the Rust uncertainty service.
If that call fails the observation is still recorded, with the estimate marked
missing.

**Indian legal-metrology eligibility** is a deterministic predicate: an observation
used for trade settlement must come from an instrument currently verified under the
Legal Metrology Act. A missing certificate yields `UNKNOWN`, not `NOT_ELIGIBLE` —
absence of evidence is not evidence of absence. Ineligible observations are
recorded with their verdict rather than rejected, so an auditor can see exactly
which payments rested on unverified instruments.

### `pooling-service` — FMMO structural fixture

An Indian cooperative does not file a Federal Milk Marketing Order, but the shape
is the same: pool the milk, value it by what it was used for, pay producers a blend
of that value. Modelling it explicitly keeps the two separable.

`ProducerMilk → Pool → ClassifiedUtilisation → PoolValuation → Allocation →
ProducerEconomicEvent`.

The invariant the whole computation rests on: **producers' totals sum to the pool's
classified value exactly**, with no residual. The residual fund is split with
remainder-exact allocation rather than by rounding each share independently, which
would leak minor units. The invariant is checked, not assumed — a failure means a
defect has silently moved producers' money, and it surfaces there rather than in a
payment run.

**RecoveryRetroactivityPolicy** governs whether a late correction may reach a
settled pool: `DO_NOT_REOPEN`, `RECALCULATE`, `APPLY_INCREMENTAL` or `CUSTOM`, all
bounded by a lookback window and a materiality floor. `CUSTOM` escalates to a person
rather than guessing which of the other three was meant.

### `shadow-settlement-service` — the comparison

Holds what the incumbent asserts (`ExternalSettlementAssertion`), what this
platform computed (`ShadowSettlementComputation`), and every difference
(`SettlementDivergence`).

The classifier is deterministic and consults no model. Given the same two records
it always returns the same verdict, which is what makes a shadow settlement
defensible. It works strongest-evidence-first:

1. **Rounding** — every differing line off by at most one minor unit, and the total
   within the bound per-line rounding can produce.
2. **Recovery** — the recovery lines account for the entire gap and every other
   line agrees. This is the most common benign divergence: the two systems agree
   about the milk and disagree about which period an advance is recovered in.
3. **Policy** — a line applied a different rate to the same quantity, or exists on
   only one side.
4. **Input** — a line used the same rate over a different quantity.
5. **Unexplained** — and only here is the ML tier asked.

`INSUFFICIENT_EVIDENCE` is distinct from `UNEXPLAINED`: it means the comparison
could not be made at all — mismatched currency, a missing counterpart, no component
detail on either side.

## `libs/integrity`

- **`money`** — fixed-point arithmetic. Products are carried at full 128-bit width
  and narrowed once, after rounding. Every precision-losing operation returns a
  `RoundingStep` so a settlement carries an auditable rounding trail rather than an
  unexplainable residual. `Allocate` splits an amount so the parts sum exactly to
  the whole, deterministically.
- **`origin`** — `NATIVE` / `IMPORTED` / `DERIVED` provenance with payload hashing.
  An imported record without a payload hash is rejected: without it, a re-import
  cannot be told from an amendment.
- **`bitemporal`** — valid time and transaction time, with an `EndOfTime` sentinel
  rather than `NULL` so interval overlap stays expressible in plain SQL.
- **`mlclient`** — the typed Go boundary to the Rust tier.

## Running the tests

Unit tests need nothing:

```sh
cd libs/integrity && go test ./...
cd ml && cargo test --workspace && cargo clippy --workspace --all-targets -- -D warnings
```

The Go↔Rust contract tests start the real Rust binaries:

```sh
cd ml && cargo build --workspace
cd libs/integrity && go test -tags mlintegration ./mlclient/...
```

Database tests need a PostgreSQL 16 with the service's schema applied:

```sh
psql -c "CREATE DATABASE ingestion;"
psql -d ingestion -f services/ingestion-service/internal/db/schema.sql
cd services/ingestion-service && \
  TEST_DATABASE_URL="postgres://..." go test -tags dbintegration ./internal/repository/...
```

The database tests are where the guarantees are actually proven: that eight
concurrent deliveries of one record produce exactly one admission, that an
identifier can be reused over time but never overlap, that a conflicted slot cannot
name a holder, that money survives a round trip without losing a minor unit.

The end-to-end tests build the real service binaries, run them against a real
PostgreSQL, and drive them over HTTP as another service would — the only way to
show the platform is a pipeline rather than a pile of components:

```sh
TEST_DATABASE_DSN="postgres://user@host:port/%s?sslmode=disable" \
  go test -tags e2e ./e2e/...
```

The DSN carries a single `%s` where the per-service database name goes; each
service in the harness gets its own database, as it would in production. The
harness deliberately runs with **no ML tier configured**, and the verdicts are
complete anyway — that is the point of the deterministic classifier, and it is
asserted rather than assumed.

## Deploying

`docker-compose.yaml` at the repository root brings up PostgreSQL, the ERP
services, the five integrity services and the four Rust ML services. Every Go
service builds from the repository root, because each Dockerfile copies `pkg/`
and `libs/`, which only resolve from there.

The ML tier shares one parameterised Dockerfile:

```sh
docker build -f ml/Dockerfile --build-arg SERVICE=anomaly-service -t gavya/anomaly-service ml/
```

Kubernetes manifests live beside each service under `deployments/k8s/`, with the
whole ML tier in `ml/deployments/k8s/ml-tier.yaml`. The ML pods hold no state and
reach no database, so they scale on request load alone, run as nonroot with a
read-only root filesystem, and can be restarted or removed without affecting the
authoritative path.

Each Go service that calls the ML tier takes its endpoint from an environment
variable — `ANOMALY_ML_URL`, `UNCERTAINTY_ML_URL`, `DIVERGENCE_ML_URL`. Leaving one
empty disables that call: the service still starts, still answers, and simply
stops offering the advisory signal. There is also an optional
`*_MODEL_VERSION` pin, which makes the service refuse any model version but the
one named, so a replayed adjudication cannot silently pick up a retrained model.
