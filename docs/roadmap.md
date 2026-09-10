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

Twenty-nine Go services, five Rust ML services, one shared Go library and one
shared Rust crate.

```sh
# everything, from the repository root
for d in $(go list -m -f '{{.Dir}}' | grep -v '/pkg$'); do (cd "$d" && go test ./...); done
cd ml && cargo test --workspace

# end to end: builds the real binaries, real PostgreSQL, real HTTP
cd e2e && TEST_DATABASE_DSN="postgres://user@host:port/%s?sslmode=disable" \
  go test -tags e2e ./...
```

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

Five Rust services in `ml/`: anomaly, uncertainty, divergence, reconciliation, and
the shared `mlcore` crate. Every call is advisory; every caller has a complete
deterministic answer without it.

**Currently disabled in every deployment.** Each Go caller reads its endpoint from
an environment variable that defaults to empty, and logs at boot that the tier is
off. See *Open work* below.

### Cross-cutting

- **Tenant isolation** in the database, not in every query. `tenant_id` on every
  table, RLS with FORCE, and a role that cannot bypass it.
- **A hash-chained append-only audit** written inside the caller's transaction, so
  a change and its record land together or not at all — **used by six services**,
  not all of them. `procurement`, `settlement`, `material`, `laboratory`,
  `production` and `milk`. The integrity spine does not need it, because its
  records are append-only and bitemporal and the history is the data. The older
  ERP services do need it and do not have it: see open work below.
- **Reference decisions.** Every reference-shaped column that carries no foreign
  key is decided by a person and recorded in
  `libs/integrity/isolation/references.sql`, with the reason. The deploy fails
  rather than warns on an undecided one.
- **Exact money and exact quantities.** `libs/integrity/money` and
  `libs/integrity/quantity`. No float touches a currency amount or a measured
  volume on the integrity path.

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

Ordered by what would be lost if it were left undone.

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
  is exact only below about 10^11. Below that, nothing lost across 100,000
  values; above 10^12, more than three quarters lost a digit. 6791947779410.3551
  comes back as 6791947779410.3555.

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
  end-to-end coverage, and around 137 of the platform's 245 registered routes
  still have none.

  "Around", because counting this by grep is unreliable in both directions and
  the first attempt got it wrong: identity-service builds its procedure name at
  the call site — `base + "/gavya.identity.v1.IdentityService/" + method` — so a
  search for the literal string reported its whole suite as uncovered when it is
  one of the better-tested services here. The figure is worth having as an order
  of magnitude and not as a number.

  What is worth having exactly is which of them matter, and one answer stood
  out: **`ingestion-service` had no end-to-end coverage of anything.** The
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
  that had never worked in one, an error silently swallowed in the other. The
  remaining uncovered services are not obviously different, and the way to find
  out is one at a time. `tenant-service`, `inventory-service`, `breeding-service`,
  `canonical-service`, `balance-service` and the write half of `health-service`
  are the ones still untouched.

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

**Genuinely open, and small: `returns` is a shape with no mechanism.**
`order-service` has a `returns` table and a `domain.Return` type, and nothing
else — no repository method, no service method, no endpoint, no reference
anywhere in the tree. Refunds do not exist. The table and the type make it look
as though they do, which is worse than their absence would be. Building them
would mean deciding what triggers a refund, whether it can be partial, who
approves one and what it does to the invoice — none of which is knowable from
here, and all of which a wrong guess turns into a model somebody works around
forever. So it stays unbuilt and is named here instead.

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
carry the audit trail. They do not. Nine of them update content in place and
write no audit entry at all.

Narrowing it down mattered, because "nine services have no audit trail" and
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
