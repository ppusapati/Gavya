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
| FR3.6 Per-table, per-operation query metrics | **Held**, and it was the last one owed. `gavya_db_table_queries_total{table,operation}` and its seconds and failures, counted by the same parser that names the span. `observe` grew a `LabelledCounter` for it, with a ceiling — labels are how a metrics endpoint becomes an outage of its own, so it emits at most `MaxSeries` and publishes how many it left out |
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
| NFR3.1 80% test coverage | **Not measured as a percentage.** What is measured is better and narrower: all 261 routes are in the permission table, the table is gate-tested against the routes each service registers, and every one is now *counted* as exercised end to end — the suite asks each service what it served and compares, rather than somebody counting by hand |
| NFR3.2 Package documentation | **Largely held** — every `libs/integrity` package has a doc comment that says why it exists, not what it contains |
| NFR3.3 Duplication under 5% | **Not measured** |
| NFR6.2 gofmt | **Held**, gated — and the gate's own list had omitted `pkg` since it was written, which is how one file stayed unformatted without anything saying so |
| NFR6.3 golangci-lint | **Held** — five linters, chosen one at a time for what each has found here, configured in `.golangci.yml` with the reasons beside them. The gate runs it per module and skips by name where it is absent; CI installs it, and a test fails the build if that step is removed |
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

The payloads are compared too, now. Every field name the console declares
against the JSON tags of the four services it calls; every field the bench sends
or reads against ingestion-service's alone; and the bench's Connect error
vocabulary against the platform's, including that its fallback is `unknown` —
because the outbox decides whether a record may be sent again from that code, and
a fallback of `unavailable` would make every unrecognised refusal look like
something worth retrying.

What that does **not** check is which message a field belongs to. Doing it
properly means resolving nested types across two languages; a field moved between
two messages of the same service would pass. The failure worth catching is a
renamed tag, and that is caught.

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

## Then the other three

**Field names in the client contract** — above.

**A check that the end-to-end suite still covers every route.** It counts now,
and it counts at runtime: every service already tallies what it served, per
procedure, so the suite asks each of them and compares the answer against the
permission table. Reading the suite's own source for call sites was tried first
and was worse than useless — it reported twenty-one routes uncovered and nineteen
of them were covered, by a constant with a digit on the end, by a method name in
a loop variable, by identity-service's own tests.

The counting version then reported three, and **one was real**: ingestion's
`ListSessions` had never been called by anything. The other two were covered on
the ML-enabled platform, which the count was not asking — the same failure
arriving inside the check itself. All three are covered now, from the plain
platform, so they stay covered on a machine with no Rust toolchain.

**`golangci-lint` in the gate.** I said it was likely to find little. It found
seventy, of which about ten were real:

- A profile written to a file whose `Close` was deferred and unchecked. A
  buffered write is not on disk until the close succeeds, so the tool reported a
  draft written that might not be there.
- Two dead error helpers, one of them a second way of reporting an invalid
  argument that the service already reported another way.
- An ineffectual assignment in the tracing middleware.
- `pgxpool.BeforeAcquire` — deprecated, and the reason matters: it answers with a
  bool, so a failure to set the tenant destroys the connection and retries, which
  under a database refusing `SET` is a pool that churns silently. `PrepareConn`
  answers with a bool *and* an error, so the connection goes back and the query
  fails with the reason. Changed.
- And the gate's own `gofmt` step listed `libs services e2e tools` — not `pkg` —
  directly above a comment saying pkg is gated like everything else. One file had
  been unformatted for as long as it had been in the repository.

The rest were the conventional discards, and they are named one by one in
`.golangci.yml` rather than silenced in a block.

## What building the console found

Writing a client for every service is a different exercise from testing every
service, because a client has to decide what to tell a person a button does.
Three procedures turned out to promise more than they do. None of them is a bug
in the sense of a wrong answer; each is a name that describes work nobody wrote.

**`reporting.v1/RequestReport` requested nothing.** It wrote a row with status
`pending`, and there was no worker, queue consumer or runner anywhere in this
platform that picked one up. `report_schedules.next_run_at` was a column that
nothing computed and `is_active` a flag nothing read. **Both runners are now
built** — see the section below — so this paragraph describes what was found
rather than what is true.

**`file.v1/GetDownloadURL` returned no URL.** It concatenated the configured
bucket with the stored name and returned the result: nothing signed it, nothing
checked the object was there, and a browser could not fetch it. The same was
true of `reporting.v1/GetReportDownloadURL`. **Both are signed links now** —
see the section below — so this paragraph describes what was found rather than
what is true.

**file-service never sees a file.** Its five procedures are a register of records
about files something else stored — there is no multipart route and no
presigned-upload procedure. So the screen has no file picker. An upload box there
would offer something the platform cannot do and would fail at the moment
somebody relied on it.

The fourth finding was in the gate rather than the platform.
`clients_test.go`'s mobile side globs its whole api directory, with a comment
saying why: a second client file is a thing somebody adds, and a list goes on
passing without it. Its web side read `index.ts` alone. Three sub-facades had
since been added — about a hundred and fifty procedures — and the comparison saw
none of them, so renaming any of those procedures on the Go side passed. Both
sides glob now, and a test asserts every console file making a call is one the
comparison read, because narrowing the glob again would simply stop those calls
being compared rather than fail anything.

## The last thirty-one

The five services the console was originally built for were the last to be
finished, which is not the order anybody would have chosen. Thirty-one
procedures had no caller, and two of them mattered more than their size suggests.

**`SignOut` had never been called.** The console's sign-out cleared the browser
and told the platform nothing, so a session stayed valid until it expired —
including one abandoned on a shared machine. It now calls SignOut, clears the
local state either way (a person who asked to sign out must not stay signed in
because the network was down), and says which of the two happened when the call
could not be made.

**Thirteen of identity-service's fifteen procedures had no client**, so a
co-operative was authorised against roles nobody could be given: adding a
person, setting a password, assigning a role and issuing a machine credential
were all rows somebody typed into a database console.

The rest were the write side of the integrity spine and the questions its
reviewing screens never asked — what a window's instruments can establish before
anything is reconciled, everything one external identifier has ever meant rather
than what it meant at one instant, the payload of a quarantined record rather
than its hash, and the three calls that put a settlement pair in front of the
classifier at all.

Coverage is now 261 of 261 procedures across 28 of 28 services, and
`TestEveryProcedureThePlatformServesHasAClient` holds the line in the other
direction: a route added to `authz.Table` without a client fails at the moment
somebody adds it, rather than at the moment somebody wants to use it.

## The report runner and the schedule runner

Built, and the two procedures above now do what their names say.

**Where a report goes.** Into the row, as bytes, rather than onto a disk. There
is no object storage in this platform and no volume shared between replicas, so
a file written by one pod is one the next request cannot read — and the
procedure that was supposed to hand it over returns a path no browser can
fetch. `GetReportContent` is the new procedure that actually delivers a report;
`GetReportDownloadURL` still answers with a locator and is still not a URL.

**What a report can be.** A declared catalogue, served by `ListReportKinds` so
that the list a person chooses from is the list the runner reads. Three types,
each drawn from a procedure another service already serves: `collections` from
procurement, `settlement_summary` from settlement, `divergences` from shadow
settlement. Nothing reimplements anybody's query — a collections report shows
what procurement priced, as procurement priced it, because two implementations
of the same rate card would eventually disagree and the disagreement would
surface as a report differing from the slip a farmer was handed.

A type nobody wrote is refused by name, with the catalogue in the refusal. A
type whose source this deployment was never told how to reach fails with the
setting named. Both alternatives — an empty file marked complete — are the
defect this platform keeps finding in itself, and an empty collections report is
indistinguishable on a screen from a period in which nobody delivered any milk.

**There is no yield report**, and that absence is the point. A yield needs a
density to convert between litres and kilograms, production-service refuses to
assume one, and a runner working at seven in the morning has nobody to ask. The
only unattended yield report this platform could produce is one resting on a
figure nobody measured, so there is no such type rather than a type that quietly
picks 1.03.

**Timezones.** A schedule carries its own, required, with no default. Seven in
the morning is seven where the society is; evaluated in UTC a schedule set in
Maharashtra fires at half past twelve in the afternoon and its "yesterday" is a
day that ended five and a half hours before the one everybody means. The cron
parser was written here rather than taken from a library for exactly that: the
answer has to be in a named zone, and `time/tzdata` is embedded because these
images are distroless and carry no zoneinfo — without it every named zone fails
inside the container and succeeds on every developer's machine.

Two bugs found by writing tests against that parser. Cron's day rule ORs the two
day fields when both are restricted, which is the opposite of what the
expression looks like it says. And a walk that rebuilds a wall-clock time does
not always move forward: in `America/New_York` — but not in `Europe/London`,
because Go does not guarantee which of a repeated hour a rebuild resolves to —
the walk alternated between two instants until its five-year bound gave up. The
schedule would have fired correctly all year and reported, on one morning in
November, that it does not fire at all, and only for societies in some zones.

**A scheduled report's period moves with the firing.** A schedule carries a
window — `yesterday`, `last_7_days`, `last_month` — rather than dates, because
fixed dates would produce the same report for ever, which is wrong in a way
nobody notices until they compare two of them. A type that needs something a
schedule cannot supply is refused when the schedule is written rather than at
two in the morning: `settlement_summary` names a cycle, and a sweep has no way
to know which one is meant.

**What happens when it goes wrong.** A failure that no retry can fix stops at
once and records why; one that might pass next time is retried three times and
then stops. A report left in `running` by a process that died is put back by a
reaper — without it the queue quietly loses whatever a restart was holding, and
the person who asked watches a report being worked on by a process that no
longer exists. A schedule always moves on whether or not its report could be
created, because leaving `next_run_at` where it is turns one missing report into
a hundred failed ones an hour.

Both sweeps publish staleness gauges. A runner that has stopped leaves every
request correctly written down and nobody producing any of them, and nothing
else in the platform looks wrong — which is the state reporting-service was in
before this.

The gate caught two things on the way. The egress policy check failed the moment
reporting-service gained upstreams, which is what it is for. And the audit
coverage check refused five new writes until each said why it needed no
before-image; four are machine bookkeeping, and the fifth now writes a trail,
because a schedule that stops producing its report is noticed weeks later.

## Signed download links

Built. Both procedures now return a link a browser can follow and a person can
send on.

**Not a presigned S3 URL.** This platform has no object storage, and a presigner
for a bucket nobody has would be a large amount of code that could not be run.
The service holding the bytes serves them, and the token is what lets it answer
a request with no session behind it — which is the whole problem: a browser
following an `<a href>` sends no Authorization header, and neither does curl or
whoever the link was forwarded to.

**Three bindings and an expiry**, in `libs/integrity/signedurl`. A token names
its purpose, its tenant, the one resource it may fetch and when it stops
working, and the HMAC covers all four. Purpose stops a report link fetching a
file — which matters most in the modulith, where the two services are one
process and may share one key. Tenant is the authority the request acts under,
taken from the verified token and never from a header, because on a request that
reached the service without a session every header is whatever the caller typed.
A link lasts fifteen minutes by default and a day at most, and a caller asking
for longer is refused rather than quietly given less.

**The gateway exempts two exact paths**, not a prefix. A prefix test is one `..`
away from being wrong — this middleware runs in front of the whole mux in the
modulith, where it can see a path the router has not cleaned, and
`/download/../cattle.v1.CattleService/ListCattle` has the prefix and names a
procedure.

**file-service is the interesting half**, because `stored_name` is the caller's
word for where something else put a file, and this code turns it into a path it
opens and streams to a browser. Two defences: the name must be a plain base
name, and the resolved path must still be inside the store after symlinks are
followed. The second is the boundary — a plain name is a plain name, and a
symlink called `invoice.pdf` pointing at `/etc/shadow` passes every check on the
name. Containment is compared component-wise rather than as a string prefix,
because `/data` is a prefix of `/database`.

It also only issues a link when the object is really there. A record can outlive
its object — this service records where something else put a file and never
receives one — and a link handed out for one of those fails after somebody has
emailed it, which is the worst moment to find out.

**What a signed link is, and the documentation says so**: a bearer credential in
a place that gets written down. It appears in browser history, in a proxy's
access log, and in whatever it is pasted into. Signing stops it being forged or
widened; it does not stop it being copied, and nothing can. The console says
this next to every link it shows. There is no revocation short of rotating the
key, which cancels every link signed with it — the keyring verifies against a
previous key so a rotation does not break the links already sent.

One property is pinned by reading the source rather than by behaviour: the
signature comparison is `hmac.Equal`. A test cannot tell that from `==` — both
refuse the same tokens and return the same errors, and the difference is a
timing signal — so the check reads the file with its comments stripped, because
a check searching the whole file would be satisfied by the paragraph explaining
it.

## Continuous integration has never run

Worth writing down, because it is the kind of thing that gets rediscovered.

`.github/workflows/check.yml` has been in this repository since 14 September
2026 and has produced thirty runs. Every one failed within a few seconds and not
one executed a step. The API is unambiguous about why:

| | |
|---|---|
| `runner_id` | `0`, on every job of every run |
| `runner_name`, `runner_group_name` | empty |
| job logs | HTTP 404 — there are none |
| check run output | empty |
| billable time | `total_ms: 0` |

Zero billable milliseconds is the one that settles it. The jobs were created,
marked started, and failed without a machine ever being assigned. Nothing ran,
so nothing was charged.

**This is not a defect in the workflow.** The YAML is valid — an invalid one
fails as `startup_failure` and says so — and `ubuntu-24.04` is a current
GitHub-hosted label. The workflow is also not obviously wrong on any axis that
could be checked statically, which is as much as anybody can say about a file
that has never executed.

It is an account-level refusal. This repository is private and on a personal
account, so its Actions minutes bill against the account's allowance; spent,
with no spending limit raised or a payment method declined, GitHub accepts the
workflow, creates the jobs, and gives them no runner. The remedy is a spending
limit, a payment method, making the repository public, or a self-hosted runner
— a decision rather than a lookup, and the reasoning for each is in the header
of the workflow file where somebody looking at a red tick will find it.

Two consequences worth stating rather than leaving implied.

**The gate's record is a local one.** Every "gate green" in this repository's
history means the script passed on one machine — the same machine that wrote
the change. That is a weaker claim than a green tick, and it is the claim being
made.

**No image has ever been built.** The `images` job exists because `go build` in
the gate stood in for building an image and the two are not the same claim, and
that job has never run either. `services/modulith/Dockerfile` was found broken
by reading it rather than by building it. The rest are in the same position:
read, and not run.

## Pending

Nothing in the code from this cross-check.

Unchanged: the platform has never been deployed anywhere and has no real users or
real data, and four things are blocked on somebody outside this repository — an
Actions allowance this account will spend, one real AMCU export file, one
analyser bench capture, and twenty conversations with people who would buy it.
