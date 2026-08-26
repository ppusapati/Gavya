// Package pathology finds the things wrong with a collection export before it
// is imported.
//
// Your specification asks for one real AMCU export "mapped, with its schema
// pathologies documented". This is what documents them. Every check here is
// something that has gone wrong in real dairy data and that a naive importer
// would carry into the platform while appearing to succeed:
//
//   - a producer code reused for a different person, so two people share a
//     payment history
//   - the same producer, shift and day recorded twice, so milk is paid for twice
//   - a date column that reads two ways, so a month of collections lands on the
//     wrong days
//   - a fat reading recorded to one decimal where the analyser gives two, so
//     every payment is rounded in the buyer's favour
//   - an amount that is not the quantity times the rate, so the file disagrees
//     with itself before anybody recomputes anything
//
// None of these is reported as a number to be minimised. Each is reported with
// the rows it was found in, because the point is for somebody who knows the
// society to look at them.
package pathology

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ppusapati/gavya/tools/amcu/internal/profile"
	"github.com/ppusapati/gavya/tools/amcu/internal/source"
)

// Severity says what an operator should do about a finding.
type Severity string

const (
	// Blocking: importing without resolving this produces wrong money.
	Blocking Severity = "blocking"
	// Serious: the import will work but something in the data is not what it seems.
	Serious Severity = "serious"
	// Note: worth knowing, usually about how the file was written.
	Note Severity = "note"
)

// Kind identifies a finding for a program rather than for a person.
//
// The title reads well and is formatted with counts and column names in it, so
// matching on it is matching on prose that will be reworded. A caller deciding
// what to do about a finding — an importer, say, working out whether a vendor
// profile already answers it — needs something stable.
type Kind string

const (
	KindAmbiguousDate      Kind = "ambiguous_date"
	KindPrecisionChange    Kind = "precision_change"
	KindMissingRoles       Kind = "missing_roles"
	KindImpossibleValue    Kind = "impossible_value"
	KindDuplicateSlot      Kind = "duplicate_slot"
	KindReusedProducerCode Kind = "reused_producer_code"
	KindArithmetic         Kind = "arithmetic"
	KindDamagedText        Kind = "damaged_text"
	KindBlankCriticalField Kind = "blank_critical_field"
	KindMultipleSocieties  Kind = "multiple_societies"
	KindUnstatedUnit       Kind = "unstated_unit"
)

// AnsweredByProfile reports whether a vendor profile can settle this finding.
//
// Two of them it can. A date column that reads two ways is answered by the
// profile's declared layout, and an unstated quantity unit by its declared unit
// — those are exactly the questions a profile exists to record the answers to,
// and a person answered them once when the profile was written.
//
// Nothing else is. A profile cannot make two people stop sharing a producer code
// or make a day's milk stop being recorded twice; those are facts about the
// data, and no amount of declaring changes them.
func (k Kind) AnsweredByProfile() bool {
	return k == KindAmbiguousDate || k == KindUnstatedUnit
}

// PerRow reports whether a finding is about particular rows rather than about
// the file as a whole.
//
// The distinction decides what an importer does with it. A file whose dates read
// two ways is wrong all the way through and nothing can be taken from it. A file
// where four rows out of four thousand have no producer is a file with four bad
// rows: refusing the whole import there means nothing is loaded until somebody
// fixes a source system that may never be fixed, while the other 3,996
// collections sit unrecorded.
//
// So a per-row finding is handled row by row — those rows are held with their
// reason and the rest are imported — and everything else stops the import.
func (k Kind) PerRow() bool {
	return k == KindBlankCriticalField || k == KindImpossibleValue || k == KindArithmetic
}

// Finding is one thing wrong, with enough detail to go and look.
type Finding struct {
	Severity Severity
	// Kind is what this is, for a program. Title is what it is, for a person.
	Kind  Kind
	Title string
	// Detail explains what was found and why it matters, in a sentence somebody
	// who runs a dairy would understand.
	Detail string
	// Rows are the 1-based data rows involved, capped so a report stays readable.
	Rows []int
	// Count is how many rows are affected in total, which may exceed len(Rows).
	Count int
	// Examples are concrete values, because an abstraction nobody can check is
	// not evidence.
	Examples []string
}

// Report is everything found, worst first.
type Report struct {
	Findings []Finding
}

func (r *Report) add(f Finding) {
	if f.Count == 0 {
		f.Count = len(f.Rows)
	}
	if len(f.Rows) > 12 {
		f.Rows = f.Rows[:12]
	}
	if len(f.Examples) > 6 {
		f.Examples = f.Examples[:6]
	}
	r.Findings = append(r.Findings, f)
}

// Inspect runs every check against the profiled export.
func Inspect(t *source.Table, p *profile.Table) *Report {
	r := &Report{}
	idx := roleIndex(p)

	checkAmbiguousDates(r, p)
	checkPrecisionChange(r, p)
	checkMissingRoles(r, idx)
	checkImpossibleMeasurements(r, t, p, idx)
	checkDuplicateCollections(r, t, idx)
	checkReusedProducerCodes(r, t, idx)
	checkArithmetic(r, t, idx)
	checkEncodingDamage(r, t, p)
	checkBlankCriticalFields(r, t, p, idx)
	checkSocietyChange(r, t, idx)
	checkQuantityUnit(r, p, idx)

	order := map[Severity]int{Blocking: 0, Serious: 1, Note: 2}
	sort.SliceStable(r.Findings, func(i, j int) bool {
		return order[r.Findings[i].Severity] < order[r.Findings[j].Severity]
	})
	return r
}

func roleIndex(p *profile.Table) map[profile.Role]int {
	idx := map[profile.Role]int{}
	for _, c := range p.Columns {
		if c.Role != profile.RoleUnknown {
			idx[c.Role] = c.Index
		}
	}
	return idx
}

func field(row []string, i int, ok bool) string {
	if !ok || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

// ---- checks ----

func checkAmbiguousDates(r *Report, p *profile.Table) {
	for _, c := range p.Columns {
		if amb, formats := c.Ambiguous(); amb {
			r.add(Finding{
				Severity: Blocking,
				Kind:     KindAmbiguousDate,
				Title:    fmt.Sprintf("Column %q could be day-first or month-first", c.Header),
				Detail: "Every value in this column parses under both conventions, so nothing in the file " +
					"says which it is. Read the wrong way, a month of collections lands on the wrong days " +
					"and every shift-level total is wrong. Ask the society which convention their system " +
					"writes, or find a row where the day exceeds twelve.",
				Examples: append([]string{"parses as: " + strings.Join(formats, ", ")}, c.Samples...),
			})
		}
	}
}

func checkPrecisionChange(r *Report, p *profile.Table) {
	for _, c := range p.Columns {
		if c.Role != profile.RoleFat && c.Role != profile.RoleSNF && c.Role != profile.RoleQuantity {
			continue
		}
		if changed, detail := c.PrecisionChanged(); changed {
			sev := Serious
			if c.Role == profile.RoleFat {
				sev = Blocking
			}
			why := "that digit is part of what a producer is paid"
			if c.Role == profile.RoleFat {
				why = "for fat, that digit is money — it multiplies straight into the rate"
			}
			r.add(Finding{
				Severity: sev,
				Kind:     KindPrecisionChange,
				Title:    fmt.Sprintf("%s lost precision partway through the file", c.Header),
				Detail: "Written with " + detail + ", which is far more low-precision values than dropped " +
					"trailing zeroes would explain. A measurement recorded to fewer places than the " +
					"instrument reports has already lost a digit, and " + why + ". Find out whether the " +
					"machine changed, the export changed, or two machines are feeding one file.",
				Examples: c.Samples,
			})
		}
	}
}

func checkMissingRoles(r *Report, idx map[profile.Role]int) {
	required := []struct {
		role profile.Role
		why  string
	}{
		{profile.RoleProducer, "without it a collection cannot be attributed to anybody"},
		{profile.RoleDate, "without it a collection cannot be placed in a settlement period"},
		{profile.RoleQuantity, "without it there is nothing to pay for"},
		{profile.RoleFat, "without it the rate cannot be applied"},
	}
	var missing []string
	for _, req := range required {
		if _, ok := idx[req.role]; !ok {
			missing = append(missing, fmt.Sprintf("%s (%s)", req.role, req.why))
		}
	}
	if len(missing) > 0 {
		r.add(Finding{
			Severity: Blocking,
			Kind:     KindMissingRoles,
			Title:    "Some fields a settlement needs were not found",
			Detail: "No column could be identified for: " + strings.Join(missing, "; ") +
				". Either the export does not carry them, or the guess failed and the mapping needs " +
				"correcting by hand.",
		})
	}
}

// bands are what milk actually measures. Outside them, either the reading is
// wrong or the column is not what it was taken for.
var bands = map[profile.Role]struct {
	lo, hi float64
	what   string
}{
	profile.RoleFat:      {2.0, 13.0, "milk fat, for cow and buffalo alike"},
	profile.RoleSNF:      {6.0, 12.0, "solids-not-fat"},
	profile.RoleCLR:      {15.0, 40.0, "a lactometer reading"},
	profile.RoleQuantity: {0.05, 2000.0, "one producer's delivery"},
}

func checkImpossibleMeasurements(r *Report, t *source.Table, p *profile.Table, idx map[profile.Role]int) {
	for role, band := range bands {
		i, ok := idx[role]
		if !ok {
			continue
		}
		var rows []int
		var examples []string
		for n, row := range t.Rows {
			v := field(row, i, true)
			if v == "" {
				continue
			}
			f, _, valid := parseNum(v)
			if !valid {
				continue
			}
			if f < band.lo || f > band.hi {
				rows = append(rows, n+1)
				examples = append(examples, fmt.Sprintf("row %d: %s", n+1, v))
			}
		}
		if len(rows) > 0 {
			r.add(Finding{
				Severity: Serious,
				Kind:     KindImpossibleValue,
				Title:    fmt.Sprintf("%d values are outside the range of %s", len(rows), band.what),
				Detail: fmt.Sprintf("Expected roughly %.1f to %.1f. A value outside that is a failed reading, "+
					"a wrong unit, or a column that is not what it was taken for. These rows should not be "+
					"settled until somebody has looked at them.", band.lo, band.hi),
				Rows: rows, Count: len(rows), Examples: examples,
			})
		}
	}
}

func checkDuplicateCollections(r *Report, t *source.Table, idx map[profile.Role]int) {
	pi, hasP := idx[profile.RoleProducer]
	di, hasD := idx[profile.RoleDate]
	if !hasP || !hasD {
		return
	}
	si, hasS := idx[profile.RoleShift]
	// A producer code is issued by a society and is only unique inside it. Two
	// centres both having a member 40 is ordinary; treating them as one person
	// would merge two people's milk.
	ci, hasC := idx[profile.RoleSociety]

	seen := map[string][]int{}
	for n, row := range t.Rows {
		key := field(row, ci, hasC) + "|" + field(row, pi, true) + "|" +
			field(row, di, true) + "|" + field(row, si, hasS)
		if strings.Trim(key, "|") == "" {
			continue
		}
		seen[key] = append(seen[key], n+1)
	}

	var rows []int
	var examples []string
	dupes := 0
	for key, at := range seen {
		if len(at) < 2 {
			continue
		}
		dupes++
		rows = append(rows, at...)
		if len(examples) < 6 {
			examples = append(examples, fmt.Sprintf("%s appears in rows %v", key, at))
		}
	}
	if dupes > 0 {
		sort.Ints(rows)
		shiftNote := ""
		if !hasS {
			shiftNote = " No shift column was identified, so a morning and an evening collection would " +
				"look identical here — confirm before treating these as duplicates."
		}
		r.add(Finding{
			Severity: Blocking,
			Kind:     KindDuplicateSlot,
			Title:    fmt.Sprintf("%d producer-day-shift slots appear more than once", dupes),
			Detail: "The same producer has more than one collection recorded for the same slot. Either the " +
				"export contains a correction written as a second row rather than a replacement, or milk " +
				"has been counted twice." + shiftNote,
			Rows: rows, Count: len(rows), Examples: examples,
		})
	}
}

func checkReusedProducerCodes(r *Report, t *source.Table, idx map[profile.Role]int) {
	pi, hasP := idx[profile.RoleProducer]
	ni, hasN := idx[profile.RoleProducerName]
	if !hasP || !hasN {
		return
	}
	// Scoped by society for the same reason: the same number at two centres is
	// two people, not one person who changed their name.
	ci, hasC := idx[profile.RoleSociety]

	names := map[string]map[string]bool{}
	for _, row := range t.Rows {
		code, name := field(row, pi, true), field(row, ni, true)
		if code == "" || name == "" {
			continue
		}
		key := code
		if hasC {
			key = field(row, ci, true) + "/" + code
		}
		if names[key] == nil {
			names[key] = map[string]bool{}
		}
		names[key][name] = true
	}

	var examples []string
	reused := 0
	for code, set := range names {
		if len(set) < 2 {
			continue
		}
		reused++
		if len(examples) < 6 {
			var ns []string
			for n := range set {
				ns = append(ns, n)
			}
			sort.Strings(ns)
			examples = append(examples, fmt.Sprintf("code %s: %s", code, strings.Join(ns, " / ")))
		}
	}
	if reused > 0 {
		r.add(Finding{
			Severity: Blocking,
			Kind:     KindReusedProducerCode,
			Title:    fmt.Sprintf("%d producer codes are used for more than one name", reused),
			Detail: "A code that means one person this year and another next year cannot be a producer " +
				"identity. This is exactly what the platform resolves at an instant rather than as a fact — " +
				"but the import needs to know when each meaning began, and the export does not say. Ask the " +
				"society for the reassignment dates before importing history.",
			Count: reused, Examples: examples,
		})
	}
}

func checkArithmetic(r *Report, t *source.Table, idx map[profile.Role]int) {
	qi, hasQ := idx[profile.RoleQuantity]
	ri, hasR := idx[profile.RoleRate]
	ai, hasA := idx[profile.RoleAmount]
	if !hasQ || !hasR || !hasA {
		return
	}
	var rows []int
	var examples []string
	for n, row := range t.Rows {
		q, _, okQ := parseNum(field(row, qi, true))
		rate, _, okR := parseNum(field(row, ri, true))
		amt, _, okA := parseNum(field(row, ai, true))
		if !okQ || !okR || !okA || q == 0 || rate == 0 {
			continue
		}
		want := q * rate
		// A rupee of tolerance absorbs the file's own rounding without hiding a
		// real disagreement.
		if math.Abs(want-amt) > 1.0 {
			rows = append(rows, n+1)
			if len(examples) < 6 {
				examples = append(examples, fmt.Sprintf("row %d: %.3f × %.2f = %.2f but the file says %.2f",
					n+1, q, rate, want, amt))
			}
		}
	}
	if len(rows) > 0 {
		r.add(Finding{
			Severity: Serious,
			Kind:     KindArithmetic,
			Title:    fmt.Sprintf("%d rows where the amount is not the quantity times the rate", len(rows)),
			Detail: "The file disagrees with its own arithmetic. That is not necessarily an error — a " +
				"recovery, an incentive or a slab rate would explain it — but whatever explains it is a " +
				"rule the shadow recomputation has to know about, and it is not in these columns.",
			Rows: rows, Count: len(rows), Examples: examples,
		})
	}
}

func checkEncodingDamage(r *Report, t *source.Table, p *profile.Table) {
	for _, c := range p.Columns {
		if c.Kind != profile.KindText {
			continue
		}
		var rows []int
		var examples []string
		for n, row := range t.Rows {
			v := field(row, c.Index, true)
			if v == "" {
				continue
			}
			if looksMojibake(v) {
				rows = append(rows, n+1)
				if len(examples) < 6 {
					examples = append(examples, fmt.Sprintf("row %d: %q", n+1, v))
				}
			}
		}
		if len(rows) > 0 {
			r.add(Finding{
				Severity: Serious,
				Kind:     KindDamagedText,
				Title:    fmt.Sprintf("%d values in %q look like damaged text", len(rows), c.Header),
				Detail: "These contain replacement characters or sequences typical of text decoded with the " +
					"wrong codepage. A producer whose name is unreadable cannot check their own statement, " +
					"which is the one thing the platform exists to let them do.",
				Rows: rows, Count: len(rows), Examples: examples,
			})
		}
	}
}

func looksMojibake(s string) bool {
	if strings.ContainsRune(s, '�') {
		return true
	}
	// The classic UTF-8-read-as-Latin-1 signatures.
	for _, sig := range []string{"Ã", "â", "Ð", "à¤"} {
		if strings.Contains(s, sig) {
			return true
		}
	}
	return false
}

func checkBlankCriticalFields(r *Report, t *source.Table, p *profile.Table, idx map[profile.Role]int) {
	for _, role := range []profile.Role{profile.RoleProducer, profile.RoleDate, profile.RoleQuantity, profile.RoleFat} {
		i, ok := idx[role]
		if !ok {
			continue
		}
		var rows []int
		for n, row := range t.Rows {
			if field(row, i, true) == "" {
				rows = append(rows, n+1)
			}
		}
		if len(rows) > 0 {
			r.add(Finding{
				Severity: Blocking,
				Kind:     KindBlankCriticalField,
				Title:    fmt.Sprintf("%d rows have no %s", len(rows), role),
				Detail: "A collection missing this cannot be settled. Importing it as a zero or a blank " +
					"would put a record in the ledger that nobody can act on and that quietly changes a " +
					"producer's totals.",
				Rows: rows, Count: len(rows),
			})
		}
	}
	_ = p
}

func checkSocietyChange(r *Report, t *source.Table, idx map[profile.Role]int) {
	i, ok := idx[profile.RoleSociety]
	if !ok {
		return
	}
	seen := map[string]int{}
	for _, row := range t.Rows {
		if v := field(row, i, true); v != "" {
			seen[v]++
		}
	}
	if len(seen) > 1 {
		var parts []string
		for v, n := range seen {
			parts = append(parts, fmt.Sprintf("%s (%d rows)", v, n))
		}
		sort.Strings(parts)
		r.add(Finding{
			Severity: Note,
			Kind:     KindMultipleSocieties,
			Title:    fmt.Sprintf("The export covers %d societies or centres", len(seen)),
			Detail: "More than one collection point is present. Each is a separate stream of authority and " +
				"a separate set of producers; importing them as one would merge two societies' member codes.",
			Examples: parts,
		})
	}
}

func checkQuantityUnit(r *Report, p *profile.Table, idx map[profile.Role]int) {
	i, ok := idx[profile.RoleQuantity]
	if !ok {
		return
	}
	var c profile.Column
	for _, col := range p.Columns {
		if col.Index == i {
			c = col
		}
	}
	// Milk is bought by weight in some places and by volume in others, and the
	// two differ by about three per cent. Nothing in a bare number says which.
	r.add(Finding{
		Severity: Note,
		Kind:     KindUnstatedUnit,
		Title:    "The unit of the quantity column is not stated anywhere in the file",
		Detail: fmt.Sprintf("Values run %.2f to %.2f averaging %.2f. Milk is bought by volume in some "+
			"places and by weight in others, and litres and kilograms differ by about three per cent — "+
			"which is larger than most of the divergences this platform exists to find. Confirm the unit "+
			"with the society before importing.", c.Min, c.Max, c.Mean),
		Examples: c.Samples,
	})
}

func parseNum(s string) (float64, int, bool) {
	var f float64
	var dp int
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, 0, false
	}
	s = strings.ReplaceAll(s, ",", "")
	n, err := fmt.Sscanf(s, "%g", &f)
	if n != 1 || err != nil {
		return 0, 0, false
	}
	if i := strings.Index(s, "."); i >= 0 {
		dp = len(s) - i - 1
	}
	return f, dp, true
}

var _ = time.Now
