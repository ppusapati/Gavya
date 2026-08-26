// Package statement renders a settlement as the document a producer is handed.
//
// Fixed-width text, because that is what the printers in these societies are.
// A village co-operative has a dot-matrix or a thermal roll printer and a
// clerk who tears the sheet along the perforation; it does not have a browser.
// Text also has the property that matters most here: what the test asserts is
// character-for-character what comes out of the printer, so a column that
// silently loses a digit is a failing test rather than a complaint next month.
//
// The one thing this must never do is show a number that is not the number.
// Truncating 10430.00 into a seven-character column gives 1043.00 — a plausible
// figure, an order of magnitude out, on the single piece of paper a farmer
// keeps. So every column is measured against its content before anything is
// written, and a statement that will not fit is refused rather than trimmed.
package statement

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/ppusapati/gavya/libs/integrity/money"

	"github.com/ppusapati/gavya/services/settlement-service/internal/domain"
)

// Labels are the words on the page.
//
// Overridable in full, because the platform does not get to decide what
// language a society hands its members. The defaults are English and are
// defaults rather than translations: this file does not contain a Telugu or
// Hindi statement, because writing one without a speaker of the language would
// be putting words in a co-operative's mouth on a document its members sign
// for. A society supplies its own.
type Labels struct {
	Title      string
	Member     string
	Society    string
	Period     string
	Cycle      string
	Date       string
	Shift      string
	Quantity   string
	Rate       string
	Amount     string
	Morning    string
	Evening    string
	Total      string
	Gross      string
	Less       string
	Net        string
	CarriedFwd string
	Paid       string
	Reference  string
	Held       string
	NotYetPaid string
	NoMilk     string
}

// DefaultLabels is the English set.
func DefaultLabels() Labels {
	return Labels{
		Title:  "MILK PAYMENT STATEMENT",
		Member: "Member", Society: "Society", Period: "Period", Cycle: "Cycle",
		Date: "Date", Shift: "Shift", Quantity: "Quantity", Rate: "Rate", Amount: "Amount",
		Morning: "Morning", Evening: "Evening",
		Total: "Total", Gross: "Gross", Less: "Less", Net: "NET PAYABLE",
		CarriedFwd: "Carried forward", Paid: "Paid", Reference: "Reference",
		Held: "Payment held", NotYetPaid: "Not yet paid",
		NoMilk: "No milk was delivered in this period.",
	}
}

type Options struct {
	// Width is how many characters the printer prints on a line.
	//
	// A real setting rather than a constant: the common machines are 80-column
	// dot-matrix and 40- or 42-column thermal, and a statement laid out for one
	// is unreadable on the other. Unlike the decisions this platform refuses to
	// default, getting this wrong is immediately visible to whoever prints the
	// first page, so it has a default.
	Width int

	// SocietyName is what the co-operative calls itself, which is not the code
	// the platform knows it by. A member recognises "Kothapalli Milk Producers"
	// and has never seen "SOC_KOTHAPALLI".
	SocietyName string

	Labels Labels
}

const (
	// DefaultWidth is the 80-column dot-matrix that most of these societies own.
	DefaultWidth = 80
	// MinWidth is below the width of the money columns alone; nothing sensible
	// can be laid out under it.
	MinWidth = 32
	// gap is the space between columns.
	gap = 2
)

// ErrNoPayable is a statement for a cycle that has not been settled yet.
type ErrNoPayable struct{ ProducerRef string }

func (e *ErrNoPayable) Error() string {
	return fmt.Sprintf("there is no payable for %s in this cycle, so there is no statement to "+
		"hand over: the cycle has not been gathered", e.ProducerRef)
}

// ErrTooNarrow is a page that cannot hold the figures without losing digits.
type ErrTooNarrow struct {
	Width  int
	Needed int
}

func (e *ErrTooNarrow) Error() string {
	return fmt.Sprintf("this statement needs %d columns and the page is %d; printing it here "+
		"would cut digits off amounts, so it is refused instead", e.Needed, e.Width)
}

// ErrDoesNotReconcile is a document whose own figures disagree.
type ErrDoesNotReconcile struct{ Detail string }

func (e *ErrDoesNotReconcile) Error() string {
	return "this statement does not reconcile and must not be handed to anybody: " + e.Detail
}

// Render lays out one producer's settlement.
func Render(s *domain.Statement, o Options) (string, error) {
	if s == nil || s.Cycle == nil {
		return "", fmt.Errorf("there is no settlement to render")
	}
	if o.Width == 0 {
		o.Width = DefaultWidth
	}
	if o.Width < MinWidth {
		return "", &ErrTooNarrow{Width: o.Width, Needed: MinWidth}
	}
	if o.Labels == (Labels{}) {
		o.Labels = DefaultLabels()
	}
	if s.Payable == nil {
		return "", &ErrNoPayable{ProducerRef: s.ProducerRef}
	}
	if err := reconciles(s); err != nil {
		return "", err
	}

	l := o.Labels
	amounts := collectAmounts(s)

	// Columns are measured against what actually goes in them. A width guessed
	// from a typical statement is a width that is wrong for the atypical one,
	// and the atypical one is the large payment.
	c := columns{
		date:  displayWidth(formatDate(time.Now())),
		shift: max(displayWidth(l.Morning), displayWidth(l.Evening)),
		qty:   displayWidth(l.Quantity),
		rate:  displayWidth(l.Rate),
		amt:   displayWidth(l.Amount),
		width: o.Width,
	}
	c.shift = max(c.shift, displayWidth(l.Shift))
	c.date = max(c.date, displayWidth(l.Date))
	for _, line := range s.Lines {
		c.qty = max(c.qty, displayWidth(line.Quantity))
		c.rate = max(c.rate, displayWidth(line.Rate))
	}
	for _, a := range amounts {
		c.amt = max(c.amt, displayWidth(a))
	}
	for _, total := range s.Quantities {
		c.qty = max(c.qty, displayWidth(total))
	}

	needed := c.date + c.shift + c.qty + c.rate + c.amt + 4*gap
	if needed > o.Width {
		return "", &ErrTooNarrow{Width: o.Width, Needed: needed}
	}

	var b strings.Builder
	rule := func(ch string) { b.WriteString(strings.Repeat(ch, o.Width) + "\n") }

	// Heading.
	rule("=")
	if o.SocietyName != "" {
		b.WriteString(centre(o.SocietyName, o.Width) + "\n")
	}
	b.WriteString(centre(l.Title, o.Width) + "\n")
	rule("=")

	// Who and when. Two columns so the page does not run to six lines of
	// preamble before the milk.
	half := o.Width / 2
	b.WriteString(pair(l.Member, s.ProducerRef, half) +
		pair(l.Period, formatDate(s.Cycle.PeriodStart)+" - "+formatDate(s.Cycle.PeriodEnd), o.Width-half) + "\n")
	b.WriteString(pair(l.Society, s.Cycle.SocietyCode, half) +
		pair(l.Cycle, s.Cycle.Name, o.Width-half) + "\n")
	rule("-")

	// The milk.
	b.WriteString(row(c, l.Date, l.Shift, l.Quantity, l.Rate, l.Amount) + "\n")
	rule("-")
	if len(s.Lines) == 0 {
		b.WriteString(l.NoMilk + "\n")
	}
	for _, line := range s.Lines {
		b.WriteString(row(c,
			formatDate(line.CollectedOn), shiftLabel(line.Shift, l),
			line.Quantity, line.Rate, line.Amount.String()) + "\n")
	}
	rule("-")

	// What it came to.
	//
	// The quantity total lands in the quantity column, under the quantities it
	// is the sum of, rather than in the money column. Put in the money column it
	// sits directly above the gross in the same alignment, and 50.000 above
	// 2021.65 reads as two amounts.
	for _, q := range orderedQuantities(s.Quantities) {
		b.WriteString(endAt(l.Total+" "+unitLabel(q[0]), q[1], c.qtyEdge(), c.qty) + "\n")
	}
	b.WriteString(figure(l.Gross, "", s.Payable.Gross.String(), o.Width, c.amt) + "\n")

	// What was taken back.
	if len(s.Deductions) > 0 {
		b.WriteString(l.Less + ":\n")
		for _, d := range s.Deductions {
			what := deductionLabel(d)
			// Deductions print with a leading minus. A page of positive numbers
			// with one of them quietly subtracted at the bottom is how a member
			// ends up believing they were paid the gross.
			b.WriteString(figure("  "+what, "", "-"+d.Amount.String(), o.Width, c.amt) + "\n")
		}
	}

	rule("-")
	b.WriteString(figure(l.Net, "", s.Payable.Net.String(), o.Width, c.amt) + "\n")
	if !s.Payable.CarriedForward.IsZero() {
		b.WriteString(figure(l.CarriedFwd, "", s.Payable.CarriedForward.String(), o.Width, c.amt) + "\n")
	}
	rule("=")

	// Where the money got to.
	switch s.Payable.Status {
	case domain.PayablePaid:
		paid := l.Paid
		if s.Payable.PaidAt != nil {
			paid += " " + formatDate(*s.Payable.PaidAt)
		}
		if s.Payable.PaymentReference != "" {
			paid += "   " + l.Reference + " " + s.Payable.PaymentReference
		}
		b.WriteString(paid + "\n")
	case domain.PayableHeld:
		b.WriteString(l.Held + ": " + s.Payable.HeldReason + "\n")
	default:
		b.WriteString(l.NotYetPaid + "\n")
	}

	out := b.String()
	// Checked after the fact as well as designed for, because the measurement
	// above is arithmetic on what the content is expected to be and this is the
	// page. A line over the width is one the printer wraps, and a wrapped
	// amount column is a statement whose figures no longer line up under their
	// headings.
	for i, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if w := displayWidth(line); w > o.Width {
			return "", fmt.Errorf("line %d came to %d characters on a %d-column page, so it "+
				"would wrap and the figures would no longer line up: %q", i+1, w, o.Width, line)
		}
	}
	return out, nil
}

// reconciles refuses a document whose own figures disagree.
//
// A statement is the thing a member brings back to argue with, so it has to be
// arguable from: the deliveries on it must come to the gross on it, and the
// deductions on it must come to the amount subtracted. A page where they do not
// is worse than no page, because every figure on it looks like a figure.
func reconciles(s *domain.Statement) error {
	p := s.Payable
	var lines int64
	for _, l := range s.Lines {
		if l.Amount.Currency != p.Gross.Currency || l.Amount.Scale != p.Gross.Scale {
			return &ErrDoesNotReconcile{Detail: fmt.Sprintf(
				"a delivery on %s is written as %s and the total as %s, which are not the same "+
					"kind of number", formatDate(l.CollectedOn), l.Amount, p.Gross)}
		}
		lines += l.Amount.Value
	}
	if lines != p.Gross.Value {
		return &ErrDoesNotReconcile{Detail: fmt.Sprintf(
			"the deliveries listed come to %s but the statement reads a gross of %s",
			minor(lines, p.Gross), p.Gross)}
	}

	var taken int64
	for _, d := range s.Deductions {
		taken += d.Amount.Value
	}
	if taken != p.Deducted.Value {
		return &ErrDoesNotReconcile{Detail: fmt.Sprintf(
			"the deductions listed come to %s but the statement reads %s taken",
			minor(taken, p.Deducted), p.Deducted)}
	}
	if p.Gross.Value-p.Deducted.Value != p.Net.Value {
		return &ErrDoesNotReconcile{Detail: fmt.Sprintf(
			"%s less %s is not %s", p.Gross, p.Deducted, p.Net)}
	}
	return nil
}

func minor(v int64, like money.Money) money.Money {
	return money.Money{Value: v, Scale: like.Scale, Currency: like.Currency}
}

// collectAmounts is every string that lands in the right-hand column.
//
// All of them, including the ones that are easy to forget: the deductions carry
// a minus sign that makes them one character wider than they look, and the
// quantity totals share the column with the money. A column measured against
// the deliveries alone is a column that fits until the fortnight a member
// delivers enough to be paid five figures.
func collectAmounts(s *domain.Statement) []string {
	out := []string{
		s.Payable.Gross.String(),
		s.Payable.Net.String(),
		s.Payable.CarriedForward.String(),
	}
	for _, l := range s.Lines {
		out = append(out, l.Amount.String())
	}
	for _, d := range s.Deductions {
		out = append(out, "-"+d.Amount.String())
	}
	for _, total := range s.Quantities {
		out = append(out, total)
	}
	return out
}

type columns struct {
	date, shift, qty, rate, amt int
	// width is the page, kept here because the amount column is positioned
	// against the right margin rather than against the column before it.
	width int
}

// qtyEdge is the column the quantity figures end in.
func (c columns) qtyEdge() int { return c.date + gap + c.shift + gap + c.qty }

// row lays out one line of the milk table.
//
// The amount goes hard against the right margin, in the same column as the
// gross, the deductions and the net below it. Laid out against the column
// before it instead, the deliveries end around the middle of the page and the
// totals at the edge — and a member cannot run their finger down the deliveries
// and see that they come to the gross, which is the one thing this page exists
// to let them do.
func row(c columns, date, shift, qty, rate, amt string) string {
	sp := strings.Repeat(" ", gap)
	left := padRight(date, c.date) + sp + padRight(shift, c.shift) + sp +
		padLeft(qty, c.qty) + sp + padLeft(rate, c.rate)
	return endAt(left, amt, c.width, c.amt)
}

// endAt puts right hard against the margin, after left.
func endAt(left, right string, width, rightWidth int) string {
	pad := width - rightWidth - displayWidth(left)
	if pad < 1 {
		pad = 1
	}
	return strings.TrimRight(left+strings.Repeat(" ", pad)+padLeft(right, rightWidth), " ")
}

// figure puts a label on the left and an amount hard against the right margin,
// so every total on the page sits in the same column as the deliveries above it.
func figure(label, note, amount string, width, amtWidth int) string {
	left := label
	if note != "" {
		left += "  " + note
	}
	pad := width - amtWidth - displayWidth(left)
	if pad < 1 {
		pad = 1
	}
	return left + strings.Repeat(" ", pad) + padLeft(amount, amtWidth)
}

// pair renders "Label   value" inside a fixed cell.
func pair(label, value string, width int) string {
	const labelWidth = 9
	s := padRight(label, labelWidth) + " " + value
	if displayWidth(s) > width {
		// Truncating a name is survivable — the member knows their own name.
		// Truncating a figure is not, which is why no amount goes through here.
		//
		// Cut one short of the cell so a space always survives between this and
		// whatever sits beside it. Cut to the full width and a long reference
		// ends flush against the next label, giving "…referencePeriod" — which
		// reads as one word and hides where the reference stopped.
		s = truncate(s, width-1)
	}
	return padRight(s, width)
}

func centre(s string, width int) string {
	w := displayWidth(s)
	if w >= width {
		return truncate(s, width)
	}
	left := (width - w) / 2
	return strings.Repeat(" ", left) + s
}

// shiftLabel turns the stored value into the word a member reads.
//
// The stored values are procurement's, copied onto the line as plain strings
// rather than imported as its type: settlement does not depend on procurement's
// Go package, only on its wire. An unrecognised value prints as it is stored,
// which is honest — better a member sees "AFTERNOON" and asks than sees a blank
// where the shift should be.
func shiftLabel(shift string, l Labels) string {
	switch shift {
	case "MORNING":
		return l.Morning
	case "EVENING":
		return l.Evening
	}
	return shift
}

func unitLabel(unit string) string {
	switch unit {
	case "PER_LITRE":
		return "litres"
	case "PER_KG":
		return "kg"
	}
	return unit
}

// deductionLabel is what a member is told the money went to.
func deductionLabel(d *domain.Deduction) string {
	name := map[domain.RecoveryKind]string{
		domain.Advance:      "Advance",
		domain.FeedCredit:   "Feed on credit",
		domain.SocietyDues:  "Society dues",
		domain.Loan:         "Loan",
		domain.OtherRecover: "Other",
	}[d.Kind]
	if name == "" {
		name = string(d.Kind)
	}
	if d.Reference != "" {
		name += " (" + d.Reference + ")"
	}
	return name
}

// orderedQuantities returns the period's quantities in a fixed order.
//
// Ranging a map directly would put litres above kilograms on one printing and
// below on the next, and two statements of the same fortnight that differ are
// two statements somebody has to explain.
func orderedQuantities(q map[string]string) [][2]string {
	order := []string{"PER_LITRE", "PER_KG"}
	var out [][2]string
	seen := map[string]bool{}
	for _, k := range order {
		if v, ok := q[k]; ok {
			out = append(out, [2]string{k, v})
			seen[k] = true
		}
	}
	var rest []string
	for k := range q {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	// Anything unexpected still appears, in a fixed order rather than none.
	for i := 0; i < len(rest); i++ {
		for j := i + 1; j < len(rest); j++ {
			if rest[j] < rest[i] {
				rest[i], rest[j] = rest[j], rest[i]
			}
		}
		out = append(out, [2]string{rest[i], q[rest[i]]})
	}
	return out
}

func formatDate(t time.Time) string { return t.UTC().Format("02 Jan 2006") }

// displayWidth estimates how many columns a string occupies.
//
// Runes rather than bytes, with non-spacing and enclosing marks skipped. A
// Telugu or Devanagari name counted in bytes is around three times its width
// and pushes every column after it out of line, which is the commonest way a
// statement in an Indian language comes out unreadable.
//
// Only Mn and Me are skipped, not every combining mark. Spacing marks (Mc) are
// combining and do take a column — the Devanagari vowel sign I renders to the
// left of its consonant and occupies its own advance — so treating all marks as
// zero-width would be as wrong in the other direction as counting bytes.
//
// It is an estimate and not a measurement, and it is worth being exact about
// where it is wrong. Indic scripts form conjuncts: a virama joins two
// consonants into one glyph, so "కృష్ణ" — a base, a spacing mark, a base, a
// virama, a base — counts four here and renders in about two. Full-width
// characters occupy two columns and count as one. Getting either right needs a
// shaping engine, not a width table.
//
// The layout is arranged so that this does not matter to the money. Free text
// whose width cannot be known — the member reference, the society's name, a
// deduction's reference — appears only in the heading and the label half of a
// line, never in a column a figure sits in, and the figures are placed against
// the right margin rather than relative to the text on their left. So a name
// this function measures badly makes the heading a little ragged and cannot
// move a decimal point. TestAMemberReferenceCannotMoveTheMoneyColumns is what
// holds that property in place.
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) {
			continue
		}
		w++
	}
	return w
}

func padRight(s string, width int) string {
	if pad := width - displayWidth(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

func padLeft(s string, width int) string {
	if pad := width - displayWidth(s); pad > 0 {
		return strings.Repeat(" ", pad) + s
	}
	return s
}

// truncate cuts a string to a display width, counting combining marks with the
// character they belong to so a cut never lands between the two.
func truncate(s string, width int) string {
	if displayWidth(s) <= width {
		return s
	}
	w := 0
	for i, r := range s {
		if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) {
			continue
		}
		if w == width {
			return s[:i]
		}
		w++
	}
	return s
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
