package domain

import (
	"fmt"
	"sort"

	"github.com/ppusapati/gavya/libs/integrity/money"
)

// Settlement is what one producer's period came to, and how.
type Settlement struct {
	Gross    money.Money
	Deducted money.Money
	Net      money.Money

	// CarriedForward is what a policy that caps at earnings could not take.
	CarriedForward money.Money

	// Taken is the amount recovered against each recovery, in the order they
	// were served. A recovery that got nothing is not in this list: a deduction
	// row for zero is a line on a producer's statement saying nothing happened,
	// which is worse than the absence.
	Taken []Taken
}

// Taken is one recovery served from one period's earnings.
type Taken struct {
	RecoveryID string
	Kind       RecoveryKind
	Reference  string
	Amount     money.Money
	// Shortfall is what this recovery wanted and did not get. Kept so a
	// statement can say why a debt did not go down as much as the producer
	// expected, rather than leaving them to work it out from two balances.
	Shortfall money.Money
}

// Settle works out what a producer takes home.
//
// The rules, in the order they matter:
//
//  1. Debts are served in priority order, oldest first within a priority, and by
//     id within a day. Every tie is broken by something recorded rather than by
//     the order rows happened to come back in — otherwise which of a producer's
//     debts a short fortnight paid would depend on the query plan, and would
//     change between two runs of the same settlement.
//
//  2. A recovery takes the smaller of its instalment and what is still
//     outstanding. An instalment of zero means there is no instalment, and the
//     whole outstanding balance is wanted.
//
//  3. Under CAP_AT_EARNINGS a recovery takes no more than the earnings left.
//     Under ALLOW_NEGATIVE it takes what it wanted and the net goes below zero.
//
//  4. Nothing ever takes more than is outstanding. This is checked here, and
//     again by a CHECK constraint on the table, because over-recovery is money
//     taken from a producer that they do not owe and it is invisible in every
//     total it appears in.
func Settle(gross money.Money, debts []*Recovery, policy DeductionPolicy) (*Settlement, error) {
	if !ValidPolicy(policy) {
		return nil, ErrNoPolicy
	}
	if gross.Value < 0 {
		return nil, fmt.Errorf("a producer cannot have earned a negative amount of milk money")
	}

	zero := money.Zero(gross.Scale, gross.Currency)
	s := &Settlement{Gross: gross, Deducted: zero, Net: gross, CarriedForward: zero}

	ordered := make([]*Recovery, 0, len(debts))
	for _, d := range debts {
		if d.Status != Outstanding {
			continue
		}
		ordered = append(ordered, d)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a.Priority != b.Priority {
			return a.Priority < b.Priority
		}
		if !a.OpenedOn.Equal(b.OpenedOn) {
			return a.OpenedOn.Before(b.OpenedOn)
		}
		return a.ID < b.ID
	})

	remaining := gross
	for _, d := range ordered {
		outstanding, err := d.OutstandingAmount()
		if err != nil {
			return nil, fmt.Errorf("recovery %s: %w", d.ID, err)
		}
		if outstanding.Value <= 0 {
			continue
		}
		if err := sameMoney(gross, outstanding); err != nil {
			return nil, fmt.Errorf("recovery %s: %w", d.ID, err)
		}

		want := outstanding
		if d.Instalment.Value > 0 {
			if err := sameMoney(gross, d.Instalment); err != nil {
				return nil, fmt.Errorf("recovery %s instalment: %w", d.ID, err)
			}
			if d.Instalment.Value < want.Value {
				want = d.Instalment
			}
		}

		take := want
		if policy == CapAtEarnings {
			if remaining.Value <= 0 {
				take = money.Zero(gross.Scale, gross.Currency)
			} else if remaining.Value < take.Value {
				take = remaining
			}
		}

		shortfall, err := money.Sub(want, take)
		if err != nil {
			return nil, err
		}
		if shortfall.Value > 0 {
			if s.CarriedForward, err = money.Add(s.CarriedForward, shortfall); err != nil {
				return nil, err
			}
		}
		if take.Value == 0 {
			continue
		}

		if remaining, err = money.Sub(remaining, take); err != nil {
			return nil, err
		}
		if s.Deducted, err = money.Add(s.Deducted, take); err != nil {
			return nil, err
		}
		s.Taken = append(s.Taken, Taken{
			RecoveryID: d.ID, Kind: d.Kind, Reference: d.Reference,
			Amount: take, Shortfall: shortfall,
		})
	}

	net, err := money.Sub(gross, s.Deducted)
	if err != nil {
		return nil, err
	}
	s.Net = net

	// The invariant worth asserting: the deduction lines add up to the deducted
	// total.
	//
	// Checking that the net plus the deductions come to the gross would look
	// like the safer assertion and would be worth nothing — the net is computed
	// as the gross less the deductions one line above, so that identity holds by
	// construction and cannot fail however wrong the rest of this is.
	//
	// This one can fail. A running total advanced without its line appended, or
	// a line appended for an amount other than the one taken, gives a producer a
	// statement whose deductions do not come to the figure subtracted from their
	// milk — and that is the document they bring back to the society to argue
	// about.
	var lines int64
	for _, tk := range s.Taken {
		lines += tk.Amount.Value
	}
	if lines != s.Deducted.Value {
		return nil, fmt.Errorf("the deduction lines come to %d but %s was taken from the "+
			"earnings; a statement built from this would not reconcile", lines, s.Deducted)
	}
	if policy == CapAtEarnings && s.Net.Value < 0 {
		return nil, fmt.Errorf("a cycle that caps at earnings produced a negative payable of %s", s.Net)
	}
	return s, nil
}

func sameMoney(a, b money.Money) error {
	if a.Currency != b.Currency {
		return fmt.Errorf("%s and %s are different currencies", a.Currency, b.Currency)
	}
	if a.Scale != b.Scale {
		return fmt.Errorf("%s is written to %d decimal places and %s to %d; comparing them "+
			"would be comparing rupees with paise", a, a.Scale, b, b.Scale)
	}
	return nil
}
