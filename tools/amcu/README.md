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
