# State of the platform, and what is left

This document exists because the plan did not.

Until now the roadmap lived in a conversation. Everything in `docs/integrity-platform.md`
describes what the platform *is*; nothing described what had been decided, what had
been deliberately deferred, or what somebody picking this up on a Monday should do
next. That is a gap of the same kind the platform spends its time refusing
elsewhere: a state nobody wrote down is a state that has to be guessed at.

Two things to know about how to read it:

- **The "built" section is verifiable.** Every claim in it can be checked by
  running something, and the command is given where it is not obvious.
- **The "history" section is not, entirely.** The work was done in phases, and the
  boundaries between the early ones are recorded nowhere but in the commit log.
  Rather than invent crisp phase numbers after the fact, it says what was decided
  and leaves the sequencing loose where it genuinely is.

---

## What is built

Twenty-nine Go services and a thirtieth binary that is all of them, four Rust ML
services, one shared Go library and one shared Rust crate.

```sh
# everything: eighty-three steps, in the order that fails fastest
TEST_DATABASE_DSN="postgres://user@host:port/%s?sslmode=disable" \
  bash scripts/check-all.sh
```

That script is the whole gate and it is worth reading rather than trusting. It
formats, vets and tests every module in the workspace; builds and tests the Rust
tier and drives it through the real typed client; provisions one database and
runs the repository suites that carry a build tag, which a plain `go test`
cannot see; and finishes with the end-to-end suite, which builds the real
binaries and talks to them over real HTTP. Every step uses `-count=1`, because
several tests read files the Go test cache does not track — compose, the
Kubernetes manifests, the schemas — and a cached pass on those is a check that
appears to run and does nothing.

The `%s` in that DSN is not decoration — it is where each service's own database
name is substituted. A DSN without it silently puts every service in one database,
and the suite still passes, which is worse than failing.

### The integrity spine

`ingestion` → `observation` → `canonical` → `pooling` → `shadow-settlement` → `balance`.

Described in full in [`integrity-platform.md`](integrity-platform.md). In one line:
the platform recomputes an incumbent system's settlements from the same
collections and reports every difference with a reason, without ever paying from
its own number.

### Commerce

- **`procurement-service`** — rate cards, chart pricing, priced collections and
  corrections. A correction re-prices from the readings under the card in force on
  the collection day, never from the old rate; the superseded version stays.
- **`settlement-service`** — cycles, producer payables, deductions, recoveries,
  payments, and the producer statement as a rendered fixed-width document rather
  than JSON. Adjustments are a distinct kind of payable, so recovering an
  overpayment is expressible without a settlement going negative.

### The physical layer

- **`material-service`** — nodes (a cooler with a code, a tanker with a
  registration), movements measured at both ends, and instruments with
  calibration and uncertainty.
- **`balance-service`** — the mathematics material-service supplies the model for.
  Classifies every measured flow as observable, unobservable, redundant or
  just-determined, using graph bridges. A meter with an unmeasured bypass is
  reported as unchecked, because the bypass absorbs any error.

### Quality

- **`laboratory-service`** — samples, chain of custody, results. A result is fit to
  price milk when its sample was sealed, its custody is unbroken and the
  instrument was in calibration. Results that fail are still recorded, with the
  reason, rather than refused.

### Genealogy

- **`production-service`** — batches, inputs, recall traversal in both directions,
  and formulations. The property the service exists for: a recall that stopped
  early does not look like one that finished. Raw milk is a batch like any other,
  so the genealogy does not stop at the plant gate.

### The ML tier

Four Rust services in `ml/` — anomaly, uncertainty, divergence and reconciliation
— over the shared `mlcore` crate. Every call is advisory; every caller has a
complete deterministic answer without it.

**Off unless a deployment turns it on, and every deployment now does.** Each Go
caller reads its endpoint from an environment variable that defaults to empty and
logs at boot that the tier is off, so the platform is complete without it. Both
compose files and the three callers' Kubernetes manifests wire all four URLs, and
a check keeps the two in step — it does not require the tier to be enabled, only
that the descriptors agree, because Kubernetes once wired one of four and nothing
anywhere said whether that was a decision.

### Cross-cutting

- **Tenant isolation** in the database, not in every query. `tenant_id` on every
  table, RLS with FORCE, and a role that cannot bypass it.
- **A hash-chained append-only audit** written inside the caller's transaction, so
  a change and its record land together or not at all — **used by twenty-five of
  the twenty-nine services**. It was six when this document was first written.
  The four that write nothing are `audit` itself, which is the trail; `gateway`,
  which proxies and decides nothing; `notification`, whose mark-as-read is one
  person's own mail and not a decision anybody disputes; and `feed`, which is the
  one place the absence is a judgement rather than an argument. Every update that
  overwrites a figure somebody could be asked to defend now records what it
  overwrote, and the test that says so refuses an exemption that does not carry
  its reason.
- **Reference decisions.** Every reference-shaped column that carries no foreign
  key is decided by a person and recorded in
  `libs/integrity/isolation/references.sql`, with the reason. The deploy fails
  rather than warns on an undecided one.
- **Exact money, exact quantities, exact measurements.** `libs/integrity/money`,
  `libs/integrity/quantity` and `libs/integrity/exact`. **No float touches a
  recorded figure anywhere in the platform**, which is a stronger claim than this
  document could make when it was written: then it held on the integrity path
  and nowhere else. A quantity on an invoice line, a tax rate, a kilogram of
  feed, a litre of milk, a fat percentage, the count on a shelf, the size of a
  pack, an animal's weight and an observation's value were all float64 from the
  wire to the column and back. What remains float is the output of floating-point
  computation in the Rust tier, and only that.
- **Authorization**, in `libs/integrity/authz`. Seven roles, a permission that is
  a domain and an action, and a table naming the one permission each of the 261
  procedures requires. It fails closed: a procedure absent from the table is
  refused, and the test in the package fails the build rather than waiting for a
  route to be added without one.
- **One server every service runs**, in `libs/integrity/serve`. Timeouts, the
  authorization check, a per-caller rate limit, readiness, liveness and metrics
  are properties of that function rather than of twenty-nine main functions that
  each had to remember.

---

## How this platform decides things

Two habits are worth stating, because they explain a lot of the code that would
otherwise look over-careful.

### A default that looks like a decision

Wherever the platform would have to pick a number nobody measured, it refuses and
says so instead. A deduction policy, a recovery priority, a milk density, a
rounding mode, a statement width, an instrument's uncertainty, a process yield, a
share tolerance — each is supplied by whoever knows it, or the answer comes back
unavailable with the reason attached.

The alternative is not neutral. A density of 1.03 quietly applied to every
litres-to-kilograms conversion is a three per cent error in every yield in a
plant, and it reads as a process problem rather than an arithmetic one.

Where the platform genuinely cannot know something but the answer is findable, it
builds the way to find it rather than guessing. `GetObservedYield` is the clearest
case: it does not know what a process should yield, so it shows a plant its own
vats — count, range, median, quartiles — and lets the plant declare its own target.

### Controls that report success while doing nothing

Repeatedly found, usually in this platform's own checks:

- The reference-decision check was blind to 70% of columns because it only
  considered names matching a table name.
- The gateway's upstream-coverage test checked that every listed path routed, but
  never that every upstream appeared — two services had been added without
  routing while it passed.
- `Net + Deducted == Gross` was asserted as an invariant. It is an identity; it
  cannot fail.
- A referential-actions test counted a magic number of `ON DELETE CASCADE`s.
  Adding a legitimate one broke it, which told nobody anything except that the
  number had moved.

The discipline that catches these is **mutation testing**: reintroduce the defect
an assertion guards and confirm the assertion fails. A test that still passes was
not testing what its name says. This is applied to every load-bearing assertion,
and it has caught more real problems than reading the code did.

---

## Open work

In the code, none. The cross-check in
[`docs/requirements-crosscheck.md`](requirements-crosscheck.md) read the platform
back against the three things in this repository that state what it should do and
turned up seven items; all seven are closed, in *Two defaults that multiplied* and
*Counting what the suite actually called* below.

What is open is that **this has never run anywhere**, and that is not
a small remainder — it is most of the distance to production. Setting it out
honestly, in the order it would hurt:

- **It has never been deployed.** Not to a cluster, not to a single host. No
  image has been built, because no Docker daemon runs in the environment this was
  developed in; the build lines were run directly instead, which is weaker.
- **No real user has ever touched it**, and no real data has ever passed through
  it. See *Blocked* below, which is the same point from the other end.

The operational gaps that were open alongside these are closed, and how is in
*What it takes to run this*: backups with a restore that is tested rather than
believed, a migration path for a database that already exists, the secrets the
deployments always referenced and nothing produced, a pipeline that runs the gate
so it no longer depends on being remembered, and — in *The morning, measured* —
the first figures anybody has for what this platform does under load, and
something that reads the metrics it has always published.

The blind spots that were named here are closed. The two quiet failures are in
*The two quiet failures*, and the database — which was the last one — is in *Two
defaults that multiplied*. What is left unwatched is written at the foot of
`deploy/monitoring/alerts.yml` and is now narrower than it was: what the database
itself is doing, meaning replication lag, disk, checkpoints, locks. The usual
exporter covers those, no deployment here runs one, and alerting on its metric
names is deliberately not done from this repository, because the check beside
those rules cannot verify a name belonging to an exporter that is not running.
Connections were on that list and should never have been: they are read off a
pool the service is already holding.

One caution about everything below. It is thorough and it is self-validated:
every check in this repository was written by the same process that wrote the
code it checks, against assumptions nobody outside has tested. The concrete
reason to hold that loosely is that tests which could not fail keep turning up —
an assertion placed after the state it meant to test, a check made redundant by a
second one, a coverage figure counted by a grep that cannot count it. Each was
found by mutation testing. The rate of finding them has not reached zero.

Everything below is closed. It is kept because the reasoning is the expensive
part and it is not visible in the diff — which is this document's whole job.

### 1. The ML tier — **connected**

`e2e/mlharness_test.go` starts the four Rust binaries and a second copy of the
three Go services that call them, so both halves run in one suite:

- The main platform still has no ML tier, and still proves the platform is
  complete without one. That was never the gap.
- A second platform has the tier connected, and `e2e/ml_test.go` asserts that
  connecting it *changes the answer* — an observation that came back with its
  uncertainty marked missing now carries a real estimate with a coverage factor
  and a model version on it. Verified by mutation: stop the harness passing the
  URLs through and the test fails with "the tier was connected, answered, and the
  observation still says its uncertainty is missing".

It skips, loudly and by name, where `cargo` is absent. A skip is honest; running
the disabled-tier tests again under an ML-sounding name would not be.

The encoding traps — a non-finite float arriving as zero, insufficient evidence
arriving as a retryable 5xx, a model pin ignored — stay in
`libs/integrity/mlclient/contract_integration_test.go`, which drives the real
binaries through the real typed client. That is the right place for them and
duplicating them in the e2e suite with hand-rolled requests would be a second,
worse copy. It is behind the `mlintegration` build tag because it needs
`cargo build` first, not because it is optional, and `scripts/check-all.sh` runs
it.

The deployment descriptors disagreed about it, too. compose wired all four ML
URLs; Kubernetes wired one of four and left three empty, with nothing anywhere
saying whether that was a decision — so the anomaly tier would have run under
compose and silently not under Kubernetes, and an observation scored in testing
would be unscored in production. Now consistent, with a check that keeps them so:
it does not require the tier to be enabled, only that the two files say the same
thing. Emptying a URL remains the supported way to turn a tier off.

### 2. The seven older services — **covered**

`billing`, `breeding`, `cattle-market`, `feed`, `inventory`, `notification` and
`product-catalog` are in the e2e harness, so all eighteen services now start and
answer a health check in one suite run — which nothing had ever shown for these.

**All twenty-six startable services are now in the harness**, not the eighteen
that first closed this item — adding the seven left eleven others unstarted by
anything, which made "every service answers health" a claim about eighteen of
twenty-nine. `gateway` is covered by its own routing tests, `audit`'s schema is
applied to every database here and its tamper-evidence is driven directly, and
`identity` starts its own binary because what that test checks is the refusal to
start. The harness list says so beside each.

Extending it to `cattle`, `farm` and `reporting` found a real defect immediately.
**An animal recorded without a breed could be created and never read back.**
`breed_id` is nullable and the insert wrote NULL for an empty one, which is right
— an absent optional reference should be NULL, or the foreign key does not mean
what it says. But every SELECT read it into a plain Go string, and pgx cannot
scan NULL into one. `owner_id` and `farm_id` were written the same way. Worse,
`ListCattle` scans the same columns, so one unclassified crossbred cow made the
whole tenant's list fail — the failure was not confined to the row that caused
it. Fixed by coalescing in the SELECTs, which is the exact reverse of what the
insert does, and there is now a test that says so on purpose rather than finding
it by accident.

`e2e/erp_test.go` writes a row as one tenant and asks as another, for each of
them. What that proves is that the service's own query is scoped: the harness
connects as a superuser, and a superuser bypasses row-level security, so RLS is
not what is being tested there. That is deliberate and stated in the file — the
query layer is the one found wrong twice in hand-written SQL and the one nothing
was checking for these seven; RLS is the backstop and `isolation_test.go` proves
it separately as `gavya_app`. Verified by mutation: replacing feed-service's
`tenant_id = $1` with a tautology makes it fail with "a second tenant sees 4
rows through feed-service, and it wrote none".

**The money question turned out to be more precise than "float64 is bad".**
Measured rather than argued:

- Through `NUMERIC(12,2)`, 200,000 values across the column's whole range
  round-tripped `float64 -> JSON -> float64` losing nothing. Go formats a float64
  with the shortest representation that round-trips, so the decimal survives.
- But five services widen their money columns to `NUMERIC(18,4)` at deploy, so
  one schema serves a yen deployment and a dinar one. At four decimals float64
  is exact only below about 10^11. Below that, nothing lost across 200,000
  values; above 10^12, 151,682 of 200,000 lost a digit — 75.8%.
  6791947779410.3551 comes back as 6791947779410.3555.

  (This read 100,000 while every other copy of the same measurement — the
  schemas, `integrity-platform.md` — read 200,000. Re-measured rather than
  reconciled by picking one: 200,000 is the figure, and the "more than three
  quarters" everywhere else is 75.8%.)

So the column could hold values the code could not carry and nothing said so.
The schemas were made to bound every four-decimal money column to what float64
carries exactly, refusing the value rather than mangling it on the way out —
where both ends would believe they agreed.

(That bound is gone now, and the rest of this section is the record of why. The
read path is exact to the full width of the column, so a ceiling drawn where
float64 stops describes nothing. Read on.)

One usability defect fell out of writing those tests: `ListNotifications`
compared channel and status unconditionally, so a caller passing neither — which
the request type invites, since neither is required — got an empty list back.
An empty list is indistinguishable from a tenant with no notifications, so the
obvious call returned the obviously wrong answer and looked right doing it. An
unset filter now means "not filtering by that".

Following that up turned out to change the recommendation. There is **no float
arithmetic on money anywhere** in these services: every write goes through
`exact.NonNegativeDecimal`, which refuses a value finer than the column holds;
comparisons are against zero; and order and billing compute their totals in SQL,
in the column's own type. So the wire refactor would buy robustness, not
correctness.

What was wrong was that the two layers disagreed about the ceiling — Go accepted
eighteen digits and the database refused anything above eleven, so a price
somebody typed came back as a constraint violation rather than a sentence saying
what was wrong with it. `exact.MoneyPrecision` and `exact.MoneyCeiling` were
added to name the decision once, with the measurement behind it.

Both are gone now, along with every `CHECK` they matched. The read path is exact
to the full width of the column, so the ceiling describes nothing — and a
constant kept alive only by tests comparing it with itself is a control that
reports success while doing nothing. The measurement survives in the comment on
each schema's drop statement, where it says why the `CHECK` was there and why it
is not any more.

**Closed, and the reasoning above was half right.** All five services now read
money as exact decimals: `libs/integrity/money` in the domain, decimal literals
on the wire, `::numeric` in every statement, and the `CHECK` ceilings dropped
because a limit of the Go read path does not belong in the database.

The recommendation against doing it rested on "buys robustness, not correctness",
and that held right up until the change was attempted. Three defects surfaced
that the old shape had been hiding, none of them about floats:

- **PostgreSQL renders a rupee in a `NUMERIC(18,4)` column as `"42.5000"`.**
  Reading that at the currency's two decimals fails, correctly, because `Parse`
  never discards precision. The column's padding has to be told from the amount's
  precision, which is what `money.ParseStored` does — refusing rather than
  rounding if a non-zero digit would be dropped. The e2e tests caught this; a
  four-decimal currency alone would not have.
- **`cattle-market`'s `PlaceBid` and `RecordSale` had never worked**, for any
  caller, since they were written. Both required a currency, neither request type
  carried one, and `currency.Normalise("")` refuses an empty code. Nothing
  noticed because the e2e file covered `CreateListing` and stopped there. Both
  endpoints have coverage now, and a bid takes the listing's currency.

  Two of them was enough to ask whether there were more. `e2e/reachable_test.go`
  parses every handler, finds the request type each endpoint takes and the
  service method it calls, and asks whether the request could satisfy what that
  method insists on — including the implicit demand a `currency.Normalise` makes
  without saying so, which is the shape that hid this one. It reports 74
  handler-to-service calls and fails if it ever matches none, so it cannot pass
  by examining nothing. Run against the tree at `e3aa028` it names `PlaceBid` and
  `RecordSale`; run against the tree now it finds nothing, which is worth
  something only because of the first half of that sentence.

  What it cannot do is tell whether an endpoint does the right thing — only
  whether it is capable of doing anything at all. The real answer to that is
  end-to-end coverage. That was around 137 of 245 registered routes when this
  started and around 109 of 249 at the time this paragraph was written. Every
  route has since been given end-to-end coverage, over the run of commits whose
  messages begin "Cover", and the administration routes added afterwards are
  exercised in `onboarding_test.go`.

  The count is no longer tracked, because the paragraph below is right that grep
  cannot do it. Checking that claim while updating this document made the same
  mistake a third time: a search for each administration procedure found nothing
  and the routes are covered, for exactly the reason given below.

  "Around", because counting this by grep is unreliable in both directions and
  the first attempt got it wrong: identity-service builds its procedure name at
  the call site — `base + "/gavya.identity.v1.IdentityService/" + method` — so a
  search for the literal string reported its whole suite as uncovered when it is
  one of the better-tested services here. The figure is worth having as an order
  of magnitude and not as a number.

  What is worth having exactly is which of them matter. Every service now has
  coverage of its main write paths; what is left uncovered is mostly reads, plus
  the write halves of `farm`, `feed`, `file`, `notification` and `reporting` —
  none of which holds a figure anybody is paid against.

  The first answer, and the one that started this: **`ingestion-service` had no
  end-to-end coverage of anything.** The
  harness built it, waited for it to report ready, and never called it. Of the
  six properties this platform claims, replayable idempotent ingestion was the
  only one with nothing behind it end to end.

  `e2e/ingestion_test.go` covers it now — one milk analyser in a village
  collection centre, reflashed halfway through the fortnight:

  - A record redelivered nine more times comes back `DUPLICATE_REPLAY` with the
    same record id every time, and the session still holds one record. Ten
    concurrent deliveries of it admit exactly one, which is the case a phone
    firing its whole outbox on regaining signal actually walks into.
  - Two payloads under one sequence number are both held and neither chosen
    between, the quarantine names the record it collided with, and the session
    stops accepting — a sequence space that has issued one number for two
    readings cannot be trusted for the rest of the run.
  - A reflashed device restarts at sequence 1 and is admitted, which is the case
    generations exist for; the late arrival from the old epoch is held rather
    than dropped.
  - A closed session refuses new records and holds them, while records admitted
    before the close still replay cleanly. A device retrying its outbox must not
    be told its accepted records have gone.

  The admission rules already had thorough unit tests and those are the right
  place for the rule table — `Admit` is a pure function. What they cannot show is
  that the loading is right, so the mutation that matters is a wiring one:
  drop the sequence from the slot lookup so it never finds the record already
  there, and these tests fail while the unit tests do not. Three rule mutations
  are caught as well.

  **`milk-service` was the second, and it had a defect.** Nine routes, none of
  them called by a test, in the service holding the reading a producer is paid
  for. `GetDailyYield` discarded the error from parsing its date, so a malformed
  date became the zero time and the query summed the readings recorded on the
  first of January, year one — of which there are none. An empty date, a
  day-first date, an RFC3339 timestamp and the word "yesterday" all came back as
  zero litres. The caller was told the animal gave nothing, which is a fact
  somebody acts on, rather than that the request was malformed, which is a fact
  they can fix.

  A second defect in the same function turned out not to be one, and that is
  worth recording because it nearly went in as though it were.
  `recorded_at::date = $3` compares a date against a parameter; bound as a
  TIMESTAMPTZ it would promote the date to midnight in the session's timezone and
  compare it against midnight UTC, so a deployment in India would read zero for
  every animal on every day. It was reproduced in psql, the fix was written, the
  tests were written — and they passed against the unfixed query. PostgreSQL
  infers the parameter's type from the comparison and types it `date`, so pgx
  encodes a date and nothing is promoted. The psql experiment had an explicit
  `::timestamptz` literal typed by hand, which is not what the driver sends: a
  check of a statement nobody runs, which is the same failure as a control that
  reports success while doing nothing, one level up.

  The query change was reverted. The tests stayed, because the reliance on type
  inference was real and nothing said so, and they run in UTC, a zone ahead and a
  zone behind — one non-UTC zone would not do, since a query that shifted
  everything by a day would still pass under Asia/Kolkata alone.

  **The pattern across both is worth stating plainly.** Two services with no
  end-to-end coverage were examined and both were hiding something: two endpoints
  that had never worked in one, an error silently swallowed in the other.

  The other six are covered now too — `tenant`, `inventory`, `breeding`, the
  write half of `health` in `e2e/lifecycle_test.go`, and the four decision
  endpoints `canonical` and `balance` had left out in `e2e/decisions_test.go`.
  Those four are `ReverseResolve`, `RetireIdentity`, `ResolveConflict` and
  `AcceptRun`, and they have something in common: each is where a person
  overrides or accepts what the machinery worked out. The happy path is walked
  every day; the endpoint somebody reaches for when the answer is wrong is used
  rarely and under pressure, which is where a defect survives longest.

  **Nothing was wrong in any of them.** Three tests failed on the first run and
  all three were the test, not the service:

  - A tenant is created `pending` and activation moves it to `active`. Lifting a
    suspension is a promotion rather than an undo, which is worth pinning because
    the obvious assumption is the opposite.
  - `calf_gender` is `VARCHAR(1)` and the service deliberately accepts "female",
    "Female", "F" and "heifer" and stores one letter. The test had asserted the
    opposite of a feature whose comment explains exactly why it exists.
  - `AcceptRun` refuses a run that never converged — "a period cannot be closed
    on an arithmetic that never closed" — and converging needs the reconciler, so
    that test belongs on the ML platform.

  One real gap did come out of it, and it is small: **a superseded identity
  mapping is in the table and reachable from no endpoint.** `RetireIdentity`
  stamps `superseded_at` and keeps the row, which is the platform's supersession
  rule working. But `ReverseResolve` and `ListIdentities` both filter superseded
  rows and neither takes an as-of, so "which code resolved to this producer last
  March" cannot be asked through the service. The history is preserved and not
  published. `e2e/decisions_test.go` asserts the row survives by querying the
  table directly, and says in its comment that this is what it is doing — the
  distinction between what is stored and what is reachable is the whole point.

  Swallowing the error was cheap enough to look for everywhere, so it was.
  Across every service, one other discarded error was worth fixing:
  `settlement-service`'s `fromRecovery` threw away the error from
  `OutstandingAmount`. `money.Sub` refuses to subtract amounts in different
  currencies or at different scales and refuses an overflow, and on any of those
  the zero Money comes back — whose `String()` is `"0"`. A loan would have been
  reported to the caller as fully repaid.

  It is not reachable today: the repository builds `Principal` and `Recovered`
  from the same row's currency and scale, so only an overflow no real figure
  reaches is left. That is an argument for the error never firing rather than for
  discarding it. "This producer owes nothing" is the worst answer a settlement
  service can give, and it was set up to give it silently.

  The other four — `tenant`, `inventory`, `canonical` and `balance` — were read
  and nothing was found. `inventory`'s `AdjustStock` already does the movement,
  the new quantity and the refusal to go below zero in one transaction, with the
  arithmetic in the database; `tenant` refuses a currency it does not recognise
  rather than defaulting; `canonical` and `balance` are integrity-layer services
  with the unit tests to match. They still have no end-to-end coverage, and
  reading a service is weaker evidence than running it.

### 7. A tenant's timezone is stored and never read — **closed**

`tenant-service` records a `Timezone` per tenant, defaulted to UTC at creation.
Nothing else in the platform reads it.

Meanwhile `milk-service` answers "what did this animal give today" with
`recorded_at::date`, which PostgreSQL evaluates in the database session's
timezone. So a tenant's day is decided by where the database is configured, not
by where the tenant is — and the tenant record says otherwise. Two things claim
to say when a tenant's day begins, which is the same shape as two things claiming
what currency an amount is in, and it goes wrong the same way: silently, and only
for whoever is furthest from the assumption.

Single-country, single-database it is invisible. A society in Assam and one in
Gujarat share a day boundary anyway. It matters for a deployment spanning
timezones, and for any figure a fortnight's settlement is drawn from — a
collection at half past eleven at night falls in one day or the other depending
on a setting nobody involved chose.

Fixed by taking the first of the two options, because it is the one this
platform has already chosen for the same shape. `milk-service` pins each tenant's
timezone on its first session — stated, never defaulted, refused if a later
session disagrees — exactly as the services holding money pin a currency. Read
from the pin rather than from tenant-service, so recording a reading does not
depend on another service being reachable, which is the argument the currency pin
already makes.

`GetDailyYield` converts the instant to the tenant's own wall clock before taking
the date. The measurement that made this worth doing: a collection at one in the
morning Indian time reads as the 11th from a database session in Kolkata and the
10th from one in UTC or Chicago. `dailyyield_integration_test.go` runs it from
three sessions — one ahead of the tenant, one behind, one UTC — and reverting the
query to the session-timezone cast fails two of the three, naming the day the
readings moved to.

The second option is still open to a future deployment: if one deployment is
declared to be one timezone, the pin becomes a formality rather than a constraint
and nothing has to be undone. What has been closed is the disagreement.
- **`order`'s money columns were half widened.** The migration named
  `order_invoices`, which is not a table in that schema; it is `invoices`, and
  `returns` was omitted entirely. The loop matched nothing for a name that does
  not exist and reported success, so orders were widened at four decimals and the
  invoices raised from them stayed at two. A dinar deployment could place an
  order at 1.234 and have its invoice total silently rounded to 1.23.

The last of those is the one worth taking a lesson from. It was invisible in the
schema text because the widening is a `DO` block that searches
`information_schema` at apply time — what it does is a fact about the database,
not about the file. `e2e/moneycolumns_test.go` applies each schema and asks the
database. Reverting the one line makes it name all four affected columns.

Neither `order` nor `billing` had any end-to-end coverage of a line item or a
payment before this. The suite created an order and an invoice, moved their
statuses, and stopped — it had never checked that either service can add up what
it is for. `e2e/money_test.go` does that now, including that tax is rounded per
line rather than on the total, which is a penny's difference on three lines of
0.10 and the reason an invoice can disagree with the lines printed on it.

**Quantities were listed here as still open, and that was wrong on both counts.**

The claim was that `order_items.quantity` should move to
`libs/integrity/quantity`. It should not. That package is about milk: a closed
set of two units, LITRES and KILOGRAMS, existing because the two differ by about
three per cent and subtracting one from the other looks like theft. An order line
counts SKUs — three pouches, two sacks of feed — and forcing it to declare itself
litres or kilograms would be inventing a fact about it. The unit it is really in
is the SKU's, which the line already references.

The second half was that `float64` is a risk there. Measured rather than
assumed: 200,000 values across the whole range of `NUMERIC(10,3)` round-tripped
`NUMERIC -> float64 -> JSON -> float64` and **none lost a digit**. The column
tops out near 10^7 and `float64` is exact to three decimals past 10^12, so there
is no value the column can hold that the code cannot carry. The boundary is also
guarded — `exact.NonNegativeDecimal` refuses a quantity finer than the column
rather than letting PostgreSQL round it silently.

So there is nothing here to fix, and saying otherwise was the same mistake in
the other direction from the money one: reaching for a type because it is the
platform's type, rather than because the problem it solves is the problem in
hand.

**`returns` was a shape with no mechanism, and now has one.** `order-service`
had a `returns` table and a `domain.Return` type and nothing else — no repository
method, no service method, no endpoint, no reference anywhere in the tree.
Refunds did not exist, and the table made it look as though they did.

What is wired is the state machine the type already declared in a comment:
requested, then approved or rejected, then completed. Naming those and writing
the allowed moves as a map rather than a chain of checks is most of it — what is
interesting about a state machine is the transitions it does not have, and those
are invisible in a series of if statements.

Two things were deliberately not decided:

- **Completing a return does not touch the invoice or the order's totals.** What
  a refund does to a customer's account is an accounting decision nobody here has
  made. An e2e test asserts the order still reads what it charged, so the absence
  is stated rather than merely true.
- **A request may ask for more than the order charged.** What somebody asked for
  is part of the record of what was decided; refusing to write it down leaves the
  person who asked arguing about a conversation with no record. The bound applies
  at approval, which is when it becomes money.

That bound is a trigger rather than a check in one code path, because refunding
more than was taken is wrong under any refund policy — arithmetic, not policy, so
it belongs where no path can route around it.

**And the trigger was wrong the first time, in a way worth recording.** It summed
what was already committed and then locked the order. A sum taken before the wait
is a sum of the world as it was before the other approval existed, so two
concurrent approvals of 60 both passed against an order of 100. Measured, not
reasoned about: 120 of 100 committed, then 60 of 100 once the two statements were
swapped.

The test for it is a transaction-level one, and that is also worth recording. The
end-to-end version fires four approvals through HTTP and passes whichever way
round the two statements are written — the window is too narrow to hit from out
there. It was written first, it passed against the broken trigger, and it is kept
only as a smoke test with a comment saying exactly that. The guarantee is a
repository test that holds one transaction open and steps the other into it. So it stays unbuilt and is named here instead.

### 3. Deployment drift — **closed**

Found and fixed, and the reason it is listed here rather than deleted is that the
fixes were the small part:

- `docker-compose.yaml` was missing `settlement`, `material`, `laboratory` and
  `production` entirely, and the gateway had no URL for `procurement` either — so
  under compose a fifth of the platform was unreachable through the only address
  a person outside it has. Every unit test passed. The e2e suite passed, because
  it calls services directly.
- Six services had no Kubernetes manifests: `identity`, `procurement`,
  `settlement`, `material`, `laboratory`, `production`.

`services/gateway-service/internal/handler/deployment_test.go` now compares the
code and the deployment descriptors as two lists of the same thing. Run it with
`-count=1`, or through `scripts/check-all.sh`: compose sits outside the module,
Go's test cache does not track it, and without that flag the check reports a
cached pass while doing nothing.

### 4. Small test gaps — **closed**

`bitemporal`, `origin` and `procurement-service`'s domain now have tests, each
mutation-checked. Two defects fell out of writing them, which is the argument for
having written them:

- **`origin.HashFields` could collide.** It separated keys from values with 0x1f
  and pairs with 0x1e, so a field containing one of those bytes was
  indistinguishable from the delimiter between fields. `{"producer\x1fP-001":
  "x"}` and `{"producer": "P-001\x1fx"}` hashed identically, as did `{"k":
  "v\x1ek2\x1fv2"}` and `{"k": "v", "k2": "v2"}`. Two source records sharing one
  payload hash is precisely what the hash exists to prevent: a re-import of one
  becomes indistinguishable from an amendment of the other, and the import path's
  idempotency stops holding. Now length-prefixed, which content cannot forge.
  Nothing called it yet, so no stored hash needed migrating — but that is luck,
  not design.
- **`bitemporal` accepted a zero-width valid interval.** The interval is
  half-open, so `[t, t)` contains nothing, not even `t`. A record written with one
  exists, occupies its slot, and can never be returned by any query at any valid
  time — worse than a refusal, because nothing afterwards reports it as missing.
  Now `ErrEmptyInterval`.

### 5. Formulations — approval added, the rest deliberately not

A recipe is now drafted, signed off, and eventually withdrawn, and **a batch may
only be made against an approved one**. That is the piece with an integrity
argument: a vat measured against a target nobody agreed to reports a variance
saying the process is wrong, when what is wrong is that the number is still a
draft.

Two details worth knowing:

- **Only approved versions are exclusive.** Several drafts may cover one period —
  otherwise a plant could not write a correction to the recipe it is currently
  running. The conflict surfaces at approval, which is when there is a decision
  to make.
- **A batch survives its recipe being withdrawn.** The check runs only when a
  batch's recipe is set or changed, not on every update. Without that, a batch
  could not be quarantined once the recipe it was made under had been stopped —
  and a recipe being stopped is often what prompted the recall.

**Not built, and not an oversight:** plant scoping and recipe derivation. Both
would be inventing a requirement. Scoping needs to know whether a tenant is one
plant or several, and nothing here says; derivation needs a plant that actually
works that way. Neither is knowable from the code, and a wrong guess is a model
somebody has to work around forever.

### 6. The older ERP services change money in place — the acute case closed

Found while making the properties table in `integrity-platform.md` truthful, and
found the hard way: the first draft of that correction claimed these services
carry the audit trail. They did not — they updated content in place and wrote no
audit entry at all.

(Seven of them were given entries for the transitions that destroy a figure,
taking the count from six services to thirteen. It has since gone to twenty-five,
and the four that write nothing are named at the top of this document. The rest
of this section is the reasoning that got to thirteen; the reasoning that got to
twenty-five is in *What an edit overwrote*, and it is the same argument applied
to ordinary edits rather than to money.)

Narrowing it down mattered, because "these services have no audit trail" and
"one figure is destroyed" are different problems:

- **Order and billing totals are derived.** They are recomputed in SQL from the
  order lines, and the lines are inserted rather than updated. Losing a total
  loses nothing that cannot be rebuilt from what is still there.
- **A SKU's price is the primary fact.** `UpdateSKUPrice` overwrote it and
  nothing else held it, so an invoice raised before a price moved could not be
  reconciled against a price that no longer existed anywhere. That was the only
  place in these services where a money figure was destroyed.

That one is closed: the price change and an audit entry carrying the old and new
figures now land in the same transaction, so if the price moved the record of it
moving is there too.

A second defect fell out of the same file. `CreateSKU` validated a price against
the tenant's own currency scale — three decimals for a dinar deployment — and
`UpdateSKUPrice` validated against a hardcoded two. A price of 1.234 was accepted
on the way in and every attempt to correct it was refused, with a message about
precision that gave no hint the two paths disagreed. **A price you could set and
never change.** Both now take the price as a decimal literal and parse it at the
tenant's own currency scale, which refuses a finer figure rather than rounding it
and cannot disagree between the two paths because there is only one of them.

Checked while there and found nothing further: every other hardcoded scale in
these services — unit sizes, kilograms, litres, fat and SNF percentages, tax
rates — matches its column. That was verified against a migrated database rather
than against the schema text, which matters: `billing`'s `tax_rate` reads as
`NUMERIC(5,2)` in its `CREATE TABLE` and is widened to `(6,3)` by an `ALTER`
further down the same file, so reading the text alone reports a disagreement that
does not exist.

**Closed for the transitions that are decisions.** `billing`, `order`,
`cattle-market`, `breeding` and `health` now record what a status changed *from*,
in the same transaction as the change. Which ones got entries was a judgement
rather than a sweep:

- **Written.** An invoice voided, an order cancelled, a bid accepted, a sale
  recorded, a pregnancy confirmed or lost, a treatment marked complete. Each is a
  decision somebody could be asked to defend, and an invoice reading "cancelled"
  gave no indication whether it had been a draft nobody sent or something a
  customer had already been billed for.
- **Not written, and not an omission.** `inventory` already writes a
  `stock_movements` row for every adjustment, so its history is preserved the way
  order totals are — the movement is the record. `notification`'s mark-as-read is
  not a decision anybody disputes.

Verified by mutation: removing either audit write makes the e2e tests fail saying
the invoice was voided, or the order cancelled, and nothing records it.

Adding the writes made two of cattle-market's own integration tests fail, and
they were right to. `libs/integrity/audit` refuses an entry it cannot attribute,
and those tests called the repository directly with a bare context — no tenant,
no actor. A record of a change that cannot say who made it is not a record
anybody can use, so the refusal is the feature. The tests now carry an acting
context and apply the audit schema alongside the service's own, which is the
deployment constraint the e2e harness already documents: the trail is written
inside the caller's transaction, so `audit_logs` has to be reachable from the
caller's connection.

**Creates and deletes, checked — and the claim above was wrong.**

The previous version of this section said a row created and then deleted "leaves
nothing at all". It does not. All three delete paths in these services are soft —
`billing`, `order` and the rest have none, and `cattle`, `file` and `reporting`
set `deleted_at` — so the row survives with every value on it. Creates need no
separate entry either: the row carries its own `created_by` and `created_at`, and
the record is the row.

Checking it turned up one real defect, sharper than the thing I had claimed.
**`cattle-service` deleted an animal and named the wrong person.** The wire
request carried a `deleted_by`, the handler dropped it, and the repository took
no actor at all — so the update stamped `updated_at` to the moment of deletion
and left `updated_by` holding whoever had last edited the row. Those two fields
are meant to be read as a pair. The record did not merely omit who deleted the
animal; it attributed the deletion to somebody who had not made it, and the
caller who supplied the right name had every reason to believe it had been kept.

Fixed through the whole chain, with the deletion refused outright when no name is
given, and an audit entry carrying the tag number — which is what somebody
searches for when an animal has gone missing from a list. Verified by mutation:
make the handler drop the field again and the test fails.

Checked whether any other handler discards a declared request field, by comparing
each `*Request` type's fields against what the handler actually reads. None does
— and that check is now `e2e/reachable_test.go`'s second half rather than
something done once, because a check done once has already stopped working.

It examines 239 handlers and stays quiet about five, which hand the whole message
on to something this cannot follow — `printOptions(*m)` and the like. Skipping
them under-reports; flagging them would have made the whole thing something to
scroll past, and all five are fine. Run against the tree before `87e8115` it
names `DeleteCattleRequest`'s `deleted_by`, which is the defect it exists for.

---

## A rate card could be priced in a unit nobody recognised

`ratecard.Basis` is the thing a chart's rate is per, and its own doc comment says
there is no default because litres and kilograms of milk differ by about three
per cent — "larger than most of the divergences this platform exists to find".

The check beneath that comment was `Basis == ""`. It refuses a card that says
nothing and accepts one that says `"LITRES"`, `"kg"` or `"per_litre"` — which are
the three things a caller naturally writes, and `procurement-service` takes
`quantity_unit` straight off the wire into it.

What followed was worse than a refusal:

- `unitWord` read `if b == PerKg { return "kilogram" }; return "litre"`, so every
  unrecognised value was described as litres. A card priced per kilogram and
  labelled `"kg"` called itself per litre in every message it produced.
- The guard that stops a collection being priced against a card in the other unit
  is `col.Unit != c.Basis`. Two unrecognised strings compare equal, so a
  collection labelled `"kg"` priced happily against a card labelled `"kg"` and
  nothing anywhere knew what either of them meant.

Three per cent of what a producer is paid, in the direction nobody checks. It is
the error `libs/integrity/quantity` exists to prevent, arriving through the one
package that prices things.

`ValidBasis` now refuses anything that is not `PER_LITRE` or `PER_KG`; `Price`
checks the collection's unit before comparing it against the card, so two
unreadable strings can no longer agree with each other; `unitWord` says a value
is unrecognised rather than calling it litres; and `procurement-service` reports
it as an invalid argument rather than an internal failure. `ErrNoBasis` and
`ErrUnknownBasis` stay separate, because "you did not say" and "you said
something I cannot read" are different things to be told.

---

## Errors that told a caller to retry what could never work

Four services reported caller-fixable conditions as internal failures, which is
the one thing a status code is for: `CodeInternal` means "try again", and these
could not succeed on any number of retries.

- `product-catalog`: a currency the tenant does not record in, a tenant with no
  currency recorded yet, and a repricing that would change a SKU's currency.
- `health` and `cattle-market`: the same currency conditions.
- `milk`: every timezone refusal — and the service had no classifier at all.
  `CreateSession` called every failure an invalid argument, so a database that
  was down looked like a malformed request; `GetDailyYield` called every failure
  internal, so a tenant that had simply never recorded any milk was told to
  retry. The same mistake in opposite directions: a code chosen without looking
  at the error.

Three of the four error sets were added by this session's own work — the
timezone errors and `ErrCurrencyChange` — so this is a gap that opened while the
rest of it was being closed.

Fixing `milk` needed one thing more. Its service layer returned plain
`fmt.Errorf` values, so a caller mistake was indistinguishable from a database
failure and any classifier would have had to guess. It has the
`ErrInvalidArgument` marker every other service here has now, and its validation
returns it.

Found by comparing every exported `Err*` a service defines against what its
handler package references. `balance` and `ingestion` classify inline rather than
through a `classify` function and were fine; only the four above were not.

**That method could not find the worst two, and did not.** It compares the errors
a service defines against the ones its handler matches, so it only sees services
that already have the vocabulary to be inconsistent. A service with no marker at
all defines nothing, matches nothing, and comes back clean. Both of the ones it
missed were exactly that.

`cattle-service` chose by procedure, and had all three ways of being wrong at
once. `CreateCattle` and `CreateBreed` reported every failure as a bad request,
so an unreachable database looked like a typo. `GetCattle` reported every failure
as not-found, so during an outage a caller was told their animal does not exist —
the one answer somebody acts on by going to look for the animal. `ListCattle`,
`ListBreeds` and `DeleteCattle` reported every failure as internal, so a missing
tenant_id looked worth retrying. It surfaced only because a weight refusal was
being added to two of those paths and would have been misreported on each.

Its repository had nothing to match on either: every read wrapped `pgx.ErrNoRows`
in a formatted string, so "no such animal" and "the database is unreachable" were
the same value. It has a named `ErrNotFound` now. That change has a trap in it
worth recording, because a mutant found it: `GetCattle` used to answer not-found
unconditionally, so it was right by accident, and after the change it is right
only if the repository names the missing row. Removing that naming broke nothing,
because the end-to-end assertion covered the delete path and not the read. The
assertion that covers it exists now.

`observation-service` was the same shape and worse placed: two codes across
sixteen call sites, invalid-argument and not-found, and never internal.
`RecordObservation` matched a missing row and fell through to invalid-argument
for everything else, so a database outage reached the caller as something they
had typed wrong — in the service the settlement path reads from, where a booth
told its reading was malformed does not send it again.

Both now have the marker and a classifier, and the four codes are distinguished
by what they ask of whoever sent the request: fix it and resend, do not bother
resending, or it was not your fault and retrying may work. The last of those was
the one absent from both.

The lesson is about the detection rather than the defect. A sweep that looks for
an inconsistency can only find code already articulate enough to be inconsistent;
finding the silent cases needs a different question, and the one that works is
how many distinct codes a handler emits across how many call sites. One or two
across sixteen is the signature.

---

## What a second look found: the wire format was accidental

Eleven services returned their domain types directly from Connect handlers, and
those types carried no json tags at all. So their API emitted Go field names:
`tenant-service` answered with `{"ID":...,"ContactEmail":...,"CurrencyScale":2}`
while every other service in the platform answers in `lower_snake_case`.

That is not a style difference, and the reason is a property of Go's decoder: it
ignores case but not underscores. A client written in the platform's own
convention matches `id` against `ID` and reads it, and matches `currency_scale`
against `CurrencyScale` and reads **nothing** — the field arrives as its zero
value with no error anywhere. Measured rather than argued: a client struct
declaring `contact_email` and `currency_scale` against a real `TenantResponse`
read `""` and `0`.

`currency_scale` reading `0` is the sharp end. A tenant recording rupees looks
to that client like a tenant whose currency has no minor unit, and the paise are
dropped — by the client, on data the platform took care to keep exact all the way
to the wire.

It is the same shape as the `deleted_by` a handler discarded, one layer out: a
value the caller has every reason to believe it received. And it was found the
same way that one was — by a decoding trap in this repository's own tests, where
a client struct read `""` where it expected a calf's sex, in a test written two
days earlier.

**Fixed by tagging, not by adding view types.** 432 fields across the eleven
services now carry an explicit json name. The services that hold money already
have view types, introduced because a `money.Money` field needs a different shape
on the wire than in the domain; the rest have no such need, and tags are the
smaller change. None of the eleven marshals its domain types into storage, so
this changes the wire and nothing else — checked before touching them.

**`e2e/wireformat_test.go` pins it.** Every exported field reachable from a
Connect response, through the domain types the responses embed, must carry a json
name in `lower_snake_case`. Removing one tag makes it fail and name the field.

Worth noting why no existing test caught this: the e2e clients mostly read `id`,
`status` and other single words, which match case-insensitively either way. The
format was wrong in exactly the fields nothing happened to assert on.

---

## What a re-audit of this work found

Everything above was re-checked against a running system rather than re-read.
Four things came out of it, and all four were in the checking rather than the
code.

**One test was named for something it did not do.**
`TestAReconciliationRunIsAcceptedOnce` accepted a run once, confirmed the id
came back, and never accepted it twice. The assertion is there now. Reaching it
takes removing two guards — the service refuses an already-accepted run and the
repository's `UPDATE` carries `AND accepted_at IS NULL` — which is why mutating
one at a time showed nothing and looked for a while like the test was still
empty.

**One assertion passed for the wrong reason.** The check that a milk session with
no timezone is refused used a fresh tenant id in the body and the shared tenant
in the header. The mismatch was what failed the call, not the missing timezone,
so removing the emptiness check left the test green. It matters because
`time.LoadLocation("")` succeeds — it returns UTC — so refusing an empty name has
to be its own check rather than a consequence of loading it. Fixed, and the
mutation kills it now.

**Two documents disagreed about a measurement.** The round-trip figure read
100,000 in one place and 200,000 in six others. Re-measured rather than
reconciled by choosing: below 10^11, 0 of 200,000 values lost a digit; above
10^12, 151,682 of 200,000 did, which is 75.8% and matches the "more than three
quarters" written everywhere else.

**The audit-trail coverage claim was stale, and this work is what made it
stale.** `integrity-platform.md` said six of the twenty-nine services write to
the trail. Thirteen do: the original six plus seven given entries for the
transitions that destroy a figure. Sixteen still write nothing, six of which are
the integrity spine and do not need to. The nuance that had to go in with the
number is that what those seven write is narrower than what the first six write —
they record decisions somebody could be asked to defend, and nothing else.

Two mutations survived without anything being wrong, and both are worth knowing
about before the next audit repeats them. Loosening the `quantity_on_hand >= 0`
CHECK does not fail any test, because the guard the test exercises is in the
repository's `UPDATE`; the constraint is a backstop for paths that bypass the
repository, and nothing covers it. Removing the service's already-accepted check
does not fail any test either, for the same reason one layer down. Redundant
guards are not a defect, but a single-layer mutation proves less than it looks
like it does.

---

## Nobody could be told what they may not do

The platform authenticated from early on and authorised nothing. A session proved
which tenant somebody acted for, and all of them were then open to them: a
collector at a village booth could approve a settlement cycle, rewrite a rate
card, or read every producer's payment history. The tenant boundary held; inside
it there was none.

The model is deliberately small, because one nobody can hold in their head is one
that gets bypassed. A permission is a domain and an action, `settlement.approve`.
There are tens, not hundreds. A role is a named set of them, and there are seven,
drawn from the jobs that exist in a dairy co-operative rather than from the way
the code is split — pooling, settlement and shadow-settlement are one domain
here, because "may approve what a producer is paid" is one job and it would be a
strange role that could do it in two of the three.

Three decisions in it are worth keeping:

- **The route table is written out, not derived.** A rule that computes a
  permission from a method name silently mis-files the one procedure whose name
  does not fit the pattern. The first draft *was* generated that way and then read
  by hand, and ten entries were wrong. `ResolveIdentity` is a lookup that every
  collection performs on its way in, and the rule filed it with `ResolveConflict`,
  which is a supervisor adjudicating a disputed slot. `CorrectCollection`
  re-prices against the rate card in force, so it changes what a producer is paid,
  and the rule called it an ordinary write — which would have let the collector
  who recorded a figure amend it afterwards with nobody else involved.
  `ConfirmPregnancy` records a vet's finding and "Confirm" made it an approval,
  which would have stopped a clerk entering it.
- **It fails closed, and somebody finds out.** A procedure absent from the table
  is refused. Fail-closed only helps if it is noticed, so the exhaustive test
  fails the build when a route is added without an entry or an entry names a
  route that no longer exists — finding out at build time beats finding out when
  a co-operative cannot record its morning collection.
- **The check is in the server, not in the handlers.** `serve.New` wraps the mux,
  so a service cannot serve a route it forgot to guard. The alternative is a note
  in a README asking twenty-nine main functions to remember, which is the
  arrangement that produced the gap.

The gateway decides the same question once, against the procedure it is about to
proxy, and then forwards; it is the single caller allowed to run unguarded, and
the test in the package names it so a second one is a failing build rather than a
quiet decision.

Administration came with it. A co-operative can now be set up — users, roles,
service identities — without somebody opening a database console, which is what
setting one up had previously required.

---

## One process, one door

Twenty-nine services is the right shape for the problem and the wrong shape for a
society with one server in a back office. The modulith is the same code mounted
on one mux behind one port: every module, one binary, one image, one compose file.

It is the shape that ships, and that has a consequence the code now takes
seriously. A defence present only in the deployment nobody runs is not a weaker
defence, it is none — so the parity between the two is tested rather than
intended. `serve.Unguarded`, which the modulith's gateway uses, is rate-limited
like every other server; the sign-in limiter is in an app package both shapes
wire; and a test fails the build if a service-level defence exists in one shape
and not the other.

Mounting twenty-eight services on one mux also found a defect that the separate
shape could never have shown. Every service registered its own `/healthz`, and an
`http.ServeMux` panics on a duplicate pattern, so the modulith would have died at
startup on the second one. The check that existed for exactly this counted module
names instead of registering them, which is a control that reports success while
doing nothing. `/healthz` now lives in `serve` with `/readyz` and `/metrics`.

---

## What a service does when it is actually deployed

Six things were true of the platform in a test harness and of nothing in a
cluster.

**Nothing had timeouts.** A handful of slow connections hold sockets until the
process runs out, and the process that runs out is the one taking the morning's
collections. `ReadHeaderTimeout` is the one that matters for the attack; the write
timeout is generous because a settlement print over a large plant is genuinely
slow, and a report that dies halfway is worse than one that takes a minute.

**Nothing bounded how fast one caller could ask.** Not a defence against a
distributed attacker and not meant to be: it stops one misbehaving client starving
everyone else, which is the failure a co-operative actually meets. The general
limit is generous on purpose, because a booth recording a morning's collections
must never come near it — a limit that catches ordinary work is a limit somebody
turns off. Sign-in is stricter and counts failures rather than attempts, so a
person who mistypes twice is not locked out while somebody guessing is. There is
deliberately no way to switch either off: a value at or below zero reads as a
mistake and the default stands, because a control a typo silently removes is the
shape of failure this platform keeps finding.

**The gateway trusted headers it should not have.** `X-Forwarded-For` and
`X-Real-Ip` arrive from the caller and are now stripped, with the peer address
written into a header the services read. A rate limit keyed on a header the
attacker sets is a rate limit with a bypass in it.

**Probes could not fail.** `/readyz` now asks the service's dependencies.
Liveness stays unconditional and that is deliberate: a failing liveness probe gets
the pod killed, so tying it to the database turns a recoverable outage into a
crash loop across the platform. Readiness is the one that asks.

**TLS was a README item.** `serve.Run` serves it when a certificate and key are
configured, refuses a half-configuration rather than guessing, and can be made to
refuse plaintext outright. The database connection is the other half: a DSN with
`sslmode` absent, or set to `disable`, `allow` or `prefer`, is now refused unless
the deployment says in a full sentence that plaintext to the database is
acceptable there. The sentence is the point. A boolean would have been set to true
once and forgotten.

**And nothing was compiled in.** No service carries a default database URL any
more, and a test refuses the build if one reappears.

The cluster path is the same argument one level out. One Ingress is the only door;
a default-deny policy sits under everything; and each of the twenty-nine services
carries a NetworkPolicy naming exactly the callers that exist and an egress policy
naming exactly the calls it makes, both generated from the call graph the code
actually has and checked against it. An early draft of those policies allowed
`0.0.0.0/0` "for probes", which allowed everything — it is gone, and the test now
rejects an `ipBlock` outright.

---

## Two things the platform could not do for the people using it

**Nobody was told anything.** `notification-service` had existed for as long as
the platform, with eight routes and an inbox per recipient, and nothing anywhere
called it. A payable was held, a fortnight approved, money marked as paid, and the
only way to learn any of it was to go and look.

The message is queued in the same transaction as the change it describes, so a
hold that commits has a message and a hold that rolls back has none, and it is
delivered afterwards by a sweep that keeps trying. That is what makes an outage of
the notification service a delay rather than a message nobody ever gets.

Who is told is a role, not a person, and that is a limit worth stating plainly: a
payable names a producer's member code, and nothing in this platform links either
to somebody who can sign in. Inventing that link would have been inventing it. So
a hold reaches the supervisor who can resolve it and the accountant who owns the
money, and money moving reaches the auditor whose job is to see it — not the
accountant, who did it, because telling somebody what they just did is noise.

**Nobody could ask why a payment was what it was.** Tracing one payable to the
deliveries gathered into it, the rate card each was priced against and the
identity mapping that attributed each to the producer took four services and a
`psql` session. `ExplainPayable` is one call that answers it, retired mappings
included — which is why the superseded ones had to become readable first, and why
the exemption that said they were already readable was not true when it was
written.

---

## Three ways a check can exist and not run

This is the failure this platform keeps finding, and a single pass found three
more of it.

**A schema that only ever applied to an empty database.** The existing test
checked that the working-tree schema applies on top of the committed one, which
catches a statement that errors and not a statement that succeeds and does
nothing — `CREATE TABLE IF NOT EXISTS` on a table that exists quietly skips every
column and constraint inside it. It also only ran while the working tree differed
from HEAD, so the moment a change was committed the upgrade path was never looked
at again. The replacement builds two databases per service, one from the current
schema and one by applying every committed version in order, and compares them at
the catalogue. Its first run found four services whose upgraded database differed
from a fresh one, every one of which had passed the older test: six CHECK
constraints missing from settlement's payables, two from procurement's priced
collections, a column left behind on production's batches, and a currency column
that was `varchar` on one and `char` on the other.

**Six repository suites nothing had ever run.** They carry a build tag and read a
variable the gate never set, so from the day they were written `go test` did not
see them and no one noticed. Running them needed a database holding every
service's schema — `audit_logs` lives in audit-service's and every service writes
there — so the gate now provisions one and runs each suite against it. Seven had
rotted in the meantime and did not compile.

**A module skipped because it did not build.** `pkg` arrived as a library carried
over from another product: over a hundred packages, most of which did not build
here because the packages they imported were never brought across. The gate
skipped the whole module for that reason, which meant the two packages the
platform genuinely imports were ungated too. It now holds those two and the ULID
polyfill that deployment applies, and it is gated like everything else. The
register of known-missing packages went with the packages that needed it.

---

## Every measurement exact, and one place a float is right

Money became exact early and the numbers beside it did not. A quantity on an
invoice line, a tax rate, a kilogram of feed, a litre of milk, a fat percentage,
the count on a shelf, the size of a pack, an animal's weight and — in the service
named for recording measurements — an observation's value were float64 from the
wire to the column and back.

The boundary was guarded in some of them and the guard was the wrong shape: it
turned a float into the literal a column would store and refused one finer, which
left the domain holding 12.5 where the column held 12.500, the response sending a
number back out, and an audit trail written from the domain carrying whatever the
float printed as. In cattle and breeding there was no guard at all, so a weight
entered as 380.567 kg was rounded into its column by PostgreSQL in silence and
read back as a figure nobody typed, and one too large arrived at the caller as an
internal failure — advice to retry something that could never succeed.

`exact.Fixed` replaced all of it: a decimal at a stated scale, read from the
digits that were written rather than through a float, compared and added only at
that scale, rendered as the literal the column holds, and scanned straight from a
NUMERIC column. A JSON number is parsed from its own text, which is the difference
that matters — eighteen significant digits survive, where a float64 holds about
sixteen and lands on a neighbouring value.

Where the line falls is the part worth keeping. `balance-service` had already
drawn it correctly without anybody saying so: its measured flows are exact and
only its statistics — the test statistic, the gross-error threshold — are floats.
Observation now matches. What was measured is exact; what the Rust tier computed
from it is a float, because a combined standard uncertainty is a square root of a
sum of squares and has no exact decimal form. The conversion between them has one
named door, `exact.Fixed.Float64`, and the comment on it says why that is the one
place a float is the right answer.

Two checks in it are not symmetrical, and the asymmetry is deliberate. A quantity
is refused below zero. A reading is not: `TEMPERATURE_C` is a quantity kind and a
cooling tank at four below is the ordinary case, so a check on the sign would have
refused every cold-chain reading.

Mutation testing earned its place twice here. A mutant that emptied a validator
changed nothing, because a second check further down had already refused the same
values — one check, not two, and the dead one is gone. And a mutant that ignored
the calf-weight refusal passed, because the test asserting it sat after the
pregnancy had already calved and was being refused for that instead. It could not
have failed for the reason it claimed.

---

## What an edit overwrote

The trail recorded money and, for a while, nothing else. Thirteen services wrote
to it and the entries they wrote were about figures somebody is paid: a payable
approved, a price changed, an invoice voided. An ordinary edit — an animal's
weight corrected, a session reopened, a stock count adjusted, a listing withdrawn
— left a row saying who had last touched it and nothing saying what it had said
before.

That is a narrower gap than "no audit trail" and a real one. The argument for
recomputing anything in this platform is that the inputs can be traced to who
entered them; a figure that changed and cannot be shown to have changed is one
nobody can defend to the member who asks about it, and that is as true of a
weight as of a payment.

So every update that overwrites a figure now records what it overwrote, in the
same transaction as the change. Where an update could not sensibly carry one, the
exemption is written down with the reason, and the test refuses an entry whose
reason is missing — an earlier version of that register allowed a placeholder,
and a register of placeholders is a register nobody reads.

The reasons fall into four kinds, and stating them is what keeps the register
from becoming a list of things somebody could not be bothered with:

- **Machine state.** A capture session moving through its own lifecycle, a
  device's key generation advancing, the delivery bookkeeping of a queued
  message. Nobody decided these, so there is nobody to ask about them.
- **A field that was empty.** An ML estimate arriving beside a reading already
  recorded deterministically, a run claiming a window, a cycle recording the
  event that has just happened to it. There is no previous value.
- **Both states are rows.** Supersession keeps the old one and writes a new one,
  and the pair is the record. A second copy in the trail would be a second thing
  to keep in step.
- **The record is elsewhere and better.** Inventory writes a stock movement for
  every adjustment; the movement *is* the history, the way an order's totals are
  rebuildable from its lines.

One of those exemptions was untrue when it was written, which is the argument for
making each one say why rather than merely listing it. The canonical identity
mapping claimed a superseded row stays readable — and the row did stay in the
table, and nothing returned it. Making it readable came first, and then the
exemption was true.

Writing the entries also found a defect in two services that had nothing to do
with the trail: reading a row before updating it returned `pgx.ErrNoRows` where
the rest of the service expected a named not-found error, so a status change
against something that did not exist was reported as an internal failure.

### And a service that writes nothing at all

The register above covers updates, so it cannot see a service with no update
functions — there is nothing for it to look at. Four services write no entry of
any kind, and until there was a second register that was not a decision anybody
had written down; it was four absences that happened to have the same shape.

Three were plainly right. `audit-service` is the trail, and an entry recording
that an entry was written is the same fact twice. `gateway-service` decides
nothing and stores nothing. `notification-service` writes one recipient's own
inbox, and nobody disputes who read a notification.

`feed-service` was the one nobody had looked at, and this document said so. The
answer on looking is that it should stay off the trail, for three reasons that
hold together and would not separately:

- **Nothing in its schema is money**, and no producer is paid on any of it.
- **Nothing outside the service reads it.** No other service's answer depends on
  a ration or a consumption row.
- **It has no update or delete path at all.** Three creates and nothing else, so
  no figure it holds can be overwritten or destroyed — which is the thing a
  before-image exists to catch. Every row carries `created_by` already, and a
  consumption row carries `fed_by` and `fed_at` besides.

That last reason is the one that can quietly stop being true, so it is not left
as prose. Giving feed-service an edit path makes the *update* register fail by
name, which was checked rather than assumed: a temporary
`UpdateNutritionPlanQuantity` produced "feed-service.UpdateNutritionPlanQuantity
(nutrition_plans) writes no audit entry at all". The two registers interlock, and
the second one only had to record the decision.

The general rule, now that both exist: reference data is not audited anywhere in
this platform — there is no `create_breed`, no `create_sku`, no `create_category`
— and what is audited is what bears on money or on a physical fact the platform
will be asked to defend. `record_milk` because a producer is paid on it,
`record_result` because it prices milk, `draw_sample` and `record_custody`
because the chain of custody is the claim, `record_input` because the recall
genealogy runs through it, `register_instrument` because eligibility does.

---

## What it takes to run this

Four things stood between a repository that passes its tests and a platform
somebody could operate, and none of them was about the code.

### A backup, and a restore that has been run

There was neither. This is the system of record for what a co-operative's
producers are paid, and losing the database had no answer.

`scripts/backup.sh` and `scripts/restore.sh` are the two halves, and three
details in them are the difference between a backup and a directory nobody has
opened:

- **The roles are dumped separately.** A database dump does not contain them:
  `gavya_app` is a cluster object and every row-level security policy in this
  platform names it. Restore the database alone onto a fresh server and every
  policy refers to a role that is not there.
- **The dump is read back** before the script says it worked, which catches the
  failure that matters most — a dump truncated by a full disk, which pg_dump
  reports and a pipeline that ignores exit codes turns into a plausible file.
- **The restore checks properties rather than the exit code.** Losing forced
  row-level security is silent: the database works, and one table serves every
  tenant's rows to whoever asks.

The part worth having is `tools/dbadmin/internal/backup`, which does the round
trip against a real PostgreSQL and compares the two databases — catalogue, row
counts, isolation, and whether the audit chain still verifies. Two mutants went
in to see whether it bites: a dump taking the schema without the rows, caught;
and the roles dump emptied, **not caught**, because roles are cluster-wide and
`gavya_app` was already on the test server. That check now reads the backup
rather than the restored database, which is the only honest version of it in an
environment that cannot stage a fresh server.

One finding worth recording for whoever compares two databases next: pg_dump
changes how PostgreSQL renders the same predicate. A CHECK written as `IN (...)`
comes back as `= ANY (ARRAY[('X'::character varying)::text, ...])` where the
original renders as `= ANY ((ARRAY['X'::character varying, ...])::text[])`.
Identical meaning, different text, on about forty constraints and one partial
index. Comparing raw text reports a good restore as broken every time.

### A way to change the schema of a database that exists

`deploy/postgres-init` builds the database, once, when a PostgreSQL data
directory is first created, and never runs again. Everything after that had no
procedure: a schema change reached a running deployment by somebody opening psql
and applying files in an order recorded only in that script's comments.

That order is load-bearing at four points, each of which is a defect somebody
met — isolation sweeps the tables that exist, foreign keys are made tenant-safe
after it because row-level security does not reach a foreign key check, the audit
trail is made append-only after the grants because it revokes some of them back,
and isolation is swept again because that step creates a table of its own.

`tools/dbadmin/cmd/migrate` applies the same files in the same order, with an
advisory lock so two rollouts cannot interleave, a record of what was applied and
when and by whom, and the four refusals the init script ends with. The order now
lives in one place and a test compares it against the script, because two copies
of an order this particular will not stay in step. That test found a difference
the first time it ran, which is why the script now marks where applying stops and
verifying starts.

### The secrets the deployments named and nothing created

Twenty-eight deployments carried `secretRef: <service>-secret` and no secret
existed anywhere in the repository, so `kubectl apply` produced twenty-eight pods
in `CreateContainerConfigError`. That is the good outcome. The bad one is a
cluster where somebody made them by hand on the first afternoon and the service
added six months later has none.

`scripts/make-secrets.sh` generates them — never commits them, and refuses to
invent a password, because a generated default is a credential that looks
deliberate and is not. It reads which services need one from the deployments
rather than from a list. Two tests hold it: every referenced secret is produced
and carries a database URL, and every environment variable a service reads whose
name says it is a credential is supplied by its ConfigMap or its Secret rather
than by somebody's shell.

### Something that runs the gate

`scripts/check-all.sh` was the whole of this repository's quality control and
nothing ran it. It ran when somebody remembered, on a machine whose PostgreSQL
happened to be up. A check that depends on being remembered stops happening the
week everyone is busy, which is the week a change most needs it.

The workflow runs the script rather than a list of steps in YAML, because a
pipeline with its own list drifts from it and then "it passes in CI" and "it
passes locally" are two claims about two different things. A test fails the build
if it stops calling the script, if the checkout goes shallow — the upgrade test
needs the history, and without it compares a version against itself and passes —
or if the DSN loses its `%s`, which would put every service in one database and
pass anyway.

---

## The morning, measured

This platform has one predictable spike and it is the moment it must not fail:
every booth in a society records inside the same half hour, twice a day, and a
producer who cannot be recorded is written on paper and typed in later — which is
the failure the platform exists to remove. Nothing had ever measured it. There
were no benchmarks of any kind, so "fast enough" was a belief about code nobody
had run under load.

`e2e/load_test.go` drives the real binaries through the real collection path. It
is behind its own build tag rather than in the gate, because a load figure taken
on a runner that is building something else is a number that gets argued with
rather than acted on.

On a four-core machine running all twenty-nine services and their PostgreSQL side
by side, one thousand collections:

| booths at once | p50 | p99 | max | throughput |
|---|---|---|---|---|
| 10 | 3.4ms | 7.3ms | 11.4ms | 2787/s |
| 25 | 8.6ms | 16.4ms | 20.3ms | 2796/s |
| 50 | 17.2ms | 27.7ms | 83.9ms | 2334–2822/s |

Throughput is flat from ten booths upward and latency rises in proportion to
concurrency, which is the signature of a server at its service rate rather than
one falling over. Nothing was refused and nothing accepted was lost at any of
them — which is the assertion that matters more than any latency figure, because
concurrency that drops a write produces a platform that is fast and wrong, and
the producer whose collection vanished is the one who finds out.

Two things that cost time and are worth passing on. The first run showed a
bimodal distribution, ninety-five per cent under 25ms and five per cent at 2.5
seconds, which looked like a fixed timer somewhere; it was cold start and did not
reproduce. And the attempt to check that it did not reproduce ran the test twice
and got figures identical to the tenth of a millisecond, because the second run
was Go's test cache handing back the first one's output. `-count=1` is in the
instructions for that reason.

### The histogram those numbers bought

`libs/integrity/observe` kept no latency histogram, and the comment saying why
was right: buckets chosen before anybody had watched this run would have been an
invented latency profile rather than a measured one. The measurement above is the
watching, so the buckets exist now and are dense from a millisecond to a tenth of
a second, where the medians and ninety-ninth percentiles actually fall, with
headroom above for a deployment that has a real network between its services.

The alert that needs it is the one a mean cannot express. A stall that hits one
call in a hundred moves a mean by a few milliseconds and is the whole of what the
person it happened to experienced.

### Something that reads the metrics

Every service has served `/metrics` since readiness was added and nothing read
them, which made the platform's observability a page that existed.

Prometheus now scrapes all twenty-nine in both deployment shapes — a static list
for compose, pod annotations for Kubernetes — and six alerts cover what is worth
waking somebody for: a service gone, a service restarting in a loop, a procedure
failing more than one call in twenty, a service failing nearly everything, a
ninety-ninth percentile past a second, and requests piling up.

The check worth having is not that the alerts exist. It is that **every metric
they name is one the platform emits**, verified by exercising a live metrics
handler, scraping it, and comparing. An alert on a plausible name that nothing
publishes evaluates to no data forever, and a rule that never fires is
indistinguishable from a system that never breaks. The same test refuses an alert
with no description, because one nobody knows how to act on gets silenced and the
silence outlives the reason for it.

What is not checked here: the PromQL itself. `promtool` was not available in the
environment this was written in, so the expressions are known to be valid YAML
naming real metrics and are not known to parse as queries.

---

## One identifier, all the way through

There was no tracing, and two things made the absence harder to see than a plain
gap would have been.

`svcclient` set an `X-Request-Id` on every call and every caller generated a
fresh one. The platform had an identifier that looked like a correlation id and
correlated nothing: a settlement that gathers a fortnight touches procurement,
canonical and notification, and each hop carried a different one. Asking what a
request had done meant reading four services' logs and matching on timestamps.

And `audit.Entry` had carried a `TraceID` field since the trail was written.
Nothing ever set it. Every audit row in this platform had an empty `trace_id`, in
a column that is in the schema, in audit-service's domain model, and on its wire
format — a field that reports success while doing nothing, which is the shape of
defect this repository keeps finding, sitting inside the mechanism built to find
it.

What there is now is W3C Trace Context, which is one header, hand-written for the
reason `libs/integrity/observe` writes Prometheus text itself: the format is
small and specified and the SDK that would write it is not. A service that
receives a `traceparent` continues that trace and one that does not starts a new
one, so a trace begins where a request enters the platform and reaches
everything it touches. It is parsed strictly — a malformed header that is
silently repaired produces a trace that looks whole and joins two unrelated
requests, which is worse than one that visibly begins again.

Three places it is installed, and none of them is a main function. The middleware
is in `serve`, outermost, so a request refused by the rate limit or the
authorisation check is still traced — the request somebody is asking about is
usually the one that was turned away. The exporter is installed by `serve.Run`,
so a service cannot forget it and become the hop missing from every trace. And
`svcclient` injects a child span on every outgoing call. The service name comes
from the environment rather than a new parameter, because adding one would mean
editing thirty main functions and the one edited wrongly is the service that
appears in every trace under somebody else's name.

The end-to-end tests assert the thing unit tests cannot reach: that the header
survives a real HTTP hop, that the middleware puts it on the context, and that
the audit write two layers down finds it there. One of them exists only to stop
the others passing against a constant — a platform that stamped one trace id on
every row would satisfy "the trail carries a trace" and be useless.

### What this found in the writing

**A scrape configuration that would have watched nothing.** The monitoring added
in the previous section pointed Prometheus at the ports in
`libs/integrity/ports`. Those are what a service listens on when run directly on
a developer's machine, where everything shares a host. Under compose each service
is alone in its container and listens on 8080, and the registry number is what is
published to the *host* — and for several services not even that: audit is 8100
in the registry and published on 8094.

Prometheus runs inside the compose network. Every target would have been a port
nothing listens on, every service would have read as down, and the test written
to prevent exactly that compared against the registry too, so it passed. It now
compares against `docker-compose.yaml`, which is where the answer actually is.

Worth stating plainly because it is the failure this document is mostly about,
committed by the work that was closing it: a check is only as good as the thing
it compares against, and choosing that is the whole of the design.

**Fifteen services whose Kubernetes container port differs from the registry.**
Found while chasing the above and *not* a defect: each service's ConfigMap,
containerPort and Service target agree with one another, and services address
each other by name on the Service port. The registry is not the cluster's port
and was never meant to be. Recorded here because it looks alarming in a grep and
the next person will find it the same way.

---

## The two quiet failures

Both were named at the foot of the alert rules as things nothing watched, and
both sat there for the same reason: a service could count requests and could say
nothing whatever about its own work. `libs/integrity/observe` had no way to
publish a number. So the gaps were recorded, correctly, and could not be closed
by writing an alert — there was nothing to write one against.

It has a gauge registry now. Pull rather than push: a service registers a
function and it is called at scrape time, because the questions these ask have
answers at the moment they are asked, and a counter maintained alongside the rows
is a second copy that drifts from them.

**The outbox.** A payable is held, the message is queued in the same transaction,
and the sweep that delivers it stops — a bad URL, a notification service down
since Friday, a panic in the dispatcher. Every money event goes on being recorded
correctly and nobody is told about any of them, and nothing in the platform looks
wrong.

Two numbers, because depth alone does not say enough: it is normal for an outbox
to be non-empty between sweeps. The one that means something is the age of the
oldest message. A deep outbox that is draining is a busy fortnight; one whose
oldest message has waited a quarter of an hour is a sweep that has stopped,
whatever its depth.

**The chain.** The trail is hash-chained so that a row altered after the fact can
be shown to have been altered, and nothing ever checked. A break was found when
somebody thought to ask, which in practice means during the audit the chain
exists to survive — and evidence nobody has looked at since it was written is a
claim rather than evidence. It is walked hourly now and the verdict published.

Three details in those two are the same decision made three times, and it is the
one this document keeps coming back to. The outbox gauges report **-1** before
their first reading rather than 0, because 0 is the same number as "the outbox is
empty" — the reassuring answer, and the wrong one. A reading that fails keeps the
last value rather than overwriting it with a zero. And each pair of alerts has a
second alert that fires when the first one has gone blind: an outbox depth that
has not been read for ten minutes, a chain not walked for three hours. A check
that has stopped looks exactly like a check that keeps finding nothing.

The walk is on its own clock rather than in the scrape, and that is not
symmetrical with the outbox on purpose. Counting undelivered rows is an indexed
count; walking a hash chain reads every row, and doing it per scrape would put a
full table scan on a fifteen-second timer.

The test beside the rules had to learn something too. It verifies that every
metric an alert names is one the platform emits, by scraping a live handler — and
a gauge registered by settlement at boot does not appear in a handler this test
builds. It now also reads the names out of the code that publishes them, so a
gauge renamed or deleted takes its alert with it. A list of names in the test
would have been a second copy, and the second copy is the one that goes stale.

---

## The requirements, and the one I deleted without reading

Everything above records what was decided. None of it compares those decisions
against a requirement somebody wrote down beforehand, and the question "is
anything still pending?" had nowhere to be answered from. That comparison is now
[`docs/requirements-crosscheck.md`](requirements-crosscheck.md).

It starts with a thing I have to say plainly. Commit 6b30f24 trimmed `pkg/` from
a hundred-odd packages to the two the platform imports, and `pkg/requirements.md`
went out with them, unread. The commit message said the deleted code was in the
history if any of it turned out to be wanted, which was true of the code and was
not a reason to delete a document called *requirements* without opening it.

Opening it changes what it is rather than what it says: it is the requirements
document for the inherited Go library — dependency injection, a dynamic query
builder, a Kafka event bus — not for a dairy platform. It is restored at
`docs/inherited/` and crossed off row by row anyway, because several of its lines
are good requirements wherever they came from, and three of them turned out to be
the same gap seen from three angles: **nothing in this platform watches the
database.** No connection-pool statistics, no query metrics, no spans around a
query. `alerts.yml` had already written that gap down and given a reason not to
close it from there; that reason does not apply to numbers a service can read off
a pool it already holds.

Two other things came out of the cross-check.

The README named twenty-two services and there are twenty-nine. The seven missing
included settlement — the money path — and identity, which is how anything in
the platform proves who it is. Fixed.

And the two clients turned out to be the requirement in its most binding form.
`web/` and `mobile/` between them call twenty-five procedures, every one of them
something somebody was promised on a screen, and nothing compared that list
against what the platform serves. All twenty-five are served; that was luck
rather than a check. The failure waiting was the quiet kind, because the clients
are in TypeScript and Dart while the route table, the permission table and the
end-to-end suite all move together with the Go. The console would have shown
`not_found` on a screen a supervisor opens once a fortnight. The bench would have
shown `not_found` on `DeliverRecord` and — correctly, by its own design — kept
every record in its outbox rather than counting it as delivered. Nothing lost,
nothing counted, until somebody telephones.

`services/gateway-service/handler/clients_test.go` compares them now: the
procedures each client calls against the permission table, and each procedure's
package against the prefixes the gateway routes. Two failures, not one — a
procedure can exist, be permitted, and still be unreachable through the only door
a client has.

---

## Two defaults that multiplied

The cross-check said the database was the one thing nothing watched, and that was
true and was not the worst of it. Going to publish the numbers meant reading what
the pool was actually configured with, and the answer was nothing.

`pgxpool` sizes a pool at the greater of four and the number of CPUs unless it is
told otherwise. Nothing told it otherwise. PostgreSQL allows a hundred
connections unless it is told otherwise. No deployment in this repository told it
otherwise. Twenty-eight services open a pool, every one of them against the same
`dairy` database — so the platform's ceiling was 112 on the four-core machine the
load test ran on, and 224 on an eight-core host, against a limit of 100.

Neither default is wrong. pgxpool's is a reasonable guess for one service and
PostgreSQL's is a reasonable guess for one machine. The defect is that the two
numbers had never appeared on the same page, and the product of them is what the
platform actually asks for. It does not show at rest, because `min_conns` is zero
and a pool holds nothing until somebody asks; it shows when every service is busy
at once, which is the morning collection, and it shows as `FATAL: sorry, too many
clients already` to whichever services ask last. A booth being turned away, with
nothing in any log saying why the number was what it was.

The pool size is eight now and the headroom forty, both in
`libs/integrity/tenantdb/limits.go` with the arithmetic beside them, and both
compose files declare `max_connections=300`. Eight because four was measured to
be enough — the load test sustained about 2,800 collections a second at fifty
booths with pools of four — and eight is that with room. A DSN that sets
`pool_max_conns` still wins, because a deployment that knows its own database is
the one entitled to override this.

The number is not what makes this closed. `TestThePoolsFitTheDatabase` multiplies
the per-service maximum by the services that actually open a pool — counted from
the code, because the gateway is in both compose files and never touches a
database — and fails if the result does not fit under what those files declare. A
number chosen today is worth little; a number that cannot silently stop adding up
is worth a great deal.

Kubernetes is documented rather than checked. Those manifests point at a
PostgreSQL this repository does not deploy, and a test comparing a manifest
against a limit nobody here controls would pass while the cluster ran out of
connections.

### Holding a connection for two minutes

Two more settings nothing had made. `serve`'s WriteTimeout allows a request two
minutes, deliberately, because a settlement print is slow — but that bounds the
request, not the statement. A query waiting on a lock held a pooled connection
for the whole two minutes, and with a pool of four that was most of a service's
capacity spent on one row. Worse, and with no bound at all: a transaction left
open by a handler that returned without committing held its connection, its locks
and its rows until the process died. A statement timeout does not touch that one
— nothing is running.

`statement_timeout` and `idle_in_transaction_session_timeout`, sixty seconds
each, on every pooled connection, overridable per DSN.

Both are asked of the server rather than of a configuration struct. The unit
tests beside them assert what `applyLimits` put in a `pgxpool.Config`, and that is
worth very little: a runtime parameter PostgreSQL does not recognise would
satisfy every one of them, and a GUC set to a value the server never applied is
exactly the shape of thing this repository keeps finding. So there is a
`dbintegration` suite that runs `SHOW statement_timeout` on a connection the
platform's own `NewPool` opened, and compares what comes back.

### What it takes to see any of this

Seven gauges off `pgxpool.Stat()` and three counters from the query tracer, on
the same `/metrics` handler as everything else, so the existing scrape
configuration reaches them unchanged. Summed across the pools a process holds,
because the modulith is twenty-eight modules in one process and does not merge
the databases — a gauge per pool would have left the twenty-eighth module as the
only one reported.

`observe` gained a Counter alongside its Gauge for this. Three of the pool
numbers only ever rise, `rate()` and `increase()` are written against counters,
and Prometheus uses the declared type to tell a process restart from a value that
fell. A monotonic number declared as a gauge works by accident and is a lie in
the exposition.

And a span per query, through pgx's `QueryTracer`, so a trace no longer stops at
the database. The name comes from a bounded vocabulary — `db SELECT collections`
— because a trace store indexes on the name and one name per statement is how it
stops being usable. Where it cannot be sure what the statement targets, it says
`db SELECT` rather than guessing: a span name that is confidently wrong sends
somebody to the wrong table.

The counters are separate from the spans on purpose. A span exists only when a
collector is configured and only inside a request; the counters are always there
and cover the sweeps and the boot checks, which deliberately have no span. "How
much of this service's time goes to the database" is a question somebody asks
before they have set up tracing.

### Two checks that could not see the work

Both had the same shape, and it is the shape this document keeps returning to.

The alert-name scanner — the one that fails the build if an alert names a metric
nothing publishes — walked `services/` and nothing else. The pool numbers are
published from `libs/integrity/tenantdb`, because that is where every service's
database connection is made. An alert naming one would have read as an alert on a
metric nothing emits, which is precisely the failure that check exists to catch,
arriving through the check itself.

And the gate's `dbintegration` scan walked `services` and `tools`. `tools` was
added when the backup round trip turned out to be unrun; `libs` was not, so the
suite that asks the server what it actually applied would have sat there proving
nothing. Both widened.

Thirteen mutants on this work, all killed: a pool left at the default, a DSN's
own setting overwritten, the idle-in-transaction bound dropped, `eachPool`
reading only the first pool, counters rendered as gauges, `Child` starting a
trace where it should start none and omitting the parent where it should name
one, the compose limit returned to a hundred, the scanner narrowed back to
`services/`, the span named after the statement, and the two query counters
stopped. The one worth naming is the compose limit: with it at a hundred, the
arithmetic test fails with the sentence a person would need — "allows 100 and the
platform can ask for 264" — rather than with a diff.

---

## Counting what the suite actually called

"261 of 261, covered end to end" was true on the afternoon somebody counted it,
and nothing recounted it. A route added after that got an entry in the permission
table — gated — and nothing else, and the difference between a procedure nobody
has exercised and one that has never worked is invisible until somebody
telephones. That state is not hypothetical here: procedures have been found in
this repository that were registered, reachable and failed on their first real
call.

The first version read the suite's own source for `<serviceConst>+"/Method"`,
which is how most of these calls are written. It reported twenty-one routes
uncovered and **nineteen of them were covered** — by a constant with a digit on
the end that the pattern did not match, by a method name arriving in a loop
variable, by identity-service's tests, which post to a URL they build themselves.
A coverage check that cannot read the call reports gaps where there are none, and
the end of that is an exception list that grows until the check means nothing.

So it asks the services instead. Every one of them has counted what it served,
per procedure, since readiness was added; a procedure with a non-zero count is
one the suite reached, whatever shape the call was written in. The measurement
comes off the same `/metrics` endpoint Prometheus scrapes, so the thing being
trusted is a thing the platform relies on anyway.

That version reported three, and then did the same thing to itself. Two of the
three were covered — on the ML-enabled platform, a second copy of three Go
services that the sweep was not asking. The failure this whole check is about,
arriving inside the check: a count is only as good as the set of things it asks,
and a thing it does not know to ask reads exactly like a thing that never ran.

**The third was real.** `ingestion.v1.IngestionService/ListSessions` had never
been called by anything.

All three are covered now and covered from the plain platform, which matters more
than it sounds: the ML tests skip where cargo is absent, so a route reached only
through them goes back to uncovered on a machine with no Rust toolchain. A run
that did not converge is exactly what balance-service produces without the
reconciler, and it is the half worth testing — accepting one would state that a
period closed on an arithmetic that never closed. A flagged observation can be
made with SQL, and the schema still refuses one with no score behind it, which is
the constraint doing its job.

The comparison itself has its own test, against sets whose answer is known. It
had nothing checking it otherwise: a version that always answered "nothing
missing" would have passed every run of the suite and every mutation of
everything else.

### Where the clients meet the platform

The procedure names were gated; the payloads were not. A renamed json tag moves
with the Go and is invisible to every Go test — the console draws a blank cell,
and the bench, which treats anything outside its closed vocabulary as *not
delivered*, leaves a morning's collections in an outbox that will never empty.

Compared now: every field the console declares against the json tags of the four
services it calls; every field the bench sends or reads against
ingestion-service's alone; and the bench's Connect error vocabulary against the
platform's, including that its fallback is `unknown`. That last is the sharpest
of the three — the outbox decides whether a record may be sent again from the
code that came back, so a fallback of `unavailable` would make every unrecognised
refusal look worth retrying, which for a device holding a day's collections is
the difference between a retry and a double count.

What it does not check is which message a field belongs to, and that is written
down where the check is.

### A linter, and what it found

I said golangci-lint was likely to find little. It found seventy, of which about
ten were real, and two of those were the kind this document exists for.

`pgxpool.BeforeAcquire` — the hook every tenant's isolation runs through — is
deprecated, and the replacement is better for a reason that has nothing to do
with deprecation. BeforeAcquire answers with a bool: a failure to set the tenant
returns false, which destroys the connection and retries on a new one, so a
database that has started refusing `SET` becomes a pool that opens connections,
fails to configure them, throws them away and opens more, with the caller waiting
and nothing saying why. `PrepareConn` answers with a bool *and* an error, so the
connection goes back to the pool and the query that wanted it fails with the
reason.

And the gate's own formatting step listed `libs services e2e tools`. Not `pkg` —
directly above a comment saying that pkg is gated like everything else. One file
had been unformatted for as long as it had been in the repository and nothing
said so, because the linter's gofmt runs per module and has no such list to
forget.

The rest: a draft profile written to a file whose close was deferred and
unchecked, which is how a tool reports something written that is not on disk; two
dead error helpers, one of them a second way of reporting an invalid argument the
service already reported another way; an ineffectual assignment in the tracing
middleware. Fifty-six were the conventional discards, and they are named
individually in `.golangci.yml` rather than silenced in a block — the two places
where the error did matter are fixed rather than listed.

Five linters, not a hundred. Each is in the file with what it has found here, and
a sixth is a decision somebody should make on the same terms.

---

## Blocked, and has been since early on

None of these can be worked around by writing more code, and each has been
deliberately left rather than guessed at:

- **One real AMCU export file.** Every vendor's format differs and none publish a
  schema. A parser written against an invented format is a parser that fails on
  first contact, silently, on somebody's collection data.
- **One analyser bench capture.** Instrument uncertainty by measurement method is
  an empirical property of a specific instrument in a specific plant. There is no
  table of these and this platform will not invent one.
- **Twenty buyer conversations.** What a society will actually pay for, and which
  of these guarantees it recognises as valuable, is not derivable from the code.

---

## What was decided, and why

Recorded because the reasoning is the expensive part and it is not visible in the
diff.

| Decision | Reason |
|---|---|
| Shadow mode, never paying from the platform's own number | Nobody sensibly agrees to trust a new settlement figure on day one with nothing to check it against. |
| All ML in Rust, reached only over the network | No cgo, no shared memory, no in-process model loading. A model failure degrades an advisory signal; it cannot take down the authoritative path. |
| Auth and authorization in scope | Explicitly reversed an earlier decision to defer them. A multi-tenant platform without them is a demo. |
| Tenant isolation in the database rather than in queries | Two cross-tenant leaks were found in one service's hand-written queries. Both returned plausible results and no test failed. |
| Money and quantities as scaled integers | The rounding trail is auditable and a settlement carries no unexplainable residual. |
| Every schema file re-runnable | A schema file that only works on an empty database works once, and the second time is in production. This has been violated twice — both times a `DROP` refused by a dependent object, killing the rest of the file. |
| Corrections by supersession, never in place | A figure that changed and cannot be shown to have changed is one nobody can defend to the member who asks about it. |
| Deployment descriptors checked against the code | A service missing from compose starts, answers, and is unreachable. Nothing else in the repository notices, because every other test either does not need the gateway or calls services directly. |
| Refuse rather than default; report rather than refuse | Two different rules, and which applies depends on whether the platform would be inventing a number (refuse) or judging somebody's process (report). A plant substitutes ingredients; refusing that means the vat is recorded wrongly or not at all, and a gap in the genealogy is worse than a note beside it. |
| Permissions written out per procedure, not derived from method names | The generated first draft got ten wrong, and the wrong ones were the dangerous ones: a collector able to amend the figure they had just recorded, a clerk unable to enter a vet's finding. A rule silently mis-files whichever procedure does not fit its pattern. |
| The authorization check in the server, not in the handlers | A service cannot serve a route it forgot to guard. The alternative is a note in a README asking twenty-nine main functions to remember, which is the arrangement that produced the gap. |
| No way to turn the rate limit off | A value at or below zero reads as a mistake and the default stands. A control a typo silently removes is not a weaker control, it is none — and the limit is set generously precisely so nobody has a reason to want it gone. |
| Plaintext to the database refused unless a deployment says so in a sentence | The setting is a phrase, not a boolean, because a boolean gets set to true once during a bad afternoon and is never read again. |
| Liveness unconditional, readiness conditional | A failing liveness probe gets the pod killed, so tying it to the database turns a recoverable outage into a crash loop across the whole platform. |
| The modulith's defences tested against the separate shape's | A defence present only in the deployment nobody runs is none. The modulith is the shape that ships. |
| Notifications addressed to roles, not to producers | A payable names a member code and nothing links either to somebody who can sign in. Addressing a person would have meant inventing that link. |
| A measurement exact, a statistic float | A combined standard uncertainty is a square root of a sum of squares and has no exact decimal form. Forcing it into an exact type would dress a float as exact, which is the failure this platform spends its time refusing. |
| A quantity refused below zero, a reading not | `TEMPERATURE_C` is a quantity kind and a cooling tank at four below is the ordinary case. A single rule for both would have refused every cold-chain reading. |
| A backup is not a backup until it has been restored | The failure arrives months later, at the one moment nobody has time to debug it. The round trip runs against a real PostgreSQL on an ordinary afternoon instead. |
| The migration runner applies the same files the init script applies | Not a parallel set of migrations. The schemas are re-runnable by design and the upgrade test already proves a fresh database matches an upgraded one; a second set of files would be a second thing to keep true. |
| Secrets generated, never committed | The alternative is a credential in git. The script refuses to invent a password, because a generated default is a credential that looks deliberate and is not. |
| CI runs the script, not a list of steps in YAML | A pipeline with its own list drifts from the script, and then "it passes in CI" and "it passes locally" are two claims about two different things — and CI's is the weaker one. |
| Histogram buckets chosen from a measurement, not in advance | The package refused a histogram for a long time on exactly this ground, and it was right to. The load test is the watching that made the choice honest. |
| The load test is not in the gate | It takes minutes and wants a quiet machine. A figure taken on a runner that is building something else gets argued with rather than acted on. |
| Alerts checked against a live metrics handler | An alert naming a metric nothing publishes never fires, and never firing is indistinguishable from nothing ever breaking. |
| W3C trace context, hand-written, rather than the OpenTelemetry SDK | The wire format is small and specified; the dependency across thirty modules is not. What goes on the wire is the standard, so a collector cannot tell. |
| A malformed traceparent starts a new trace rather than being repaired | A silently repaired header produces a trace that looks whole and joins two unrelated requests. A visible gap is the honest failure. |
| Spans dropped rather than blocking the request | A tracing system that slows the collection path is breaking the thing it exists to measure. The drops are counted, because a silent one makes a trace with a hole in it look like a service that was never called. |
| A gauge reports -1 before its first reading, never 0 | Zero is the same number as "the queue is empty", which is the reassuring answer and the wrong one. A failed reading keeps the last value for the same reason. |
| Every alert that can go blind has a second alert watching it | A check that has stopped looks exactly like a check that keeps finding nothing. |
| No alerts on metrics from an exporter this repository does not run | The check beside the rules verifies that every name an alert uses is one the platform actually emits. Rules it cannot verify would be the one thing in that file nothing had checked. |
| The clients are a requirement, and are compared like one | `web/` and `mobile/` are the only statement of what was promised that a person actually sees. They are in two other languages in two other modules, so every Go-side check moves with a rename and none of them notices. |
| A pool size and a server limit are one arithmetic, and it is gated | Two defaults, neither chosen, multiplying: twenty-eight pools of `max(4, numCPU)` against a `max_connections` of 100. Both numbers were reasonable alone and nothing put them on the same page. The test is the page. |
| A GUC is asked of the server, not of a config struct | A runtime parameter PostgreSQL does not recognise satisfies every unit test that checks what was sent. `SHOW statement_timeout` on a connection the platform's own code opened is the only version of that check worth having. |
| A rising number is declared a counter | `rate()` and `increase()` are written against counters, and Prometheus uses the declared type to tell a process restart from a value that fell. A monotonic gauge works by accident and is a lie in the exposition. |
| A span named from a bounded vocabulary, and silent where it cannot be sure | A trace store indexes on the name, so one name per statement makes it unusable — and a name that is confidently wrong sends somebody to the wrong table. `db SELECT` is the honest answer when the target is a subquery. |
| A query outside a request gets a counter, not a trace | A sweep makes one service call and hundreds of queries. Starting a trace per query would bury every trace somebody actually asked for. The opposite of what `Inject` does, on purpose. |
| Coverage counted from what the services served, not read out of the tests | The source-reading version reported twenty-one gaps and nineteen were not gaps. A check that cannot read the call reports gaps where there are none, and the end of that is an exception list nobody trusts. |
| A route covered from the plain platform, not only the ML one | The ML tests skip where cargo is absent, so a route reached only through them is uncovered on any machine without a Rust toolchain — and a check that passes or fails according to what is installed gets deleted. |
| The comparison in a coverage check has its own test | A version that always answered "nothing missing" would pass every run of the suite and every mutation of everything else. |
| Five linters, each with what it found written beside it | A hundred linters across thirty-five modules is some thousands of findings, most of them style, and the end is a wall of nolint or a gate somebody turned off. |
| The conventional error discards named one by one | A blanket exclusion also hides the two places where the error mattered. Both of those are fixed; the list is what is left. |
