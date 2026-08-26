package statement

import (
	"strings"
	"testing"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/money"

	"github.com/ppusapati/gavya/services/settlement-service/internal/domain"
)

func rs(t *testing.T, s string) money.Money {
	t.Helper()
	m, err := money.Parse(s, 2, "INR")
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return m
}

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// sample is a fortnight that adds up: five mornings of ten litres at 43.0000,
// one of which came to an odd figure, less an advance instalment and the dues.
//
//	5 × 430.00, with the third at 301.65   = 2021.65
//	less advance 1000.00 and dues 50.00    = 1050.00
//	                                         -------
//	                                          971.65
func sample(t *testing.T) *domain.Statement {
	t.Helper()
	paid := time.Date(2026, 3, 17, 10, 0, 0, 0, time.UTC)
	amounts := []string{"430.00", "430.00", "301.65", "430.00", "430.00"}
	var lines []*domain.Line
	for i, a := range amounts {
		lines = append(lines, &domain.Line{
			CollectedOn: day(2026, time.March, i+1),
			Shift:       "MORNING", Quantity: "10.000", QuantityUnit: "PER_LITRE",
			Rate: "43.0000", Amount: rs(t, a), CollectionID: "COL",
		})
	}
	return &domain.Statement{
		ProducerRef: "P_00123",
		Cycle: &domain.Cycle{
			SocietyCode: "SOC_KOTHAPALLI", Name: "March 1-15",
			PeriodStart: day(2026, time.March, 1), PeriodEnd: day(2026, time.March, 15),
			Currency: "INR",
		},
		Lines: lines,
		Deductions: []*domain.Deduction{
			{Kind: domain.Advance, Reference: "shed", Amount: rs(t, "1000.00")},
			{Kind: domain.SocietyDues, Amount: rs(t, "50.00")},
		},
		Quantities: map[string]string{"PER_LITRE": "50.000"},
		Payable: &domain.ProducerPayable{
			Gross: rs(t, "2021.65"), Deducted: rs(t, "1050.00"), Net: rs(t, "971.65"),
			CarriedForward: money.Zero(2, "INR"),
			Status:         domain.PayablePaid, PaidAt: &paid, PaymentReference: "CHQ 448120",
		},
	}
}

func render(t *testing.T, s *domain.Statement, o Options) string {
	t.Helper()
	out, err := Render(s, o)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return out
}

// The document, character for character.
//
// A golden test rather than a set of assertions about pieces of it, because a
// statement is read as a whole and the failures that matter here are layout
// failures — a column that shifts, a rule that is one short, a figure that
// lands under the wrong heading. Those are invisible to an assertion that only
// checks the number is present somewhere on the page.
func TestTheStatementIsTheDocumentAMemberIsHanded(t *testing.T) {
	const want = `` +
		`================================================================================` + "\n" +
		`                 Kothapalli Milk Producers Co-operative Society` + "\n" +
		`                             MILK PAYMENT STATEMENT` + "\n" +
		`================================================================================` + "\n" +
		`Member    P_00123                       Period    01 Mar 2026 - 15 Mar 2026     ` + "\n" +
		`Society   SOC_KOTHAPALLI                Cycle     March 1-15                    ` + "\n" +
		`--------------------------------------------------------------------------------` + "\n" +
		`Date         Shift    Quantity     Rate                                   Amount` + "\n" +
		`--------------------------------------------------------------------------------` + "\n" +
		`01 Mar 2026  Morning    10.000  43.0000                                   430.00` + "\n" +
		`02 Mar 2026  Morning    10.000  43.0000                                   430.00` + "\n" +
		`03 Mar 2026  Morning    10.000  43.0000                                   301.65` + "\n" +
		`04 Mar 2026  Morning    10.000  43.0000                                   430.00` + "\n" +
		`05 Mar 2026  Morning    10.000  43.0000                                   430.00` + "\n" +
		`--------------------------------------------------------------------------------` + "\n" +
		`Total litres            50.000` + "\n" +
		`Gross                                                                    2021.65` + "\n" +
		`Less:` + "\n" +
		`  Advance (shed)                                                        -1000.00` + "\n" +
		`  Society dues                                                            -50.00` + "\n" +
		`--------------------------------------------------------------------------------` + "\n" +
		`NET PAYABLE                                                               971.65` + "\n" +
		`================================================================================` + "\n" +
		`Paid 17 Mar 2026   Reference CHQ 448120` + "\n"

	got := render(t, sample(t), Options{
		Width:       80,
		SocietyName: "Kothapalli Milk Producers Co-operative Society",
	})
	if got != want {
		t.Errorf("the statement has changed.\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

// Every figure on the page ends in the same column.
//
// This is the property that lets a member run a finger down the deliveries and
// see that they come to the gross. Laid out against the column before it, the
// deliveries end near the middle of the page and the totals at the margin, and
// the two columns cannot be compared by eye at all.
func TestEveryAmountEndsInTheSameColumn(t *testing.T) {
	out := render(t, sample(t), Options{Width: 80})

	want := -1
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		var figure string
		switch {
		case strings.HasSuffix(line, "430.00"), strings.HasSuffix(line, "301.65"):
			figure = line[len(line)-6:]
		case strings.HasPrefix(line, "Gross"):
			figure = "2021.65"
		case strings.HasPrefix(line, "NET PAYABLE"):
			figure = "971.65"
		case strings.Contains(line, "-1000.00"):
			figure = "-1000.00"
		case strings.Contains(line, "-50.00"):
			figure = "-50.00"
		default:
			continue
		}
		if !strings.HasSuffix(line, figure) {
			t.Fatalf("expected %q at the end of %q", figure, line)
		}
		end := displayWidth(line)
		if want == -1 {
			want = end
		}
		if end != want {
			t.Errorf("%q ends at column %d; every other figure ends at %d, so they do not "+
				"line up and cannot be added by eye", line, end, want)
		}
	}
	if want != 80 {
		t.Errorf("the figures end at column %d on an 80-column page, not at the margin", want)
	}
}

// No line runs past the page, at any of the widths a society might print at.
func TestNothingRunsPastTheMargin(t *testing.T) {
	for _, width := range []int{60, 72, 80, 132} {
		out := render(t, sample(t), Options{Width: width, SocietyName: "Kothapalli Milk Producers"})
		for i, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
			if w := displayWidth(line); w > width {
				t.Errorf("width %d: line %d is %d characters and would wrap: %q", width, i+1, w, line)
			}
		}
	}
}

// The failure this package exists to prevent.
//
// A member has a very good fortnight, the amount does not fit the column, and
// the layout trims it: 10430.00 becomes 1043.00. A plausible figure, an order
// of magnitude out, on the one piece of paper they keep — and nothing anywhere
// reports an error, because a shortened string is still a string.
func TestAnAmountTooWideForThePageIsRefusedRatherThanTrimmed(t *testing.T) {
	s := sample(t)
	s.Lines[0].Amount = rs(t, "99999999.00")
	// 99999999.00 + 430.00 + 301.65 + 430.00 + 430.00
	s.Payable.Gross = rs(t, "100001590.65")
	s.Payable.Net = rs(t, "100000540.65")

	out, err := Render(s, Options{Width: 40})
	if err == nil {
		t.Fatalf("a statement was laid out on a page too narrow for its figures:\n%s", out)
	}
	var narrow *ErrTooNarrow
	if !asErr(err, &narrow) {
		t.Fatalf("the refusal is not about the page being too narrow: %v", err)
	}
	if narrow.Needed <= 40 {
		t.Errorf("the refusal says %d columns are needed, which is not more than the 40 given",
			narrow.Needed)
	}
	// And on a page wide enough, the whole figure survives.
	wide := render(t, s, Options{Width: narrow.Needed})
	if !strings.Contains(wide, "99999999.00") {
		t.Error("the large delivery does not appear in full on a page wide enough for it")
	}

}

// A statement whose own deliveries do not come to its own gross is refused.
//
// It is the document a member brings back to argue with, so it has to be
// arguable from. A page where the lines and the total disagree is worse than no
// page, because every figure on it looks like a figure.
func TestAStatementThatDoesNotAddUpIsNeverRendered(t *testing.T) {
	t.Run("the deliveries do not come to the gross", func(t *testing.T) {
		s := sample(t)
		s.Lines[0].Amount = rs(t, "431.00") // one paisa too many
		if _, err := Render(s, Options{Width: 80}); err == nil {
			t.Fatal("a statement was handed over whose deliveries do not come to its gross")
		} else if !strings.Contains(err.Error(), "2021.65") {
			t.Errorf("the refusal does not name the figures that disagree: %v", err)
		}
	})

	t.Run("the deductions do not come to what was taken", func(t *testing.T) {
		s := sample(t)
		s.Deductions = s.Deductions[:1] // the dues line goes missing
		if _, err := Render(s, Options{Width: 80}); err == nil {
			t.Fatal("a statement was handed over showing fewer deductions than were taken")
		}
	})

	t.Run("the gross less the deductions is not the net", func(t *testing.T) {
		s := sample(t)
		s.Payable.Net = rs(t, "971.66")
		if _, err := Render(s, Options{Width: 80}); err == nil {
			t.Fatal("a statement was handed over whose subtraction is wrong")
		}
	})
}

// A cycle that has not been gathered has no figures, and a page of headings with
// blanks where the money goes is a page somebody will read as zero.
func TestACycleThatHasNotBeenSettledProducesNoStatement(t *testing.T) {
	s := sample(t)
	s.Payable = nil
	_, err := Render(s, Options{Width: 80})
	if err == nil {
		t.Fatal("a statement was produced for a cycle that has not been gathered")
	}
	var none *ErrNoPayable
	if !asErr(err, &none) {
		t.Errorf("the refusal is not about there being no payable: %v", err)
	}
}

// A member reference in any script cannot move the money columns.
//
// This is the property the layout is arranged to give, and it is the one worth
// holding rather than an exact width for Telugu. displayWidth is an estimate —
// Indic conjuncts render narrower than their rune count — so the question is
// not whether the heading is perfectly justified but whether a name it measures
// badly can shift a decimal point. It cannot: free text lives in the heading
// and in label positions, and every figure is placed against the right margin.
//
// Measured by comparing whole lines against an ASCII statement rather than by
// calling displayWidth, because a test that measures with the function under
// test moves its own ruler when that function is wrong. An earlier version did
// exactly that, and counting bytes instead of runes passed it.
func TestAMemberReferenceCannotMoveTheMoneyColumns(t *testing.T) {
	ascii := strings.Split(render(t, sample(t), Options{Width: 80}), "\n")

	for _, ref := range []string{
		"కృష్ణ",       // Telugu, with combining vowel signs and a conjunct
		"राजेश कुमार", // Devanagari
		"P_00123",     // the ASCII original
		"a-very-long-member-reference-that-runs-past-its-cell-and-then-some-more",
	} {
		s := sample(t)
		s.ProducerRef = ref
		got := strings.Split(render(t, s, Options{Width: 80}), "\n")

		if len(got) != len(ascii) {
			t.Fatalf("%q: the statement has %d lines against %d", ref, len(got), len(ascii))
		}
		// One line carries the member reference. Every other line must be
		// byte-identical to the ASCII statement — same rules, same table, same
		// figures in the same columns.
		//
		// Located by its label rather than by a hardcoded index, because the
		// heading is a line shorter when no society name is given and an index
		// that is quietly wrong points the whole assertion at another line.
		memberLine := -1
		for i, line := range ascii {
			if strings.HasPrefix(line, "Member") {
				memberLine = i
				break
			}
		}
		if memberLine < 0 {
			t.Fatal("no line carries the member reference")
		}
		for i := range got {
			if i == memberLine {
				continue
			}
			if got[i] != ascii[i] {
				t.Errorf("%q: line %d changed when only the member reference did:\n"+
					"  ascii %q\n  got   %q", ref, i+1, ascii[i], got[i])
			}
		}
		// And the reference never runs into the period beside it.
		if strings.Count(got[memberLine], "Period") != 1 {
			t.Errorf("%q: the member line lost or duplicated the period: %q", ref, got[memberLine])
		}
		if len(got[memberLine]) > 0 && !strings.Contains(got[memberLine], "01 Mar 2026") {
			t.Errorf("%q: the period was pushed off the member line: %q", ref, got[memberLine])
		}
	}
}

// A reference too long for its cell is cut, and that is the one place cutting
// is allowed: a member knows their own name, and nobody reads a reference to
// check arithmetic. No amount goes through the same path.
func TestALongReferenceIsCutAndNoAmountEverIs(t *testing.T) {
	s := sample(t)
	s.ProducerRef = strings.Repeat("X", 200)
	out := render(t, s, Options{Width: 80})

	if !strings.Contains(out, "2021.65") || !strings.Contains(out, "971.65") {
		t.Errorf("a long member reference disturbed the figures:\n%s", out)
	}
	for i, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if len([]rune(line)) > 80 {
			t.Errorf("line %d runs to %d columns: %q", i+1, len([]rune(line)), line)
		}
	}

	// The cut leaves a space before whatever sits beside it. Cut to the full
	// cell instead and the line reads "...XXXXXXPeriod", which looks like one
	// word and hides where the reference stopped — so a member cannot tell
	// their reference was shortened, only that it looks wrong.
	for _, line := range strings.Split(out, "\n") {
		if idx := strings.Index(line, "Period"); idx > 0 {
			if line[idx-1] != ' ' {
				t.Errorf("the truncated reference runs straight into the next label: %q", line)
			}
			break
		}
	}
}

// Deductions carry a minus sign.
//
// A page of positive numbers with one of them quietly subtracted at the bottom
// is how a member comes away believing they were paid the gross, and the
// argument that follows is one the society cannot win with the same piece of
// paper.
func TestMoneyTakenBackIsShownAsTakenBack(t *testing.T) {
	out := render(t, sample(t), Options{Width: 80})
	for _, want := range []string{"-1000.00", "-50.00"} {
		if !strings.Contains(out, want) {
			t.Errorf("the statement does not show %s as a subtraction:\n%s", want, out)
		}
	}
}

// The same settlement printed twice is the same page.
//
// The quantities are held in a map, and ranging one directly puts litres above
// kilograms on one printing and below on the next. Two statements of the same
// fortnight that differ are two statements somebody has to explain.
func TestTheSamePeriodPrintsTheSamePageEveryTime(t *testing.T) {
	s := sample(t)
	s.Quantities = map[string]string{"PER_LITRE": "50.000", "PER_KG": "3.200"}
	// Kilograms would exceed the litres-only gross, so the figures move with it.
	first := render(t, s, Options{Width: 80})
	for i := 0; i < 12; i++ {
		if again := render(t, s, Options{Width: 80}); again != first {
			t.Fatalf("printing %d differs from the first:\n--- first ---\n%s\n--- again ---\n%s",
				i+2, first, again)
		}
	}
	// And litres come before kilograms, rather than in whichever order the map
	// happened to yield.
	litres := strings.Index(first, "Total litres")
	kg := strings.Index(first, "Total kg")
	if litres < 0 || kg < 0 {
		t.Fatalf("both units should appear:\n%s", first)
	}
	if litres > kg {
		t.Error("kilograms are listed before litres")
	}
}

// What happened to the money is on the page, in each of the three states it can
// be in. A member holding a statement that says nothing about payment cannot
// tell whether they have been paid.
func TestThePageSaysWhereTheMoneyGot(t *testing.T) {
	t.Run("paid", func(t *testing.T) {
		out := render(t, sample(t), Options{Width: 80})
		if !strings.Contains(out, "Paid 17 Mar 2026") || !strings.Contains(out, "CHQ 448120") {
			t.Errorf("a paid statement does not say when or against what:\n%s", out)
		}
	})

	t.Run("held", func(t *testing.T) {
		s := sample(t)
		s.Payable.Status = domain.PayableHeld
		s.Payable.PaidAt, s.Payable.PaymentReference = nil, ""
		s.Payable.HeldReason = "membership under question"
		out := render(t, s, Options{Width: 80})
		if !strings.Contains(out, "Payment held") || !strings.Contains(out, "membership under question") {
			t.Errorf("a held statement does not say it is held, or why:\n%s", out)
		}
	})

	t.Run("not yet paid", func(t *testing.T) {
		s := sample(t)
		s.Payable.Status = domain.Payable
		s.Payable.PaidAt, s.Payable.PaymentReference = nil, ""
		out := render(t, s, Options{Width: 80})
		if !strings.Contains(out, "Not yet paid") {
			t.Errorf("an unpaid statement does not say so:\n%s", out)
		}
	})
}

// A member who delivered nothing gets a page that says so.
//
// A table with no rows under it reads as a printing fault, and the member's
// question is then about the printer rather than about the fortnight.
func TestAMemberWhoDeliveredNothingIsToldSo(t *testing.T) {
	s := sample(t)
	s.Lines = nil
	s.Deductions = nil
	s.Quantities = map[string]string{}
	s.Payable.Gross = money.Zero(2, "INR")
	s.Payable.Deducted = money.Zero(2, "INR")
	s.Payable.Net = money.Zero(2, "INR")

	out := render(t, s, Options{Width: 80})
	if !strings.Contains(out, "No milk was delivered") {
		t.Errorf("an empty statement does not say why it is empty:\n%s", out)
	}
	if !strings.Contains(out, "0.00") {
		t.Errorf("an empty statement does not show a zero payable:\n%s", out)
	}
}

// A society supplies its own words, and the platform does not overwrite them.
func TestASocietySuppliesItsOwnWords(t *testing.T) {
	l := DefaultLabels()
	l.Net = "CHELLINCHAVALASINA MOTTAM"
	l.Morning = "Uda"
	out := render(t, sample(t), Options{Width: 80, Labels: l})
	if !strings.Contains(out, "CHELLINCHAVALASINA MOTTAM") {
		t.Errorf("the society's own wording for the net was not used:\n%s", out)
	}
	if strings.Contains(out, "NET PAYABLE") {
		t.Error("the English default was printed alongside the society's own wording")
	}
	if !strings.Contains(out, "Uda") || strings.Contains(out, "Morning") {
		t.Error("the society's own wording for the morning shift was not used")
	}
}

// The carried-forward line appears only when there is something to carry.
//
// A line reading "Carried forward 0.00" on every statement is noise on the one
// document a member actually reads, and noise is what makes the line that
// matters get skipped.
func TestCarriedForwardIsShownOnlyWhenThereIsSome(t *testing.T) {
	if out := render(t, sample(t), Options{Width: 80}); strings.Contains(out, "Carried forward") {
		t.Errorf("a statement with nothing carried forward shows the line anyway:\n%s", out)
	}

	s := sample(t)
	s.Payable.CarriedForward = rs(t, "1700.00")
	out := render(t, s, Options{Width: 80})
	if !strings.Contains(out, "Carried forward") || !strings.Contains(out, "1700.00") {
		t.Errorf("a statement with money carried forward does not say so:\n%s", out)
	}
}

// asErr is errors.As without importing errors into every assertion.
func asErr[T error](err error, target *T) bool {
	for err != nil {
		if v, ok := err.(T); ok {
			*target = v
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// displayWidth, against literal expectations.
//
// The layout tests cannot pin this down on their own. They compare rendered
// pages, and the money columns are anchored to the right margin precisely so
// that a bad width estimate cannot move them — so a width that is wrong shows
// up only as a ragged heading, which no assertion about figures will catch.
//
// The cases here are ones with no ambiguity. A combining acute is one column,
// not two, in every renderer there is; the argument about Indic conjuncts does
// not apply to it. So this is the claim the function actually makes, tested
// where the claim is exact.
func TestDisplayWidthCountsColumnsAndNotBytes(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int
		why  string
	}{
		{"", 0, "nothing is nothing"},
		{"P_00123", 7, "plain ASCII is its own length"},
		{"430.00", 6, "an amount is its own length"},

		// Written as an escape on purpose. Typed as a literal "é" this is
		// U+00E9, one precomposed rune, and the case tests nothing about
		// combining marks at all — which is what an earlier version of this
		// test did.
		{"e\u0301", 1, "e plus a combining acute is one column"},
		{"cafe\u0301", 4, "and does not widen the word it is in"},

		// Non-spacing marks (Mn) sit on the character before them. U+0943 is
		// the Devanagari vowel sign vocalic R.
		{"\u0915\u0943", 1, "a non-spacing vowel sign takes no column of its own"},

		// Spacing marks (Mc) do take a column, and Indic scripts use both.
		// U+093F is the Devanagari vowel sign I, which renders to the left of
		// its consonant and occupies its own advance. Counting it as zero
		// because it is a combining mark would be as wrong as counting bytes.
		{"\u0915\u093F", 2, "a spacing vowel sign occupies a column"},

		// Enclosing marks (Me) surround rather than follow.
		{"1\u20E3", 1, "an enclosing keycap encloses the digit"},
	} {
		if got := displayWidth(c.in); got != c.want {
			t.Errorf("displayWidth(%q) = %d, want %d — %s (it is %d bytes and %d runes)",
				c.in, got, c.want, c.why, len(c.in), len([]rune(c.in)))
		}
	}
}
