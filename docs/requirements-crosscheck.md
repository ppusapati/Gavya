# Requirements, against what was built

A cross-check: everything in this repository that states what the platform is
supposed to do, read against what it does.

Written down because the question "is anything still pending?" had no place to be
answered from. The roadmap records what was decided and why; it does not compare
that against a requirement somebody wrote before the work started.

---

## First, the thing that has to be said

There is one file in this repository's history called `requirements.md`, and I
deleted it without reading it.

Commit 6b30f24 trimmed `pkg/` from a hundred-odd packages to the two the platform
imports. `pkg/requirements.md` went with them, along with `pkg/design.md`,
`pkg/todo.md`, `pkg/explanation.md` and eight other documents. The commit message
says "the deleted code is in the history if any of it turns out to be wanted" and
that is true of the code; it was not a reason to delete a document named
*requirements* without opening it.

It is recovered now, verbatim, at
[`docs/inherited/kosha-requirements.md`](inherited/kosha-requirements.md), with
its sprint backlog at [`docs/inherited/kosha-todo.md`](inherited/kosha-todo.md).
Reading it changes what it is rather than what it says: it is the requirements
document for **Kosha**, the shared Go infrastructure library this repository
inherited from another product — dependency injection, a dynamic query builder,
a Kafka event bus. It is not a requirements document for a dairy platform. That
does not make deleting it unread correct, and it is not the reason it was
deleted.

`pkg/design.md` and `pkg/explanation.md` are not restored: they describe the
internals of packages that are gone, and a design document for deleted code is
an invitation to go looking for it. They are at `git show 6b30f24^:pkg/design.md`
and `git show 6b30f24^:pkg/explanation.md`.

## What "the requirements" turn out to be

Three things state what this platform should do, and they are different in kind.

| Source | What it is | How binding |
|---|---|---|
| [`docs/inherited/kosha-requirements.md`](inherited/kosha-requirements.md) | The inherited library's requirements: 11 functional groups, 7 non-functional. | **Historical.** Written for a library the platform deliberately does not use. Crossed off below anyway, because several of its lines are good requirements regardless of where they came from. |
| [`README.md`](../README.md) and [`docs/integrity-platform.md`](integrity-platform.md) | What the platform claims about itself: six properties, the service list, the Go/Rust boundary. | **Binding.** A claim in here is one a reader believes. |
| `web/` and `mobile/` | The requirement in executable form: 25 procedures the supervisor's console and the collection bench actually call. | **Most binding of the three.** Everything here is something somebody was promised on a screen. |

---

## Source 1 — the inherited library's requirements

The platform meets some of these, deliberately does not meet others, and owes a
few. The distinction is the whole value of the exercise, so each row says which
of the three it is.

**Deliberate divergences** are not gaps. This platform is not the library the
document describes, and four of its choices are settled and documented
elsewhere: HTTP rather than gRPC, an outbox rather than Kafka, environment
variables rather than configuration files, and plain constructor wiring rather
than a dependency-injection framework.

### Database (FR1) and multi-tenancy (FR7)

| Req | State | Where |
|---|---|---|
| FR1.1 PostgreSQL via pgx/v5 with pooling | **Held** | `libs/integrity/tenantdb` |
| FR1.2 Multi-tenancy support | **Held, differently** — one database per service with row-level security on `app.tenant_id`, not a pool per tenant | `libs/integrity/isolation`, `tenantdb` |
| FR1.3 Transactions via Unit of Work | **Divergence** — explicit `pgx.Tx` in each repository. The audit entry is written inside the same transaction, which is the property Unit of Work was wanted for | every `internal/repository` |
| FR1.4 SQL injection prevention | **Held** — handwritten parameterised SQL throughout; no string-built queries | — |
| FR1.5 Generic repository helpers | **Divergence** — handwritten, on purpose | — |
| FR1.6 Configurable timeouts on all database operations | **Held.** The HTTP server bounds a request (`ReadTimeout` 30s, `WriteTimeout` 2m); `statement_timeout` now bounds one query at 60s and `idle_in_transaction_session_timeout` bounds a transaction that has stopped doing anything. Both are set on every pooled connection and overridable per DSN | `libs/integrity/tenantdb/limits.go` |
| FR1.7 Runtime field-name validation | **Not applicable** — no dynamic query builder, so there are no field masks to validate |
| FR7.1–7.2 Tenant from headers, propagated | **Held** | `tenantctx`, `X-Gavya-Tenant` |
| FR7.3–7.4 Per-tenant database routing and pools | **Divergence** — row-level security instead. A pool per tenant is a connection count that grows with the customer list |

### Logging (FR2), errors (FR6)

| Req | State |
|---|---|
| FR2.1–2.3 Structured Zap logging, levels, context-aware with tracing | **Held.** Request tracing arrived last; `libs/integrity/tracing` carries a W3C `traceparent` across every hop and the audit trail records it |
| FR2.4 No `fmt.Printf` in production code | **Held** in `services/` and `libs/`. `tools/amcu` prints to a terminal, which is what it is for |
| FR6.1–6.5 Structured errors, code mapping, wrapping, consistent shape | **Held.** `libs/integrity/connectjson`; codes chosen by cause rather than by procedure, which is what lets the alert rules count a 5xx as the platform's fault |

### Metrics (FR3) and tracing (FR4) — where most of what is owed is

| Req | State |
|---|---|
| FR3.1 Multiple metrics providers (Prometheus, OTel, Datadog) | **Divergence** — one hand-rolled Prometheus text exposition. A provider abstraction for one provider is an abstraction nobody has tested against a second |
| FR3.3 HTTP request metrics | **Held** — count, seconds, a duration histogram and in-flight, per procedure |
| FR3.2 Database operation metrics | **Held** — `gavya_db_queries_total`, `gavya_db_query_seconds_total`, `gavya_db_query_failures_total`, counted in the pgx tracer so they cover sweeps and boot checks as well as requests |
| FR3.4 Connection pool statistics | **Held** — seven numbers off `pgxpool.Stat()`, summed across the pools a process holds so the modulith reads correctly |
| FR3.5 Configurable backend | Divergence, as FR3.1 |
| FR3.6 Per-table, per-operation query metrics | **Owed**, and the least urgent. The per-table breakdown exists in the traces (a span is named `db SELECT collections`); as a metric it would need labels, which `observe` does not have |
| FR4.1 OpenTelemetry-based tracing | **Held in protocol, not in library** — W3C trace context and OTLP/HTTP JSON export, written directly rather than through the OTel SDK |
| FR4.2 Jaeger, Zipkin and OTLP exporters | **Partly** — OTLP only. Both of the others accept OTLP |
| FR4.3 Automatic spans for database operations | **Held** — a span per query via pgx's `QueryTracer`, named from a bounded vocabulary rather than the SQL text |
| FR4.4 Context propagation across services | **Held** — `svcclient` injects on every call |

FR3.2, FR3.4 and FR4.3 were one gap seen from three angles: **the database was
the one thing in this platform nothing watched.** `deploy/monitoring/alerts.yml`
said so in its closing note and gave a reason not to close it from there — the
check beside those rules verifies that every metric an alert names is one a live
handler emits, and it cannot do that for an exporter nothing runs. That reason
covered the exporter and never covered these: they are read off a pool the
service is already holding, so a live handler does emit them.

Closing it turned up something larger than a missing metric, which is in
*Pending* below and in the roadmap: the pool sizes and the server's connection
limit had never been on the same page, and did not add up.

### Configuration (FR8), auth (FR9), server (FR10), DI (FR11)

| Req | State |
|---|---|
| FR8.1 Configuration from TOML/YAML/JSON files | **Divergence** — environment variables only. A twelve-factor deployment and a Kubernetes ConfigMap are both environment variables in the end |
| FR8.2 Environment overlay | Held, in the sense that it is the only layer |
| FR8.3 Validation | **Partly** — required values are checked at boot per service; there is no schema |
| FR8.4 Hot reload via observers | **Divergence** — a process restart is the reload, and the restart is watched (`ServiceRestartLoop`) |
| FR9.1–9.3, 9.5 JWT, permissions, user context, no hardcoded identity | **Held** — `identity-service`, `libs/integrity/authz`, 261 routes each naming the permission it needs |
| FR9.4 gRPC interceptor for authorisation | **Not applicable** — HTTP middleware, `authz.Guard`, installed by `serve` so a service cannot forget it |
| FR10.1 Multi-server (gRPC and HTTP) | **Divergence** — Connect unary JSON over HTTP only |
| FR10.2 Graceful shutdown | **Held** |
| FR10.3 Health checks | **Held** — `/healthz` unconditional, `/readyz` asks dependencies. Deliberately: liveness that consults the database turns a database outage into a platform-wide crash loop |
| FR10.4 CORS | **Held**, at the gateway, with an allow-list and no credentials echo |
| FR11 Uber FX, no Wire | **Divergence** — neither. Each `main` wires its own dependencies in about forty lines |

### Event bus (FR5)

**Divergence, and the most consequential one.** There is no Kafka. Money events
are written to `notification_outbox` in the same transaction as the change, and a
sweep delivers them. The reason is the property Kafka cannot give: a payable
approved and a message queued either both happen or neither does. An event
published to a broker after a commit is an event that can be lost between the two.

The cost of that choice is a sweep that can stop, which is a quiet failure — and
that is now watched (`NotificationOutboxStalled`, `OutboxDepthUnknown`).

### Non-functional

| Req | State |
|---|---|
| NFR1.1 Database operation timeouts | **Held**, as FR1.6 |
| NFR1.2 Pool sizing | **Held, at eight rather than the thirty this document asked for**, and the number is not the interesting part. Nothing set a pool size at all and nothing set `max_connections` either, so twenty-eight pools of `max(4, numCPU)` faced a server limit of 100 — 112 on the four-core machine the load test ran on, 224 on an eight-core host. Both numbers now stated, and their arithmetic is gated | `libs/integrity/tenantdb/limits.go`, `TestThePoolsFitTheDatabase` |
| NFR1.3 Minimal middleware overhead | **Held, and measured** — p50 3.4ms, p99 7.3ms at ten booths recording at once (`e2e/load_test.go`) |
| NFR1.4 Caching with TTL and LRU | **Divergence** — there is no cache. Nothing in the measured path wanted one |
| NFR2.1 No panics in production paths | **Held** |
| NFR2.2 Graceful degradation | **Held** — every ML call is advisory and the platform answers with the tier switched off; `/readyz` names the dependency that is down |
| NFR2.3 Retry on transient failures | **Held**, and narrowly: only retryable codes, and no retry by default, because repeating a call that already had an effect double-counts a collection |
| **NFR2.4 Circuit breaker** | **Owed** — not clearly needed. Retries are bounded and off by default, so there is no amplification to break. Recorded rather than done |
| NFR3.1 80% test coverage | **Not measured as a percentage.** What is measured is better and narrower: all 261 routes are in the permission table, the table is gate-tested against the routes each service actually registers, and each route is exercised end to end against a real database. See *what nothing checks* below |
| NFR3.2 Package documentation | **Largely held** — every `libs/integrity` package has a doc comment that says why it exists, not what it contains |
| NFR3.3 Duplication under 5% | **Not measured** |
| NFR6.2 gofmt | **Held**, gated |
| **NFR6.3 golangci-lint** | **Owed** — the gate runs `go vet` across all 35 modules and `govulncheck` in CI, and no linter beyond that |
| NFR6.4–6.5 Few dependencies, standard library preferred | **Held** — `libs/integrity` has no third-party dependency beyond pgx and Connect |
| NFR7.1 Go 1.25.4+ | Exceeded — Go 1.26.1 |
| NFR7.2 PostgreSQL 12+ | **Narrowed** — PostgreSQL 16. Forced row-level security and the schema history table are what it is built and tested against |
| NFR7.3–7.4 Kafka, gRPC versions | Not applicable |

---

## Source 2 — what the platform claims about itself

`docs/integrity-platform.md` states six properties and, unusually, states where
each one does *not* hold. Re-read against the code: the scoping is still
accurate, including the two rows that say the older ERP services do not hold a
property the integrity layer does.

One claim was out of date and has been corrected. `README.md` named 22 services;
there are 29. The seven it did not name are `balance`, `identity`, `laboratory`,
`material`, `procurement`, `production` and `settlement` — including the two
that matter most to a reader arriving at the repository: settlement is the money
path, and identity is how anything proves who it is.

---

## Source 3 — the clients, which are the requirement in executable form

`web/` is the supervisor's console: quarantine, identity mapping, slot conflicts,
mass-balance windows and runs, divergences. `mobile/` is the collection bench:
device registration, generation rolls, capture sessions, delivery and batch
delivery from an offline outbox.

Between them they call **25 procedures. All 25 are served, all 25 are in the
permission table, and all four of their packages are routed by the gateway.**

Nothing compared those two lists until now. The failure that was waiting is the
quiet kind: the clients are in TypeScript and Dart, in different modules, and the
Go route table, permission table and end-to-end suite all move together when a
procedure is renamed, because they are all written beside the Go. The console
would have got `not_found` on a screen a supervisor opens once a fortnight. The
bench would have got `not_found` on `DeliverRecord` and — correctly, by its own
design — kept every record in its outbox rather than treating it as delivered.
Nothing lost, nothing counted, until somebody telephones.

`services/gateway-service/handler/clients_test.go` compares them now, on every
run of the gate.

What that test does **not** check is field names. The bench's closed
vocabularies were verified by hand and agree — `ACCEPTED`, `DUPLICATE_REPLAY`,
`QUARANTINED`, session status `OPEN`, and the `records` shape of a batch — but
by hand is not the same as gated, and a renamed JSON tag would put the bench's
outbox into `unrecognised`, which is the state it treats as *not delivered*.

---

## Done since this was written — the database

Items 1, 2, 3 and 5 of the original list, and closing them turned up a defect
none of them named.

**The pools and the server limit had never been on the same page.** `pgxpool`
sizes a pool at `max(4, numCPU)` unless told otherwise, and nothing told it
otherwise. PostgreSQL allows 100 connections unless told otherwise, and no
deployment here told it otherwise. Twenty-eight services open a pool against the
same `dairy` database, so the platform's ceiling was 112 on a four-core host and
224 on an eight-core one, against a limit of 100. Neither default was wrong on
its own; nothing anywhere multiplied them. What that looks like when the morning
arrives is `FATAL: sorry, too many clients already` from whichever services ask
last.

Now: `tenantdb.MaxConns` is eight and `tenantdb.Headroom` is forty, both compose
files declare `max_connections=300`, and `TestThePoolsFitTheDatabase` does the
arithmetic on every run of the gate. The Kubernetes shape points at a PostgreSQL
this repository does not deploy, so that one is documented rather than checked —
a test comparing a manifest against a limit nobody here controls would pass while
the cluster ran out of connections.

Alongside it: a statement timeout and an idle-in-transaction timeout on every
pooled connection, both read back off a real server rather than off a config
struct; seven pool gauges and three query counters on the same `/metrics`
handler as everything else; three alerts; and a span per query, named from a
bounded vocabulary — `db SELECT collections` — so a trace no longer stops at the
database.

Two checks had to be widened to see any of it, and both had the same shape of
blindness. The alert-name scanner walked only `services/`, so an alert naming a
metric published from `libs/` would have read as an alert on a metric nothing
emits — the exact failure that check exists to catch, arriving through the check
itself. And the gate's `dbintegration` scan walked `services` and `tools` but not
`libs`, so the suite that asks the server what it actually applied would never
have run.

## Pending, in the order I would do them

1. **Field names in the client contract.** The procedure names are gated; the
   payloads are not, and the bench's outbox is what depends on them.
2. **`golangci-lint` in the gate.** Cheap; likely to find little, given `go vet`
   across 35 modules and the mutation testing, which is why it is here and not
   higher.
3. **A check that the end-to-end suite still covers every route.** 261 of 261 was
   true when it was counted by hand. Nothing recounts it, so a route added
   tomorrow is covered by the permission table and by nothing else.

Not on this list, and unchanged: the platform has never been deployed anywhere
and has no real users or real data, and three things are blocked on somebody
outside this repository — one real AMCU export file, one analyser bench capture,
and twenty conversations with people who would buy it.
