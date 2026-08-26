# Reality-acquisition tools

Three tools for the phase before anything is built on top of somebody else's data:
two for the files an AMCU exports, one for the wire an analyser speaks.

- **`amcu-profile`** — reads a real collection export and reports what is in it,
  what is wrong with it, and how it maps onto the platform.
- **`amcu-import`** — reads the same export through the profile the first one
  produced, and loads the collections.
- **`bench-capture`** — records what an analyser or controller actually sends and
  afterwards helps work out what it meant.

`amcu-profile` and `bench-capture` never write to the platform. `amcu-import`
does, and only when told to: a dry run is the default.

---

# amcu-profile

Reads a real collection export and reports what is in it, what is wrong with it,
and how it maps onto the platform.

This is the tool for the reality-acquisition phase. The specification asks for
"at least one real AMCU/collection export mapped and schema pathologies
documented"; this turns that from a fortnight of reading somebody else's CSV into
an afternoon of checking a draft.

It never writes to the platform. It reads a file and prints what it found.

```sh
go build -o amcu-profile ./cmd/amcu-profile

amcu-profile collections.csv                    # what is in it, what is wrong
amcu-profile --quiet collections.csv            # findings only
amcu-profile --vendor SmartDairy \
             --write-profile smartdairy.json \
             collections.csv                    # propose a vendor profile
amcu-profile --check smartdairy.json newer.csv  # has the format changed?
```

It exits non-zero when a finding is blocking, so it can gate an import.

## What it reads

Nothing about the file is assumed, and every choice it makes is reported so you
can disagree with it:

- **Encoding** — UTF-8, either UTF-16, or a single-byte codepage. A file that is
  not UTF-8 is named as such, because a producer whose name is unreadable cannot
  check their own statement.
- **Delimiter** — chosen by which separator gives the most consistent field count
  across the file, not by which is commonest. A file full of names with commas
  beats a comma-delimited file on frequency alone.
- **Fixed width** — where no delimiter fits, the column boundaries are found from
  the positions that are blank on every line.
- **Header** — inferred from whether the first row looks unlike the rows beneath
  it, with the reasoning printed.

Rows whose field count differs from the header are kept, never dropped. A ragged
row is usually a name containing the delimiter, and discarding it silently loses
a producer.

## What it identifies

Each column is matched to the fact it holds — producer, date, shift, quantity,
fat, SNF, CLR, rate, amount and the rest — from two kinds of evidence, weighed
differently.

A header is a hint and a lying one: vendors label a rate column `FAT`. The values
are better evidence, because milk is a physical substance. Fat lives at 3–8.5 per
cent across cow and buffalo; solids-not-fat at 8–9.3; a lactometer reading at
25–31.

Two things make that work on real data rather than clean data:

- **The middle of the column decides, not its extremes.** One failed analyser
  reading — which every real export contains — would otherwise push a whole
  column out of its own band. A single 19.4 in a fat column was enough.
- **Bands are scored, not tested in order.** Fat and SNF overlap, so first-match
  assigns whichever was listed first and the two end up swapped. Swapped, every
  payment is computed from the wrong measurement and nothing downstream can tell.

Where the values and the header disagree, the values win and the disagreement is
printed.

## What it finds wrong

Each of these is something that has gone wrong in real dairy data and that a
naive importer would carry in while appearing to succeed.

| Finding | Why it matters |
| --- | --- |
| A date column that reads two ways | A month of collections lands on the wrong days |
| A producer code used for two names | Two people share one payment history |
| The same producer, day and shift twice | Milk is paid for twice |
| A measurement that lost precision partway | For fat, that digit multiplies into the rate |
| An amount that is not quantity × rate | The file disagrees with itself before anybody recomputes |
| Readings outside what milk measures | A failed reading, a wrong unit, or a mislabelled column |
| Text decoded with the wrong codepage | A producer cannot read their own name |
| A missing producer, date, quantity or fat | The collection cannot be settled |
| More than one society in one file | Two societies' member codes would be merged |

Duplicates and code reuse are scoped by society, because a producer code is
issued by a society and is only unique inside it. Member 40 at two centres is two
people.

Precision loss is distinguished from dropped trailing zeroes. About one in ten
two-decimal readings ends in zero and is written with one; flagging that would
bury every real finding under noise.

## Vendor profiles

An AMCU vendor's export format is a fact about their software, not about dairy.
SmartDairy, a federation's in-house system and a twenty-year-old terminal all
carry the same handful of facts in different columns, orders, units and date
conventions.

So a vendor is a **declaration**, never code. `--write-profile` proposes one from
a real file; a person corrects it where the guess was wrong; from then on that
vendor's exports import without anybody rediscovering the format. Supporting a
new AMCU is writing a small file.

A proposed profile is explicitly a draft. Everything the file cannot answer is
listed under `unresolved` rather than filled in with a plausible default, and the
profile is refused until those are settled:

- **`date_layout`** — which way round the dates read
- **`quantity_unit`** — litres or kilograms, which differ by about three per cent,
  larger than most divergences worth finding
- **`shift_values`** — what this vendor writes for morning and evening
- any column that was not identified, and any guess below 0.6 confidence

A default for any of these would be a silent decision about somebody's milk
payment.

---

# bench-capture

Records what an instrument sends, byte for byte, and afterwards says where the
readings live in those bytes.

The specification asks for "a representative analyser/controller interface
captured and mapped". Capturing has to come first and has to be dumb: at the
moment somebody is stood at a bench with a milk analyser, nobody knows the
protocol, and anything that parses while it records drops the bytes it did not
expect — which are the interesting ones.

```sh
go build -o bench-capture ./cmd/bench-capture

# a serial analyser — configure the port first, see below
stty -F /dev/ttyUSB0 9600 cs8 -cstopb -parity raw -echo
bench-capture --from /dev/ttyUSB0 --out bench.jsonl

bench-capture --listen :9100 --out bench.jsonl          # it pushes over TCP
bench-capture --dial 192.168.1.50:4001 --out bench.jsonl # it waits to be called

bench-capture --analyse bench.jsonl                      # afterwards
```

The port is configured with `stty` rather than in the tool. That keeps it free of
a serial library and its cgo, which means a bench laptop needs nothing installed
and the binary is the one already built for the platform.

## Marking

While recording, type what is happening and press enter.

```
  ← noted: known weight 5.00 L
14:27:04.458  17 bytes
  0000  53 54 2c 47 53 2c 2b 20  20 35 2e 30 30 20 4c 0d  |ST,GS,+  5.00 L.|
```

Those notes are the whole value of the recording. Bytes with nothing known
beside them are a puzzle; bytes recorded next to a known five-litre weight are a
Rosetta stone. Mark two different values of the same thing — five litres, then
twelve and a half — and the analysis can tell a field from a coincidence.

## What the analysis says

**How the messages were separated**, and on what evidence. A terminator every
chunk ends with is the instrument telling you where its messages end. Failing
that, the terminator is looked for in the stream, because a forty-byte line at
9600 baud usually arrives in two reads and has no terminator at either boundary.
Failing that, a uniform write size, then silence.

A recording that stops mid-message — which is how every bench session ends — has
its last stub discarded and says so. Kept, that stub would either hide a field or
shorten it, reporting a four-byte weight as one byte, and a parser written to
that would work until the reading reached double figures.

**What never changes and what does.** A constant run of text is a header, an
address or a unit marker. The reading lives among the varying offsets.

**Where the values you noted appear.** ASCII decimal, packed BCD and scaled
binary integers are searched, in both byte orders. A value is only reported when
it sits at the same offset in *every* frame recorded under that note — a match in
one frame is a coincidence.

Where two noted values land at different offsets but end at the same one, that is
one right-aligned field and it is reported as one, not two:

```
  12.5  from note "known weight 12.50 L"
      at offset 8, 5 bytes, as ASCII decimal
  5     from note "known weight 5.00 L"
      at offset 9, 4 bytes, as ASCII decimal

Notes
  the values found at offsets 8 and 9 all end at offset 12, so that is one
  right-aligned field occupying offsets 8–12, not several fields
```

Nothing is claimed to be decoded. Instrument protocols are various enough that a
confident wrong answer costs more than a set of observations, so the tool reports
what it saw and leaves the conclusion to a person.

## The recording

One JSON object per line, flushed as it happens, because a bench session ends
when somebody unplugs something and a buffered recording loses the minute that
mattered.

```json
{"t":"2026-08-25T14:27:03.858Z","mark":"known weight 5.00 L"}
{"t":"2026-08-25T14:27:04.457Z","hex":"53542c47532c2b2020352e3030204c0d0a","len":17}
```

Hex rather than base64: reading the file with your eyes is a supported use. Read
boundaries are preserved as they arrived, because where the writes fell is itself
evidence.


---

# amcu-import

Reads an export through its vendor profile and loads the collections.

The other half of `amcu-profile`. That one turns a real file into a description
of itself; this one turns the file plus that description into collections the
platform holds. Between them, supporting a new AMCU is writing a small file
rather than writing a parser.

```sh
go build -o amcu-import ./cmd/amcu-import

# what would be loaded, and what would not
amcu-import --profile smartdairy.json collections.csv

# load it
amcu-import --profile smartdairy.json \
            --into http://gateway:8000 \
            --tenant T_01HZ... --device D_01HZ... --session SE_01HZ... \
            collections.csv
```

A dry run is the default. Loading somebody's milk into a ledger is not something
to do because a flag was forgotten.

## What it refuses

The job is not to get as many rows in as possible. A row that cannot be trusted
is worse in than out: once in, it joins to everything else, and the settlement
computed from it looks as ordinary as any other.

| Refused | Because |
| --- | --- |
| A draft profile | Every unresolved question in it is a silent decision about somebody's milk payment |
| A file whose columns have moved | A vendor who reorders fat and SNF produces a file that imports perfectly and pays everyone wrongly |
| A file with duplicate producer-day-shift slots | Milk paid for twice |
| A file where one producer code is used for two names | Two people sharing a payment history |

The first two are refusals about the *description*; the rest are about the data.
Both stop the import before a single row is delivered.

## What it settles, and what it holds

Two of the things that would stop an import are questions a profile exists to
answer: which way round the dates read, and whether the quantity is litres or
kilograms. Where the profile declares them, they are settled — and reported as
settled, so somebody can check they were settled the way this file needs rather
than discovering later that they were not.

Rows that cannot be read are held with their reason and their line number, never
dropped. A file where nine rows in ten import cleanly and one silently vanishes
is worse than one that refuses outright, because nobody counts the rows.

A finding about particular rows is dealt with row by row rather than stopping
the file. Four bad rows in four thousand is four bad rows; refusing the whole
import there leaves 3,996 collections unrecorded until a source system is fixed
that may never be fixed.

## Loading the same file twice

The batch identifier is derived from the file's own content, so a re-import
produces the same batch and the platform recognises the replay. A timestamp or a
random identifier would make every re-import look like new milk and pay for it
again.

The sequence within a batch is the line the row was on, not a count of the rows
that imported. A counter renumbers everything after a row that was later fixed,
and duplicate detection is on the pair — so a corrected re-import would read as a
different set of records rather than the same ones.

## Provenance

Every record carries the batch it arrived in, the line it was on, the vendor, and
a hash of the source text. A year later somebody asks why a producer was paid
what they were, and the answer is the line from the file rather than a
recollection.
