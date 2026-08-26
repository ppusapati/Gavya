package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/money"
)

const inr = "INR"

// rupees is a helper for writing amounts the way a society writes them.
func rupees(t *testing.T, s string) money.Money {
	t.Helper()
	m, err := money.Parse(s, 2, inr)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return m
}

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// debt builds an outstanding recovery.
func debt(t *testing.T, id string, kind RecoveryKind, principal, recovered, instalment string, priority int32, opened time.Time) *Recovery {
	t.Helper()
	return &Recovery{
		ID: id, TenantID: "T", ProducerRef: "P1", Kind: kind,
		Principal: rupees(t, principal), Recovered: rupees(t, recovered),
		Instalment: rupees(t, instalment),
		Priority:   priority, Status: Outstanding, OpenedOn: opened,
	}
}

// A fortnight with nothing owed pays out in full.
func TestAProducerWithNoDebtsTakesHomeEverything(t *testing.T) {
	s, err := Settle(rupees(t, "4820.50"), nil, CapAtEarnings)
	if err != nil {
		t.Fatal(err)
	}
	if s.Net.String() != "4820.50" {
		t.Errorf("net %s, want 4820.50", s.Net)
	}
	if !s.Deducted.IsZero() {
		t.Errorf("deducted %s from a producer who owes nothing", s.Deducted)
	}
	if len(s.Taken) != 0 {
		t.Errorf("%d deductions appeared from nowhere", len(s.Taken))
	}
}

// The ordinary case: an advance being recovered by instalments, and dues.
func TestDebtsAreServedInPriorityOrder(t *testing.T) {
	debts := []*Recovery{
		// Deliberately out of order in the slice, so a function that served them
		// as given would serve dues before the advance.
		debt(t, "R_DUES", SocietyDues, "50.00", "0.00", "0.00", 2, day(2026, time.March, 1)),
		debt(t, "R_ADV", Advance, "5000.00", "1000.00", "1000.00", 1, day(2026, time.January, 10)),
	}
	s, err := Settle(rupees(t, "4820.50"), debts, CapAtEarnings)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Taken) != 2 {
		t.Fatalf("%d deductions, want 2", len(s.Taken))
	}
	if s.Taken[0].RecoveryID != "R_ADV" {
		t.Errorf("the first debt served was %s, want the priority-1 advance", s.Taken[0].RecoveryID)
	}
	if s.Taken[0].Amount.String() != "1000.00" {
		t.Errorf("the advance took %s, want its 1000.00 instalment", s.Taken[0].Amount)
	}
	if s.Taken[1].Amount.String() != "50.00" {
		t.Errorf("the dues took %s, want 50.00", s.Taken[1].Amount)
	}
	if s.Deducted.String() != "1050.00" {
		t.Errorf("deducted %s, want 1050.00", s.Deducted)
	}
	if s.Net.String() != "3770.50" {
		t.Errorf("net %s, want 3770.50", s.Net)
	}
}

// The defect this guards: an instalment of 1000 against an advance with only 200
// left on it takes 1000, and the producer has paid back 5800 on a 5000 loan.
//
// It is invisible in every total — the society's books balance, the payout is a
// plausible number — and the only person who notices is the producer, a year
// later, if they kept the slips.
func TestARecoveryNeverTakesMoreThanIsOwed(t *testing.T) {
	debts := []*Recovery{
		debt(t, "R_ADV", Advance, "5000.00", "4800.00", "1000.00", 1, day(2026, time.January, 10)),
	}
	s, err := Settle(rupees(t, "4820.50"), debts, CapAtEarnings)
	if err != nil {
		t.Fatal(err)
	}
	if s.Deducted.String() != "200.00" {
		t.Errorf("took %s against a debt with 200.00 left on it", s.Deducted)
	}
	if s.Net.String() != "4620.50" {
		t.Errorf("net %s, want 4620.50", s.Net)
	}
}

// A recovery already settled or waived takes nothing, however it is ordered.
func TestASettledOrWaivedRecoveryTakesNothing(t *testing.T) {
	settled := debt(t, "R_DONE", Advance, "5000.00", "0.00", "1000.00", 1, day(2026, time.January, 10))
	settled.Status = Settled
	waived := debt(t, "R_WAIVED", Loan, "3000.00", "0.00", "500.00", 1, day(2026, time.January, 10))
	waived.Status = Waived

	s, err := Settle(rupees(t, "4820.50"), []*Recovery{settled, waived}, CapAtEarnings)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Deducted.IsZero() {
		t.Errorf("deducted %s against recoveries that are no longer being recovered", s.Deducted)
	}
}

// A short fortnight under a policy that caps: the first debt is served, the
// second gets what is left, the third gets nothing, and the net is zero rather
// than negative.
func TestAShortFortnightServesInOrderAndStopsAtZero(t *testing.T) {
	debts := []*Recovery{
		debt(t, "R_1", Advance, "5000.00", "0.00", "1000.00", 1, day(2026, time.January, 10)),
		debt(t, "R_2", FeedCredit, "800.00", "0.00", "0.00", 2, day(2026, time.February, 1)),
		debt(t, "R_3", SocietyDues, "50.00", "0.00", "0.00", 3, day(2026, time.March, 1)),
	}
	s, err := Settle(rupees(t, "1200.00"), debts, CapAtEarnings)
	if err != nil {
		t.Fatal(err)
	}
	if s.Net.String() != "0.00" {
		t.Errorf("net %s, want 0.00: a capped policy must not hand a producer a bill", s.Net)
	}
	if s.Deducted.String() != "1200.00" {
		t.Errorf("deducted %s, want the whole 1200.00 of earnings", s.Deducted)
	}
	if len(s.Taken) != 2 {
		t.Fatalf("%d deductions, want 2 — the third debt had nothing left to take", len(s.Taken))
	}
	if got := s.Taken[1].Amount.String(); got != "200.00" {
		t.Errorf("the feed credit took %s, want the 200.00 that was left", got)
	}
	// 600 of feed credit and 50 of dues went unserved.
	if s.CarriedForward.String() != "650.00" {
		t.Errorf("carried forward %s, want 650.00", s.CarriedForward)
	}
	if got := s.Taken[1].Shortfall.String(); got != "600.00" {
		t.Errorf("the feed credit's shortfall reads %s, want 600.00", got)
	}
}

// The other policy, which is the reason the first one is not a default. The same
// fortnight, the same debts, and the producer owes money at the window.
func TestAPolicyThatAllowsNegativeHandsTheProducerABill(t *testing.T) {
	debts := []*Recovery{
		debt(t, "R_1", Advance, "5000.00", "0.00", "1000.00", 1, day(2026, time.January, 10)),
		debt(t, "R_2", FeedCredit, "800.00", "0.00", "0.00", 2, day(2026, time.February, 1)),
	}
	s, err := Settle(rupees(t, "1200.00"), debts, AllowNegative)
	if err != nil {
		t.Fatal(err)
	}
	if s.Net.String() != "-600.00" {
		t.Errorf("net %s, want -600.00", s.Net)
	}
	if s.Deducted.String() != "1800.00" {
		t.Errorf("deducted %s, want 1800.00", s.Deducted)
	}
	if !s.CarriedForward.IsZero() {
		t.Errorf("carried forward %s under a policy that carries nothing forward", s.CarriedForward)
	}
}

// Two debts at the same priority opened on the same day is a real tie: two
// advances entered together on the day a society signed a batch of them.
//
// Broken by id, so the same settlement run twice serves them in the same order.
// Without a total order the amounts each debt received would differ between runs
// against the same data, and reconciling two statements would be impossible.
func TestTheOrderIsTotalSoTheSameSettlementTwiceAgrees(t *testing.T) {
	opened := day(2026, time.January, 10)
	build := func(order []string) []*Recovery {
		var out []*Recovery
		for _, id := range order {
			out = append(out, debt(t, id, Advance, "900.00", "0.00", "0.00", 1, opened))
		}
		return out
	}

	forwards, err := Settle(rupees(t, "1000.00"), build([]string{"R_AAA", "R_BBB"}), CapAtEarnings)
	if err != nil {
		t.Fatal(err)
	}
	backwards, err := Settle(rupees(t, "1000.00"), build([]string{"R_BBB", "R_AAA"}), CapAtEarnings)
	if err != nil {
		t.Fatal(err)
	}
	if forwards.Taken[0].RecoveryID != backwards.Taken[0].RecoveryID {
		t.Errorf("the same two debts were served in different orders depending on how they "+
			"arrived: %s then %s, against %s then %s",
			forwards.Taken[0].RecoveryID, forwards.Taken[1].RecoveryID,
			backwards.Taken[0].RecoveryID, backwards.Taken[1].RecoveryID)
	}
	// And the amounts differ, which is what makes the order matter rather than
	// being a cosmetic detail: 900 to the first, 100 to the second.
	if forwards.Taken[0].Amount.String() != "900.00" || forwards.Taken[1].Amount.String() != "100.00" {
		t.Errorf("the tie-break did not change what each debt got: %s then %s",
			forwards.Taken[0].Amount, forwards.Taken[1].Amount)
	}
}

// A debt older by a day is served first at equal priority. This is the rule a
// society would state out loud, and it must be the rule the code follows rather
// than an accident of insertion order.
func TestAtEqualPriorityTheOlderDebtIsServedFirst(t *testing.T) {
	newer := debt(t, "R_AAA", Advance, "900.00", "0.00", "0.00", 1, day(2026, time.February, 1))
	older := debt(t, "R_ZZZ", Advance, "900.00", "0.00", "0.00", 1, day(2026, time.January, 1))
	// The newer one sorts first by id, so if the date were ignored it would win.
	s, err := Settle(rupees(t, "1000.00"), []*Recovery{newer, older}, CapAtEarnings)
	if err != nil {
		t.Fatal(err)
	}
	if s.Taken[0].RecoveryID != "R_ZZZ" {
		t.Errorf("served %s first; the older debt R_ZZZ should come first at equal priority",
			s.Taken[0].RecoveryID)
	}
}

// A cycle with no policy cannot settle. The point of the error is that it
// arrives here, at the arithmetic, rather than being resolved by a default that
// nobody chose.
func TestSettlementWithNoPolicyIsRefused(t *testing.T) {
	if _, err := Settle(rupees(t, "100.00"), nil, ""); err != ErrNoPolicy {
		t.Errorf("settling with no policy gave %v, want ErrNoPolicy", err)
	}
}

// A debt written to a different scale is not silently rescaled. 1000 at scale 0
// is a thousand rupees and at scale 2 is ten; guessing which was meant is how a
// producer loses ninety-nine per cent of a payout.
//
// The refusal has to name the recovery. A mismatch left to be caught by the
// subtraction further down reports "money: scale mismatch: 2 vs 0", which is
// true, arrives at the right time, and tells whoever is holding the pager
// nothing about which of a producer's four debts is the bad one.
func TestADebtWrittenToADifferentScaleIsRefusedByName(t *testing.T) {
	d := debt(t, "R_BADSCALE", Advance, "500.00", "0.00", "0.00", 1, day(2026, time.January, 1))
	whole, err := money.New(500, 0, inr)
	if err != nil {
		t.Fatal(err)
	}
	d.Principal, d.Recovered = whole, money.Zero(0, inr)

	_, err = Settle(rupees(t, "1000.00"), []*Recovery{d}, CapAtEarnings)
	if err == nil {
		t.Fatal("a debt at a different scale was settled against earnings at scale 2")
	}
	if !strings.Contains(err.Error(), "R_BADSCALE") {
		t.Errorf("the refusal does not say which recovery is at fault: %v", err)
	}
}

// A recovery in another currency is refused rather than added, and likewise says
// which one.
func TestADebtInAnotherCurrencyIsRefusedByName(t *testing.T) {
	d := debt(t, "R_BADCCY", Advance, "500.00", "0.00", "0.00", 1, day(2026, time.January, 1))
	usd, err := money.Parse("500.00", 2, "USD")
	if err != nil {
		t.Fatal(err)
	}
	d.Principal, d.Recovered = usd, money.Zero(2, "USD")

	_, err = Settle(rupees(t, "1000.00"), []*Recovery{d}, CapAtEarnings)
	if err == nil {
		t.Fatal("a debt in USD was recovered from earnings in INR")
	}
	if !strings.Contains(err.Error(), "R_BADCCY") {
		t.Errorf("the refusal does not say which recovery is at fault: %v", err)
	}
}

// An instalment written to a different scale from the principal it belongs to.
// The debt itself is consistent, so the checks on the outstanding balance pass
// and only the instalment is wrong — an instalment of 1000 at scale 0 against a
// principal at scale 2 would take a hundred times the intended amount.
func TestAnInstalmentAtTheWrongScaleIsRefusedByName(t *testing.T) {
	d := debt(t, "R_BADINST", Advance, "5000.00", "0.00", "0.00", 1, day(2026, time.January, 1))
	inst, err := money.New(1000, 0, inr)
	if err != nil {
		t.Fatal(err)
	}
	d.Instalment = inst

	_, err = Settle(rupees(t, "4000.00"), []*Recovery{d}, CapAtEarnings)
	if err == nil {
		t.Fatal("an instalment at scale 0 was taken against a principal at scale 2")
	}
	if !strings.Contains(err.Error(), "R_BADINST") || !strings.Contains(err.Error(), "instalment") {
		t.Errorf("the refusal does not point at the instalment of a named recovery: %v", err)
	}
}

// Every paisa of the gross is either paid out or recovered. Checked over a
// spread of awkward amounts rather than one, because the failure this guards
// against — a paisa lost between the two — appears at particular values and not
// at round ones.
func TestNothingIsLostBetweenTheNetAndTheDeductions(t *testing.T) {
	amounts := []string{"0.01", "0.99", "1.00", "1234.56", "9999.99", "4820.50"}
	for _, a := range amounts {
		debts := []*Recovery{
			debt(t, "R_1", Advance, "333.33", "0.00", "111.11", 1, day(2026, time.January, 1)),
			debt(t, "R_2", SocietyDues, "7.77", "0.00", "0.00", 2, day(2026, time.January, 2)),
		}
		s, err := Settle(rupees(t, a), debts, CapAtEarnings)
		if err != nil {
			t.Fatalf("%s: %v", a, err)
		}
		sum, err := money.Add(s.Net, s.Deducted)
		if err != nil {
			t.Fatalf("%s: %v", a, err)
		}
		if sum.String() != a {
			t.Errorf("earnings of %s came to %s net plus %s deducted, which is %s",
				a, s.Net, s.Deducted, sum)
		}
	}
}

// A deduction of zero is not written. A statement line saying a producer was
// charged nothing for something is noise on the one document they actually read.
func TestARecoveryThatTakesNothingLeavesNoLine(t *testing.T) {
	debts := []*Recovery{
		debt(t, "R_1", Advance, "5000.00", "0.00", "1000.00", 1, day(2026, time.January, 1)),
		debt(t, "R_2", SocietyDues, "50.00", "0.00", "0.00", 2, day(2026, time.January, 2)),
	}
	s, err := Settle(rupees(t, "1000.00"), debts, CapAtEarnings)
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range s.Taken {
		if tk.Amount.IsZero() {
			t.Errorf("recovery %s produced a deduction line for zero", tk.RecoveryID)
		}
	}
	if len(s.Taken) != 1 {
		t.Errorf("%d deduction lines, want 1", len(s.Taken))
	}
}

// A recovery fully recovered but not yet marked settled takes nothing more.
func TestAFullyRecoveredDebtTakesNothingFurther(t *testing.T) {
	d := debt(t, "R_1", Advance, "5000.00", "5000.00", "1000.00", 1, day(2026, time.January, 1))
	s, err := Settle(rupees(t, "4000.00"), []*Recovery{d}, CapAtEarnings)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Deducted.IsZero() {
		t.Errorf("took a further %s against a debt already paid in full", s.Deducted)
	}
}
