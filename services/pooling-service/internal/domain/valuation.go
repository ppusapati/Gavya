package domain

import (
	"errors"
	"fmt"
	"sort"

	"github.com/ppusapati/gavya/libs/integrity/money"
)

// The rounding mode every pool computation uses.
//
// Fixed rather than configurable: a pool's defining property is that the
// producers' payments sum to the pool's value exactly, and that invariant is
// only checkable if every step rounded the same way. A per-tenant rounding mode
// would make two pools incomparable for no benefit anyone has asked for.
const poolRounding = money.RoundHalfUp

// weightScale is the number of decimal places quantities are weighted at when
// sharing out the residual fund. Three places resolves a gram in a kilogram,
// which is finer than any dairy scale in the field.
const weightScale = 3

var (
	ErrEmptyPool       = errors.New("pool has no producer milk")
	ErrNoUtilisation   = errors.New("pool has no classified utilisation")
	ErrZeroQuantity    = errors.New("pool has no pooled quantity to share against")
	ErrCurrencyMismatch = errors.New("prices are quoted in more than one currency")
)

// ValuationInput is everything needed to value a pool, gathered by the service.
type ValuationInput struct {
	Currency        string
	Scale           int32
	Producers       []ProducerMilk
	Utilisations    []ClassifiedUtilisation
	ComponentPrices []ComponentPrice
}

// ValuationResult is the computed pool value together with every producer's
// share and the full rounding trail that produced them.
type ValuationResult struct {
	ClassifiedValue        money.Money
	ComponentValue         money.Money
	ProducerSettlementFund money.Money
	TotalQuantity          string
	BlendPrice             money.Rate
	Allocations            []Allocation
	RoundingTrail          []money.RoundingStep
}

// ComputeValuation values a pool and shares it out.
//
// The whole point of the exercise is one invariant: the producers' totals sum
// to the classified value exactly, with no residual left unexplained. Every
// precision-losing step is recorded, and the residual fund is split with
// remainder-exact allocation rather than by rounding each share independently,
// which would leak minor units.
func ComputeValuation(in ValuationInput) (*ValuationResult, error) {
	if len(in.Producers) == 0 {
		return nil, ErrEmptyPool
	}
	if len(in.Utilisations) == 0 {
		return nil, ErrNoUtilisation
	}

	trail := make([]money.RoundingStep, 0, len(in.Utilisations)+len(in.Producers)*2)

	classifiedValue, err := valueUtilisations(in, &trail)
	if err != nil {
		return nil, err
	}

	componentValues, componentValue, err := valueComponents(in, &trail)
	if err != nil {
		return nil, err
	}

	fund, err := money.Sub(classifiedValue, componentValue)
	if err != nil {
		return nil, fmt.Errorf("compute settlement fund: %w", err)
	}

	weights, totalWeight, err := producerWeights(in.Producers)
	if err != nil {
		return nil, err
	}
	if totalWeight == 0 {
		return nil, ErrZeroQuantity
	}

	// Allocate rather than pro-rate each share independently: rounding each
	// producer's slice on its own leaves a residual that belongs to nobody, and
	// over a year of pools that residual is real money.
	shares, err := money.Allocate(fund, weights)
	if err != nil {
		return nil, fmt.Errorf("share settlement fund: %w", err)
	}

	allocations := make([]Allocation, 0, len(in.Producers))
	for i, p := range in.Producers {
		total, err := money.Add(componentValues[i], shares[i])
		if err != nil {
			return nil, fmt.Errorf("total for producer %s: %w", p.ProducerRef, err)
		}
		allocations = append(allocations, Allocation{
			ProducerRef:    p.ProducerRef,
			ComponentValue: componentValues[i],
			FundShare:      shares[i],
			Total:          total,
			Weight:         weights[i],
		})
	}

	// The invariant, checked rather than assumed. If it ever fails, a defect in
	// the arithmetic has silently moved producers' money, and that must surface
	// here rather than in a payment run.
	paid, err := money.Sum(totals(allocations))
	if err != nil {
		return nil, err
	}
	if paid.Value != classifiedValue.Value {
		return nil, fmt.Errorf(
			"allocation does not conserve value: producers total %s but the pool is worth %s",
			paid, classifiedValue)
	}

	totalQuantity, err := sumQuantities(in.Producers, in.Currency)
	if err != nil {
		return nil, err
	}

	blend, err := blendPrice(classifiedValue, totalWeight)
	if err != nil {
		return nil, err
	}

	return &ValuationResult{
		ClassifiedValue:        classifiedValue,
		ComponentValue:         componentValue,
		ProducerSettlementFund: fund,
		TotalQuantity:          totalQuantity.String(),
		BlendPrice:             blend,
		Allocations:            allocations,
		RoundingTrail:          trail,
	}, nil
}

// valueUtilisations prices what the pool's milk was actually used for.
func valueUtilisations(in ValuationInput, trail *[]money.RoundingStep) (money.Money, error) {
	// Sorted so the rounding trail is identical between runs; an unordered walk
	// would produce a different trail for the same pool.
	sorted := append([]ClassifiedUtilisation(nil), in.Utilisations...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Class < sorted[j].Class })

	total := money.Zero(in.Scale, in.Currency)
	for _, u := range sorted {
		if !ValidClass(u.Class) {
			return money.Money{}, fmt.Errorf("utilisation class %q is not recognised", u.Class)
		}
		// Quantities carry three decimals, finer than the currency, so they are
		// parsed at their own scale and the product is narrowed to the currency
		// once — rather than truncating the quantity up front and losing grams.
		qty, err := money.Parse(u.Quantity, weightScale, in.Currency)
		if err != nil {
			return money.Money{}, fmt.Errorf("class %s quantity: %w", u.Class, err)
		}
		raw, mulStep, err := money.MulRate(qty, u.Price, poolRounding)
		if err != nil {
			return money.Money{}, fmt.Errorf("class %s value: %w", u.Class, err)
		}
		mulStep.Operation = "CLASS_VALUE:" + string(u.Class)
		*trail = append(*trail, mulStep)

		value, scaleStep, err := money.Rescale(raw, in.Scale, poolRounding)
		if err != nil {
			return money.Money{}, fmt.Errorf("class %s rescale: %w", u.Class, err)
		}
		scaleStep.Operation = "CLASS_RESCALE:" + string(u.Class)
		*trail = append(*trail, scaleStep)

		if total, err = money.Add(total, value); err != nil {
			return money.Money{}, fmt.Errorf("accumulate class %s: %w", u.Class, err)
		}
	}
	return total, nil
}

// valueComponents prices each producer's constituents at the uniform rates.
func valueComponents(in ValuationInput, trail *[]money.RoundingStep) ([]money.Money, money.Money, error) {
	prices := make(map[ComponentKind]money.Rate, len(in.ComponentPrices))
	for _, cp := range in.ComponentPrices {
		prices[cp.Component] = cp.Price
	}

	// Sorted so the trail does not depend on map iteration order.
	kinds := make([]ComponentKind, 0, len(prices))
	for k := range prices {
		kinds = append(kinds, k)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })

	perProducer := make([]money.Money, 0, len(in.Producers))
	running := money.Zero(in.Scale, in.Currency)

	for _, p := range in.Producers {
		producerValue := money.Zero(in.Scale, in.Currency)
		for _, kind := range kinds {
			raw, present := p.Components[kind]
			if !present || raw == "" {
				continue
			}
			qty, err := money.Parse(raw, weightScale, in.Currency)
			if err != nil {
				return nil, money.Money{}, fmt.Errorf("producer %s component %s: %w", p.ProducerRef, kind, err)
			}
			product, mulStep, err := money.MulRate(qty, prices[kind], poolRounding)
			if err != nil {
				return nil, money.Money{}, fmt.Errorf("producer %s component %s value: %w", p.ProducerRef, kind, err)
			}
			mulStep.Operation = fmt.Sprintf("COMPONENT_VALUE:%s:%s", p.ProducerRef, kind)
			*trail = append(*trail, mulStep)

			value, scaleStep, err := money.Rescale(product, in.Scale, poolRounding)
			if err != nil {
				return nil, money.Money{}, fmt.Errorf("producer %s component %s rescale: %w", p.ProducerRef, kind, err)
			}
			scaleStep.Operation = fmt.Sprintf("COMPONENT_RESCALE:%s:%s", p.ProducerRef, kind)
			*trail = append(*trail, scaleStep)

			if producerValue, err = money.Add(producerValue, value); err != nil {
				return nil, money.Money{}, err
			}
		}
		perProducer = append(perProducer, producerValue)

		var err error
		if running, err = money.Add(running, producerValue); err != nil {
			return nil, money.Money{}, err
		}
	}
	return perProducer, running, nil
}

// producerWeights converts each producer's pooled quantity into an integer
// weight. Integer weights are what make the split exactly reproducible.
func producerWeights(producers []ProducerMilk) ([]int64, int64, error) {
	weights := make([]int64, 0, len(producers))
	var total int64
	for _, p := range producers {
		q, err := money.Parse(p.Quantity, weightScale, "XXX")
		if err != nil {
			return nil, 0, fmt.Errorf("producer %s quantity: %w", p.ProducerRef, err)
		}
		if q.Value < 0 {
			return nil, 0, fmt.Errorf("producer %s pooled a negative quantity", p.ProducerRef)
		}
		weights = append(weights, q.Value)
		total += q.Value
	}
	return weights, total, nil
}

func sumQuantities(producers []ProducerMilk, currency string) (money.Money, error) {
	total := money.Zero(weightScale, currency)
	for _, p := range producers {
		q, err := money.Parse(p.Quantity, weightScale, currency)
		if err != nil {
			return money.Money{}, err
		}
		if total, err = money.Add(total, q); err != nil {
			return money.Money{}, err
		}
	}
	return total, nil
}

// blendPrice is the pool's realisation per unit, quoted at four decimals
// because producers compare cooperatives on the fourth place.
//
// Rescaling the value to weightScale+blendScale before dividing by a
// weightScale-scaled quantity leaves the quotient already expressed at
// blendScale, so the two scale factors cancel in one step instead of being
// applied and undone.
func blendPrice(classifiedValue money.Money, totalWeight int64) (money.Rate, error) {
	if totalWeight == 0 {
		return money.Rate{}, ErrZeroQuantity
	}
	const blendScale = 4

	widened, _, err := money.Rescale(classifiedValue, weightScale+blendScale, poolRounding)
	if err != nil {
		return money.Rate{}, fmt.Errorf("widen for blend price: %w", err)
	}
	per, _, err := money.Div(widened, totalWeight, poolRounding)
	if err != nil {
		return money.Rate{}, fmt.Errorf("blend price: %w", err)
	}
	return money.Rate{Numerator: per.Value, Scale: blendScale}, nil
}

func totals(allocations []Allocation) []money.Money {
	out := make([]money.Money, 0, len(allocations))
	for _, a := range allocations {
		out = append(out, a.Total)
	}
	return out
}
