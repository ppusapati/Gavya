package domain

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ppusapati/gavya/libs/integrity/money"
)

func rate(t *testing.T, s string, scale int32) money.Rate {
	t.Helper()
	r, err := money.ParseRate(s, scale)
	if err != nil {
		t.Fatalf("ParseRate(%q): %v", s, err)
	}
	return r
}

func producer(ref, quantity, fat, snf string) ProducerMilk {
	return ProducerMilk{
		ProducerRef: ref,
		Quantity:    quantity,
		Components: map[ComponentKind]string{
			ComponentFat: fat,
			ComponentSNF: snf,
		},
	}
}

// input builds a pool where three producers deliver 1000 litres between them
// and the milk is used across two classes.
func input(t *testing.T) ValuationInput {
	t.Helper()
	return ValuationInput{
		Currency: "INR",
		Scale:    2,
		Producers: []ProducerMilk{
			producer("prod-1", "400.000", "16.000", "34.000"),
			producer("prod-2", "350.000", "14.700", "29.750"),
			producer("prod-3", "250.000", "10.250", "21.250"),
		},
		Utilisations: []ClassifiedUtilisation{
			{Class: ClassI, Quantity: "600.000", Price: rate(t, "42.0000", 4)},
			{Class: ClassIII, Quantity: "400.000", Price: rate(t, "36.5000", 4)},
		},
		ComponentPrices: []ComponentPrice{
			{Component: ComponentFat, Price: rate(t, "300.0000", 4)},
			{Component: ComponentSNF, Price: rate(t, "180.0000", 4)},
		},
	}
}

// The invariant the whole design rests on: producers' totals sum to the pool's
// value exactly, with nothing left over.
func TestProducerTotalsConserveThePoolValue(t *testing.T) {
	res, err := ComputeValuation(input(t))
	if err != nil {
		t.Fatalf("ComputeValuation: %v", err)
	}

	totals := make([]money.Money, 0, len(res.Allocations))
	for _, a := range res.Allocations {
		totals = append(totals, a.Total)
	}
	sum, err := money.Sum(totals)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Value != res.ClassifiedValue.Value {
		t.Fatalf("producers total %s but the pool is worth %s", sum, res.ClassifiedValue)
	}
}

func TestClassifiedValueIsTheSumOfClassValues(t *testing.T) {
	res, err := ComputeValuation(input(t))
	if err != nil {
		t.Fatalf("ComputeValuation: %v", err)
	}
	// 600.000 * 42.0000 = 25200.00, 400.000 * 36.5000 = 14600.00
	if res.ClassifiedValue.String() != "39800.00" {
		t.Errorf("classified value = %s, want 39800.00", res.ClassifiedValue)
	}
}

func TestSettlementFundIsTheResidual(t *testing.T) {
	res, err := ComputeValuation(input(t))
	if err != nil {
		t.Fatalf("ComputeValuation: %v", err)
	}
	expected, err := money.Sub(res.ClassifiedValue, res.ComponentValue)
	if err != nil {
		t.Fatal(err)
	}
	if res.ProducerSettlementFund.Value != expected.Value {
		t.Errorf("fund = %s, want %s", res.ProducerSettlementFund, expected)
	}
}

// Class prices below component prices leave a negative residual. Producers then
// bear the shortfall, which is a real outcome rather than an error.
func TestNegativeFundIsAValidOutcomeAndStillConserves(t *testing.T) {
	in := input(t)
	in.Utilisations = []ClassifiedUtilisation{
		{Class: ClassIV, Quantity: "1000.000", Price: rate(t, "10.0000", 4)},
	}

	res, err := ComputeValuation(in)
	if err != nil {
		t.Fatalf("a negative residual was rejected: %v", err)
	}
	if res.ProducerSettlementFund.Value >= 0 {
		t.Fatalf("fund = %s, want negative for this fixture", res.ProducerSettlementFund)
	}

	totals := make([]money.Money, 0, len(res.Allocations))
	for _, a := range res.Allocations {
		totals = append(totals, a.Total)
	}
	sum, err := money.Sum(totals)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Value != res.ClassifiedValue.Value {
		t.Errorf("a negative fund broke conservation: %s vs %s", sum, res.ClassifiedValue)
	}
}

// A fund that does not divide evenly must still be fully distributed; the odd
// minor units belong to producers, not to a rounding residual.
func TestIndivisibleFundIsFullyDistributed(t *testing.T) {
	in := input(t)
	// Three equal producers and a fund that will not split into three.
	in.Producers = []ProducerMilk{
		producer("prod-1", "100.000", "4.000", "8.500"),
		producer("prod-2", "100.000", "4.000", "8.500"),
		producer("prod-3", "100.000", "4.000", "8.500"),
	}
	in.Utilisations = []ClassifiedUtilisation{
		{Class: ClassI, Quantity: "300.000", Price: rate(t, "33.3333", 4)},
	}

	res, err := ComputeValuation(in)
	if err != nil {
		t.Fatalf("ComputeValuation: %v", err)
	}

	shares := make([]money.Money, 0, len(res.Allocations))
	for _, a := range res.Allocations {
		shares = append(shares, a.FundShare)
	}
	sum, err := money.Sum(shares)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Value != res.ProducerSettlementFund.Value {
		t.Fatalf("shares total %s but the fund is %s; minor units went missing", sum, res.ProducerSettlementFund)
	}
}

func TestSharesFollowContribution(t *testing.T) {
	res, err := ComputeValuation(input(t))
	if err != nil {
		t.Fatalf("ComputeValuation: %v", err)
	}

	byRef := map[string]Allocation{}
	for _, a := range res.Allocations {
		byRef[a.ProducerRef] = a
	}
	// prod-1 pooled 400 litres, prod-3 pooled 250, so prod-1's share is larger.
	if byRef["prod-1"].FundShare.Value <= byRef["prod-3"].FundShare.Value {
		t.Errorf("the larger contributor did not receive the larger share: %s vs %s",
			byRef["prod-1"].FundShare, byRef["prod-3"].FundShare)
	}
	if byRef["prod-1"].Weight != 400000 {
		t.Errorf("prod-1 weight = %d, want 400000 at three decimal places", byRef["prod-1"].Weight)
	}
}

func TestBlendPriceIsValuePerUnit(t *testing.T) {
	res, err := ComputeValuation(input(t))
	if err != nil {
		t.Fatalf("ComputeValuation: %v", err)
	}
	// 39800.00 over 1000 litres is 39.80 per litre.
	if res.BlendPrice.String() != "39.8000" {
		t.Errorf("blend price = %s, want 39.8000", res.BlendPrice)
	}
	if res.TotalQuantity != "1000.000" {
		t.Errorf("total quantity = %s, want 1000.000", res.TotalQuantity)
	}
}

// Quantities carry three decimals; truncating them to the currency's two would
// silently discard grams on every line.
func TestQuantityPrecisionIsNotLostToTheCurrencyScale(t *testing.T) {
	in := input(t)
	in.Producers = []ProducerMilk{producer("prod-1", "1.111", "0.045", "0.094")}
	in.Utilisations = []ClassifiedUtilisation{
		{Class: ClassI, Quantity: "1.111", Price: rate(t, "1000.0000", 4)},
	}
	in.ComponentPrices = nil

	res, err := ComputeValuation(in)
	if err != nil {
		t.Fatalf("ComputeValuation: %v", err)
	}
	// 1.111 * 1000 = 1111.00 exactly. Truncating the quantity to 1.11 would
	// give 1110.00.
	if res.ClassifiedValue.String() != "1111.00" {
		t.Errorf("classified value = %s, want 1111.00", res.ClassifiedValue)
	}
}

func TestEveryRoundingStepIsRecorded(t *testing.T) {
	res, err := ComputeValuation(input(t))
	if err != nil {
		t.Fatalf("ComputeValuation: %v", err)
	}
	if len(res.RoundingTrail) == 0 {
		t.Fatal("no rounding trail was produced; the total could not be reproduced by hand")
	}
	for i, step := range res.RoundingTrail {
		if step.Operation == "" {
			t.Errorf("step %d carries no operation label", i)
		}
		if step.Mode != poolRounding {
			t.Errorf("step %d used mode %s, want %s", i, step.Mode, poolRounding)
		}
	}
}

func TestComputationIsDeterministic(t *testing.T) {
	first, err := ComputeValuation(input(t))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		again, err := ComputeValuation(input(t))
		if err != nil {
			t.Fatal(err)
		}
		if again.ClassifiedValue.Value != first.ClassifiedValue.Value {
			t.Fatalf("run %d produced a different pool value", i)
		}
		if len(again.RoundingTrail) != len(first.RoundingTrail) {
			t.Fatalf("run %d produced a different rounding trail length", i)
		}
		for j := range first.RoundingTrail {
			if again.RoundingTrail[j].Operation != first.RoundingTrail[j].Operation {
				t.Fatalf("run %d reordered the rounding trail at step %d", i, j)
			}
		}
		for j := range first.Allocations {
			if again.Allocations[j].Total.Value != first.Allocations[j].Total.Value {
				t.Fatalf("run %d paid producer %s differently", i, first.Allocations[j].ProducerRef)
			}
		}
	}
}

func TestConservationHoldsAcrossManyAwkwardPools(t *testing.T) {
	// Prices and quantities chosen to force rounding at every step.
	for n := 1; n <= 40; n++ {
		in := input(t)
		in.Producers = nil
		for i := 0; i < n; i++ {
			in.Producers = append(in.Producers, producer(
				fmt.Sprintf("prod-%d", i),
				fmt.Sprintf("%d.%03d", 10+i, (i*37)%1000),
				fmt.Sprintf("0.%03d", (i*13)%1000),
				fmt.Sprintf("1.%03d", (i*29)%1000),
			))
		}
		in.Utilisations = []ClassifiedUtilisation{
			{Class: ClassI, Quantity: "333.333", Price: rate(t, "41.3717", 4)},
			{Class: ClassII, Quantity: "111.111", Price: rate(t, "38.9991", 4)},
			{Class: ClassIV, Quantity: "77.777", Price: rate(t, "29.6663", 4)},
		}

		res, err := ComputeValuation(in)
		if err != nil {
			t.Fatalf("%d producers: %v", n, err)
		}

		totals := make([]money.Money, 0, len(res.Allocations))
		for _, a := range res.Allocations {
			totals = append(totals, a.Total)
		}
		sum, err := money.Sum(totals)
		if err != nil {
			t.Fatal(err)
		}
		if sum.Value != res.ClassifiedValue.Value {
			t.Fatalf("%d producers: totals %s but pool is %s (off by %d minor units)",
				n, sum, res.ClassifiedValue, sum.Value-res.ClassifiedValue.Value)
		}
	}
}

func TestEmptyPoolIsRejected(t *testing.T) {
	in := input(t)
	in.Producers = nil
	if _, err := ComputeValuation(in); !errors.Is(err, ErrEmptyPool) {
		t.Errorf("got %v, want ErrEmptyPool", err)
	}

	in = input(t)
	in.Utilisations = nil
	if _, err := ComputeValuation(in); !errors.Is(err, ErrNoUtilisation) {
		t.Errorf("got %v, want ErrNoUtilisation", err)
	}
}

func TestZeroPooledQuantityIsRejected(t *testing.T) {
	in := input(t)
	in.Producers = []ProducerMilk{producer("prod-1", "0.000", "0.000", "0.000")}
	if _, err := ComputeValuation(in); !errors.Is(err, ErrZeroQuantity) {
		t.Errorf("got %v, want ErrZeroQuantity", err)
	}
}

func TestNegativeContributionIsRejected(t *testing.T) {
	in := input(t)
	in.Producers = []ProducerMilk{producer("prod-1", "-5.000", "0.200", "0.425")}
	if _, err := ComputeValuation(in); err == nil {
		t.Error("a negative pooled quantity was accepted")
	}
}

func TestUnknownClassIsRejected(t *testing.T) {
	in := input(t)
	in.Utilisations = []ClassifiedUtilisation{
		{Class: "CLASS_IX", Quantity: "100.000", Price: rate(t, "40.0000", 4)},
	}
	if _, err := ComputeValuation(in); err == nil {
		t.Error("an unrecognised utilisation class was accepted")
	}
}

func TestSingleProducerTakesTheWholeFund(t *testing.T) {
	in := input(t)
	in.Producers = []ProducerMilk{producer("prod-1", "1000.000", "40.000", "85.000")}

	res, err := ComputeValuation(in)
	if err != nil {
		t.Fatalf("ComputeValuation: %v", err)
	}
	if len(res.Allocations) != 1 {
		t.Fatalf("got %d allocations, want 1", len(res.Allocations))
	}
	if res.Allocations[0].FundShare.Value != res.ProducerSettlementFund.Value {
		t.Errorf("share %s does not equal the fund %s",
			res.Allocations[0].FundShare, res.ProducerSettlementFund)
	}
	if res.Allocations[0].Total.Value != res.ClassifiedValue.Value {
		t.Errorf("the sole producer received %s of a %s pool",
			res.Allocations[0].Total, res.ClassifiedValue)
	}
}
