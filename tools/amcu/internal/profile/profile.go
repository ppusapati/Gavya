// Package profile describes what is actually in each column of a collection
// export, and guesses what each column is for.
//
// The guess matters because the alternative is an operator reading four hundred
// thousand rows of somebody else's export to work out which column is fat. The
// guess is always reported with its evidence and can always be overridden — a
// mapping this tool produces is a starting point, not an answer.
package profile

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/ppusapati/gavya/tools/amcu/internal/source"
)

// Role is what a column appears to hold. These are the fields a collection
// record needs before it can be settled.
type Role string

const (
	RoleUnknown      Role = ""
	RoleSociety      Role = "society_code"
	RoleProducer     Role = "producer_code"
	RoleProducerName Role = "producer_name"
	RoleDate         Role = "collected_on"
	RoleShift        Role = "shift"
	RoleQuantity     Role = "quantity"
	RoleFat          Role = "fat_percent"
	RoleSNF          Role = "snf_percent"
	RoleCLR          Role = "clr"
	RoleRate         Role = "rate"
	RoleAmount       Role = "amount"
	RoleSample       Role = "sample_id"
	RoleMachine      Role = "machine_id"
	RoleOperator     Role = "operator"
)

// Kind is the shape of the values in a column.
type Kind string

const (
	KindEmpty   Kind = "empty"
	KindInteger Kind = "integer"
	KindDecimal Kind = "decimal"
	KindDate    Kind = "date"
	KindText    Kind = "text"
	KindMixed   Kind = "mixed"
)

// Column is everything worth knowing about one column before mapping it.
type Column struct {
	Index  int
	Header string
	Kind   Kind

	Rows      int
	NonEmpty  int
	Distinct  int
	Blank     int
	MinLength int
	MaxLength int

	// Numeric summary, when the column holds numbers.
	Min, Max, Mean float64
	// P5 and P95 are the range with the tails cut off. Role inference uses these
	// rather than Min and Max, because a single failed analyser reading — which
	// every real export contains — would otherwise push a whole column out of
	// the band that identifies it. That is not hypothetical: one 19.4 in a fat
	// column is enough to make it look like a quantity.
	P5, P95 float64
	// Decimals is the most decimal places seen. A fat column recorded to one
	// place when the analyser reports two has already lost a digit that decides
	// what a producer is paid.
	Decimals int
	// DecimalHistogram counts how many values carried each number of decimal
	// places, which is how a silent precision change shows itself.
	DecimalHistogram map[int]int

	// DateFormats are every layout the values parse under. More than one, and
	// the file is ambiguous: 03/04 is two different days in two conventions.
	DateFormats []string

	Samples []string
	// TopValues are the most common values with their counts, which is what
	// identifies a shift or a code column at a glance.
	TopValues []ValueCount

	Role Role
	// RoleReason is why that role was chosen, so an operator can disagree with
	// the reasoning rather than just the answer.
	RoleReason string
	// RoleConfidence is 0..1. Anything below 0.6 should be read as a suggestion.
	RoleConfidence float64
}

type ValueCount struct {
	Value string
	Count int
}

// Table is the profiled export.
type Table struct {
	Shape   source.Shape
	Columns []Column
}

// Profile examines every column and infers what it holds.
func Profile(t *source.Table) *Table {
	out := &Table{Shape: t.Shape}
	for i, h := range t.Header {
		out.Columns = append(out.Columns, profileColumn(i, h, t.Rows))
	}
	inferRoles(out.Columns)
	return out
}

func profileColumn(idx int, header string, rows [][]string) Column {
	c := Column{
		Index:            idx,
		Header:           strings.TrimSpace(header),
		Rows:             len(rows),
		MinLength:        math.MaxInt32,
		DecimalHistogram: map[int]int{},
		Min:              math.Inf(1),
		Max:              math.Inf(-1),
	}

	distinct := map[string]int{}
	var ints, decs, dates, texts int
	var sum float64
	var numbers []float64
	formats := map[string]int{}

	for _, r := range rows {
		if idx >= len(r) {
			c.Blank++
			continue
		}
		v := strings.TrimSpace(r[idx])
		if v == "" {
			c.Blank++
			continue
		}
		c.NonEmpty++
		distinct[v]++

		if l := len([]rune(v)); l < c.MinLength {
			c.MinLength = l
		} else if l > c.MaxLength {
			c.MaxLength = l
		}

		if f, dp, ok := parseNumber(v); ok {
			if dp == 0 {
				ints++
			} else {
				decs++
			}
			c.DecimalHistogram[dp]++
			if dp > c.Decimals {
				c.Decimals = dp
			}
			sum += f
			numbers = append(numbers, f)
			if f < c.Min {
				c.Min = f
			}
			if f > c.Max {
				c.Max = f
			}
			continue
		}
		if fs := dateLayouts(v); len(fs) > 0 {
			dates++
			for _, f := range fs {
				formats[f]++
			}
			continue
		}
		texts++
	}

	if c.MinLength == math.MaxInt32 {
		c.MinLength = 0
	}
	c.Distinct = len(distinct)

	switch {
	case c.NonEmpty == 0:
		c.Kind = KindEmpty
		c.Min, c.Max = 0, 0
	case dates > c.NonEmpty*3/4:
		c.Kind = KindDate
		c.Min, c.Max = 0, 0
	case decs > 0 && ints+decs > c.NonEmpty*3/4:
		c.Kind = KindDecimal
		c.Mean = sum / float64(ints+decs)
	case ints > c.NonEmpty*3/4:
		c.Kind = KindInteger
		c.Mean = sum / float64(ints)
	case texts > c.NonEmpty*3/4:
		c.Kind = KindText
		c.Min, c.Max = 0, 0
	default:
		c.Kind = KindMixed
		if n := ints + decs; n > 0 {
			c.Mean = sum / float64(n)
		} else {
			c.Min, c.Max = 0, 0
		}
	}
	if math.IsInf(c.Min, 1) {
		c.Min = 0
	}
	if math.IsInf(c.Max, -1) {
		c.Max = 0
	}
	c.P5, c.P95 = percentiles(numbers)

	for f := range formats {
		c.DateFormats = append(c.DateFormats, f)
	}
	sort.Strings(c.DateFormats)

	type kv struct {
		v string
		n int
	}
	all := make([]kv, 0, len(distinct))
	for v, n := range distinct {
		all = append(all, kv{v, n})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].n != all[j].n {
			return all[i].n > all[j].n
		}
		return all[i].v < all[j].v
	})
	for i := 0; i < len(all) && i < 8; i++ {
		c.TopValues = append(c.TopValues, ValueCount{all[i].v, all[i].n})
	}
	for i := 0; i < len(all) && i < 5; i++ {
		c.Samples = append(c.Samples, all[i].v)
	}

	return c
}

// percentiles returns the 5th and 95th, which describe where a column's values
// actually live rather than how far its worst reading strayed.
func percentiles(v []float64) (float64, float64) {
	if len(v) == 0 {
		return 0, 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	at := func(q float64) float64 {
		i := int(q * float64(len(s)-1))
		if i < 0 {
			i = 0
		}
		if i >= len(s) {
			i = len(s) - 1
		}
		return s[i]
	}
	return at(0.05), at(0.95)
}

// parseNumber reads a value as a number, returning how many decimal places it
// was written with. The count matters as much as the value: a fat column that
// suddenly changes from two places to one has lost precision somewhere upstream.
func parseNumber(s string) (float64, int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, 0, false
	}
	// A thousands separator is only a separator when it groups three digits.
	if strings.Count(s, ",") > 0 && strings.Count(s, ".") > 0 {
		s = strings.ReplaceAll(s, ",", "")
	} else if i := strings.LastIndex(s, ","); i >= 0 && len(s)-i-1 == 3 {
		s = strings.ReplaceAll(s, ",", "")
	} else {
		// Otherwise a comma is a decimal point, as much of the world writes it.
		s = strings.Replace(s, ",", ".", 1)
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, 0, false
	}
	dp := 0
	if i := strings.Index(s, "."); i >= 0 {
		dp = len(s) - i - 1
	}
	return f, dp, true
}

// layouts are the ways a date is written in a collection export. Both the
// day-first and month-first orders are here on purpose: which one a file uses
// cannot be told from a value like 03/04/2026, and pretending otherwise is how
// a month of collections lands on the wrong days.
var layouts = []string{
	"2006-01-02", "2006/01/02", "02-01-2006", "02/01/2006", "01-02-2006", "01/02/2006",
	"02-Jan-2006", "02-Jan-06", "2006-01-02 15:04:05", "02/01/2006 15:04",
	"01/02/2006 15:04", "20060102", "02.01.2006",
}

func dateLayouts(v string) []string {
	var out []string
	for _, l := range layouts {
		if _, err := parseWith(l, v); err == nil {
			out = append(out, l)
		}
	}
	return out
}
func parseWith(layout, v string) (t timeLike, err error) { return parseTime(layout, v) }

// Ambiguous reports whether a date column could be read two ways, and which.
func (c Column) Ambiguous() (bool, []string) {
	if c.Kind != KindDate || len(c.DateFormats) < 2 {
		return false, nil
	}
	dayFirst, monthFirst := false, false
	for _, f := range c.DateFormats {
		if strings.HasPrefix(f, "02") {
			dayFirst = true
		}
		if strings.HasPrefix(f, "01") {
			monthFirst = true
		}
	}
	if dayFirst && monthFirst {
		return true, c.DateFormats
	}
	return false, nil
}

// PrecisionChanged reports whether the column genuinely lost precision partway
// through, as opposed to merely dropping trailing zeroes.
//
// This distinction is the whole difficulty. A file that writes 4.35 and 4.3 has
// not changed precision — 4.30 written without its trailing zero looks exactly
// like a one-decimal value, and about one in ten two-decimal readings ends in
// zero. Flagging that would bury a real finding under noise on every column of
// every export.
//
// So the test is whether the low-precision values are far commoner than
// suppression alone predicts. A tenth of the rows is what trailing zeroes look
// like; a third of them is a machine or an export that changed.
func (c Column) PrecisionChanged() (bool, string) {
	if len(c.DecimalHistogram) < 2 {
		return false, ""
	}

	total := 0
	maxPlaces := 0
	for dp, n := range c.DecimalHistogram {
		total += n
		if dp > maxPlaces {
			maxPlaces = dp
		}
	}
	if total == 0 || maxPlaces == 0 {
		return false, ""
	}

	// How many rows would sit below the top precision if the only cause were
	// trailing zeroes: a tenth per place dropped, compounding.
	expected := 0.0
	for dp, n := range c.DecimalHistogram {
		if dp >= maxPlaces {
			continue
		}
		_ = n
		share := 1.0
		for k := 0; k < maxPlaces-dp; k++ {
			share *= 0.1
		}
		expected += share * float64(total)
	}
	below := 0
	for dp, n := range c.DecimalHistogram {
		if dp < maxPlaces {
			below += n
		}
	}
	// Twice what suppression predicts, and at least a twentieth of the file, before
	// this is worth anybody's attention.
	if float64(below) < expected*2 || float64(below) < float64(total)*0.05 {
		return false, ""
	}
	places := make([]int, 0, len(c.DecimalHistogram))
	for dp := range c.DecimalHistogram {
		places = append(places, dp)
	}
	sort.Ints(places)
	var parts []string
	for _, dp := range places {
		parts = append(parts, fmt.Sprintf("%d places in %d rows", dp, c.DecimalHistogram[dp]))
	}
	return true, strings.Join(parts, ", ")
}
