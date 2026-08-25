package profile

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// timeLike and parseTime keep the date parsing in one place.
type timeLike = time.Time

func parseTime(layout, v string) (time.Time, error) { return time.Parse(layout, strings.TrimSpace(v)) }

// inferRoles works out what each column is for.
//
// Two kinds of evidence are used and they are weighed differently. A header name
// is a strong hint but a lying one — vendors name a column FAT when it holds a
// rate, and a column that says QTY may be in kilograms. The values are the
// better evidence: milk fat lives between about 2 and 12 per cent everywhere on
// earth, and a column of numbers in that band, to two decimals, with thousands
// of distinct values, is fat whatever its header says.
//
// So the range test can override the name, and when the two disagree the
// disagreement is reported rather than resolved silently.
func inferRoles(cols []Column) {
	for i := range cols {
		c := &cols[i]
		byName, nameConf := roleFromHeader(c.Header)
		byValue, valueConf, valueWhy := roleFromValues(*c)

		switch {
		case byName != RoleUnknown && byValue != RoleUnknown && byName == byValue:
			c.Role, c.RoleConfidence = byName, min1(nameConf+valueConf*0.5)
			c.RoleReason = fmt.Sprintf("the header says so and %s", valueWhy)

		case byName != RoleUnknown && byValue != RoleUnknown && byName != byValue:
			// The values win, and the operator is told the header disagreed.
			c.Role, c.RoleConfidence = byValue, valueConf*0.8
			c.RoleReason = fmt.Sprintf(
				"%s — note the header %q suggests %s instead, which is worth checking",
				valueWhy, c.Header, byName)

		case byValue != RoleUnknown:
			c.Role, c.RoleConfidence, c.RoleReason = byValue, valueConf, valueWhy

		case byName != RoleUnknown:
			c.Role, c.RoleConfidence = byName, nameConf*0.7
			c.RoleReason = fmt.Sprintf("only the header %q suggests it; the values do not confirm", c.Header)
		}
	}

	// Where two columns claim the same role, the more confident keeps it. Two
	// fat columns is not a thing; one of them is something else.
	best := map[Role]int{}
	for i := range cols {
		if cols[i].Role == RoleUnknown {
			continue
		}
		j, seen := best[cols[i].Role]
		if !seen {
			best[cols[i].Role] = i
			continue
		}
		lose := i
		if cols[i].RoleConfidence > cols[j].RoleConfidence {
			best[cols[i].Role] = i
			lose = j
		}
		cols[lose].RoleReason = fmt.Sprintf(
			"another column matched %s more strongly, so this one is left unassigned (was: %s)",
			cols[lose].Role, cols[lose].RoleReason)
		cols[lose].Role = RoleUnknown
		cols[lose].RoleConfidence = 0
	}
}

func min1(f float64) float64 {
	if f > 1 {
		return 1
	}
	return f
}

// headerHints are the names these columns actually carry in the field, across
// vendors and the abbreviations operators type. They are hints only.
var headerHints = []struct {
	role  Role
	words []string
}{
	{RoleSociety, []string{"society", "dcs", "centre", "center", "vlcc", "bmc", "route", "unit"}},
	{RoleProducer, []string{"producer", "member", "farmer", "supplier", "pourer", "memberid", "memberno", "farmerid", "code"}},
	{RoleProducerName, []string{"name", "membername", "farmername", "producername"}},
	{RoleDate, []string{"date", "dt", "collectiondate", "shiftdate", "day"}},
	{RoleShift, []string{"shift", "session", "ampm", "milking"}},
	{RoleQuantity, []string{"qty", "quantity", "litre", "liter", "ltr", "lt", "weight", "kg", "volume", "milk"}},
	{RoleFat, []string{"fat", "fatpercent", "fatpct", "f"}},
	{RoleSNF, []string{"snf", "snfpercent", "solidsnotfat"}},
	{RoleCLR, []string{"clr", "lactometer", "lr", "density"}},
	{RoleRate, []string{"rate", "price", "perlitre", "perliter", "rateperltr"}},
	{RoleAmount, []string{"amount", "amt", "value", "payable", "total", "netamount"}},
	{RoleSample, []string{"sample", "sampleid", "sampleno"}},
	{RoleMachine, []string{"machine", "device", "amcu", "dpu", "terminal", "analyser", "analyzer"}},
	{RoleOperator, []string{"operator", "user", "clerk", "secretary", "incharge"}},
}

func roleFromHeader(h string) (Role, float64) {
	n := normalise(h)
	if n == "" {
		return RoleUnknown, 0
	}
	for _, hint := range headerHints {
		for _, w := range hint.words {
			if n == w {
				return hint.role, 0.7
			}
		}
	}
	for _, hint := range headerHints {
		for _, w := range hint.words {
			if len(w) >= 3 && strings.Contains(n, w) {
				return hint.role, 0.45
			}
		}
	}
	return RoleUnknown, 0
}

func normalise(h string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(h) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// band describes where a measurement actually lives.
//
// Two ranges, not one, because the possible range and the usual range answer
// different questions. Fat can be anything from 2 to 13 across cow, buffalo and
// a bad morning; but a column sitting at 7.8 to 9.2 is far more characteristic
// of solids-not-fat than of fat, even though both bands contain it. Scoring
// against the usual range is what tells them apart — testing possible ranges in
// order and taking the first match assigns whichever was listed first, which is
// how a file's fat and SNF columns end up swapped.
type band struct {
	role                 Role
	usualLo, usualHi     float64
	possibleLo, possible float64
	decimals             int
	what                 string
}

var bands = []band{
	{RoleFat, 3.0, 8.5, 2.0, 13.0, 1, "milk fat, across cow and buffalo"},
	{RoleSNF, 8.0, 9.3, 6.0, 12.0, 1, "solids-not-fat"},
	{RoleCLR, 25.0, 31.0, 15.0, 40.0, 0, "a lactometer reading"},
}

// score says how well a column sits in a band: 1 when its middle 90 per cent is
// entirely inside the usual range, falling off as it strays, and 0 once it
// leaves what is physically possible.
func (b band) score(c Column) float64 {
	lo, hi := c.P5, c.P95
	if hi <= 0 || lo < b.possibleLo || hi > b.possible {
		return 0
	}
	width := hi - lo
	if width <= 0 {
		width = 0.001
	}
	inside := math.Min(hi, b.usualHi) - math.Max(lo, b.usualLo)
	if inside <= 0 {
		return 0.15 // possible but not typical
	}
	s := inside / width
	if c.Decimals < b.decimals {
		s *= 0.75 // recorded coarser than this measurement usually is
	}
	return s
}

// roleFromValues identifies a column from what is in it.
//
// The bands are physical facts about milk rather than conventions, which is why
// they can outrank a header. They are scored rather than tested in order, and
// they are scored against the middle of the column rather than its extremes.
func roleFromValues(c Column) (Role, float64, string) {
	switch c.Kind {
	case KindDate:
		return RoleDate, 0.9, "the values parse as dates"

	case KindText:
		if c.Distinct <= 4 && c.NonEmpty > 20 && c.MaxLength <= 12 {
			if looksLikeShift(c.TopValues) {
				return RoleShift, 0.85, "there are only a handful of distinct short values and they read as milking sessions"
			}
			return RoleShift, 0.5, fmt.Sprintf("only %d distinct short values across %d rows, which is shift-shaped", c.Distinct, c.NonEmpty)
		}
		if c.Distinct > c.NonEmpty/3 && c.MaxLength > 6 {
			return RoleProducerName, 0.6, "the values are long and mostly distinct, which reads as names"
		}

	case KindDecimal:
		best, bestScore := band{}, 0.0
		for _, b := range bands {
			if s := b.score(c); s > bestScore {
				best, bestScore = b, s
			}
		}
		if bestScore >= 0.5 {
			return best.role, 0.6 + bestScore*0.3, fmt.Sprintf(
				"the middle of the column runs %.2f–%.2f, which sits in the usual range for %s (%.1f–%.1f)",
				c.P5, c.P95, best.what, best.usualLo, best.usualHi)
		}

		switch {
		case c.P5 >= 0.1 && c.P95 <= 200.0 && c.Mean < 30 && c.Decimals >= 1:
			return RoleQuantity, 0.55, fmt.Sprintf("the middle of the column runs %.2f–%.2f averaging %.1f, which is a plausible per-producer delivery", c.P5, c.P95, c.Mean)
		case c.P5 >= 10 && c.P95 <= 200 && c.Distinct < c.NonEmpty/4:
			return RoleRate, 0.5, fmt.Sprintf("the values run %.2f–%.2f and repeat heavily, which reads as a rate rather than a measurement", c.P5, c.P95)
		case c.P5 >= 0 && c.P95 > 200:
			return RoleAmount, 0.5, fmt.Sprintf("the values reach %.2f, too large for a measurement and shaped like money", c.P95)
		case bestScore > 0:
			return best.role, 0.4, fmt.Sprintf(
				"the values are within the possible range for %s but not its usual one (%.2f–%.2f) — worth confirming",
				best.what, c.P5, c.P95)
		}

	case KindInteger:
		switch {
		case c.Distinct > c.NonEmpty/2 && c.MaxLength <= 12:
			return RoleProducer, 0.5, "the values are mostly distinct short integers, which reads as a producer code"
		case c.Distinct <= 20 && c.NonEmpty > 50:
			return RoleSociety, 0.45, fmt.Sprintf("only %d distinct integers across %d rows, which reads as a society or centre code", c.Distinct, c.NonEmpty)
		}
	}
	return RoleUnknown, 0, ""
}

// shiftWords are what a milking session is called in the exports this will meet.
var shiftWords = []string{"m", "e", "am", "pm", "mor", "eve", "morning", "evening", "1", "2", "a", "b"}

func looksLikeShift(top []ValueCount) bool {
	matched := 0
	for _, v := range top {
		n := normalise(v.Value)
		for _, w := range shiftWords {
			if n == w {
				matched++
				break
			}
		}
	}
	return matched >= 2 && matched >= len(top)-1
}
