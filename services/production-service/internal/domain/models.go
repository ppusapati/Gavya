// Package domain holds what a batch is and what a recall asks of it.
//
// Two questions, and they are the same question read in opposite directions.
// Forward: this tanker was contaminated, which cartons contain it. Backward:
// this carton came back, what went into it and what else came out of the same
// silo.
//
// The answer to either has to be complete or say plainly that it is not. A
// partial list of affected cartons is worse than no list, because it gets acted
// on: the ones named are pulled off the shelf and the ones missed stay there
// with somebody's confidence behind them. So every walk in this package reports
// whether it finished, and a walk that stopped early names the batches it had
// not yet followed — which is the difference between an incomplete answer and a
// wrong one.
package domain

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/money"
	"github.com/ppusapati/gavya/libs/integrity/quantity"
)

// Kind is where a batch sits between the plant gate and the shelf.
type Kind string

const (
	// Raw is milk as it arrived. It is a batch like any other, and that is
	// deliberate: if raw milk were modelled as something else the genealogy
	// would stop at the plant gate, which is exactly where a recall needs to
	// keep going.
	Raw Kind = "RAW"
	// Intermediate is cream, skim, curd, a silo's contents — anything made from
	// batches and consumed by batches.
	Intermediate Kind = "INTERMEDIATE"
	// Finished is what leaves. The end of the forward walk, and the thing a
	// recall is actually looking for.
	Finished Kind = "FINISHED"
)

func ValidKind(k Kind) bool {
	switch k {
	case Raw, Intermediate, Finished:
		return true
	}
	return false
}

// SourceKind is what a raw batch was drawn from.
//
// Polymorphic, like the laboratory's sample source and for the same reason: a
// movement lives in material-service and a node lives beside it, and no foreign
// key from here names either.
type SourceKind string

const (
	FromMovement SourceKind = "MOVEMENT"
	FromNode     SourceKind = "NODE"
)

func ValidSourceKind(k SourceKind) bool { return k == FromMovement || k == FromNode }

// Status is what may be done with a batch.
type Status string

const (
	Open        Status = "OPEN"
	Released    Status = "RELEASED"
	Quarantined Status = "QUARANTINED"
	Recalled    Status = "RECALLED"
	Disposed    Status = "DISPOSED"
)

func ValidStatus(s Status) bool {
	switch s {
	case Open, Released, Quarantined, Recalled, Disposed:
		return true
	}
	return false
}

// Held says whether the batch is stopped: it may not be fed into anything new.
//
// The database enforces this too, on the consuming end. Both, because the
// database is what makes it true and this is what lets a service say why before
// the transaction fails.
func (s Status) Held() bool { return s == Quarantined || s == Recalled || s == Disposed }

// NeedsReason says whether the status is one nobody can act on without knowing
// why it was set. A recall handed to a distributor with no reason on it cannot
// be explained to the person who receives the phone call.
func (s Status) NeedsReason() bool { return s.Held() }

// Batch is a quantity of one thing, made at one time, that can be pointed at.
type Batch struct {
	ID       string
	TenantID string

	// Code is what is painted on the vessel or printed on the carton. A person
	// reads this off a label and types it in; it is not the identifier.
	Code string

	Kind Kind
	// ProductRef is what it is, in the plant's own words: RAW_MILK, CREAM,
	// PANEER, GHEE, SMP. Free text, because a closed list here would be this
	// platform deciding what a dairy is allowed to make.
	ProductRef string

	Produced   quantity.Quantity
	ProducedAt time.Time
	ProducedBy string

	// Where a raw batch came from. Empty on everything else, which says where
	// it came from through its inputs instead.
	SourceKind SourceKind
	SourceRef  string

	// ExpectedYieldPPM is what the plant expected this process to yield, in
	// parts per million of what it consumed. Nil where none was declared, and
	// nil is the ordinary case: this platform holds no table of standard
	// yields, because a society's paneer yield is a property of its milk, its
	// process and its equipment, and a figure invented here would be a number
	// nobody measured sitting in a variance report somebody is asked to explain.
	ExpectedYieldPPM *int64

	Status       Status
	StatusReason string

	CreatedAt time.Time
	UpdatedAt time.Time
	CreatedBy string
	UpdatedBy string
	DeletedAt *time.Time
}

// Input is one lot that went into one batch, and how much of it.
type Input struct {
	ID       string
	TenantID string

	OutputBatchID string
	InputBatchID  string

	Consumed quantity.Quantity

	CreatedAt time.Time
	CreatedBy string
}

var (
	ErrNoCode          = errors.New("a batch must carry the code written on the vessel; that is what a person reads off a label at two in the morning")
	ErrNoKind          = errors.New("a batch must say whether it is RAW, INTERMEDIATE or FINISHED")
	ErrNoProduct       = errors.New("a batch must say what it is")
	ErrRawNeedsSource  = errors.New("a raw batch must say what it was drawn from — a movement or a node — because that is where the genealogy continues past the plant gate")
	ErrSourceOnMade    = errors.New("only a raw batch names an outside source; anything else says where it came from through its inputs")
	ErrHoldNeedsReason = errors.New("a batch that is quarantined, recalled or disposed of must say why; a hold nobody can explain is one that gets lifted by whoever is on shift")
	ErrNotPositive     = errors.New("a batch of nothing is not a batch")
	ErrConsumeSelf     = errors.New("a batch cannot be an input to itself")
	ErrHeldInput       = errors.New("a batch under hold cannot be consumed; a hold that does not stop the lot moving is not a hold")
)

func (b *Batch) Validate() error {
	switch {
	case b.TenantID == "":
		return errors.New("tenant_id is required")
	case b.Code == "":
		return ErrNoCode
	case !ValidKind(b.Kind):
		return ErrNoKind
	case b.ProductRef == "":
		return ErrNoProduct
	case !quantity.ValidUnit(b.Produced.Unit):
		return quantity.ErrNoUnit
	case b.Produced.Value() <= 0:
		return ErrNotPositive
	case b.ProducedAt.IsZero():
		return errors.New("a batch must say when it was made")
	case b.ProducedBy == "":
		return errors.New("a batch must say who made it")
	case b.Kind == Raw && (!ValidSourceKind(b.SourceKind) || b.SourceRef == ""):
		return ErrRawNeedsSource
	case b.Kind != Raw && (b.SourceKind != "" || b.SourceRef != ""):
		return ErrSourceOnMade
	case !ValidStatus(b.Status):
		return errors.New("a batch must say what may be done with it")
	case b.Status.NeedsReason() && b.StatusReason == "":
		return ErrHoldNeedsReason
	case b.ExpectedYieldPPM != nil && *b.ExpectedYieldPPM <= 0:
		return errors.New("a declared yield of nothing is not an expectation")
	case b.CreatedBy == "":
		return errors.New("actor is required")
	}
	return nil
}

func (i *Input) Validate() error {
	switch {
	case i.TenantID == "":
		return errors.New("tenant_id is required")
	case i.OutputBatchID == "" || i.InputBatchID == "":
		return errors.New("an input line names the batch it went into and the batch it came from")
	case i.OutputBatchID == i.InputBatchID:
		return ErrConsumeSelf
	case !quantity.ValidUnit(i.Consumed.Unit):
		return quantity.ErrNoUnit
	case i.Consumed.Value() <= 0:
		return errors.New("consuming nothing is not consumption")
	case i.CreatedBy == "":
		return errors.New("actor is required")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Walking the genealogy
// ---------------------------------------------------------------------------

// Direction is which way a walk follows an input line.
type Direction string

const (
	// Forward follows a batch into what was made from it. This is the recall
	// question: the tanker was bad, where did it end up.
	Forward Direction = "FORWARD"
	// Backward follows a batch into what it was made from. This is the
	// complaint question: the carton came back, what went into it.
	Backward Direction = "BACKWARD"
)

func ValidDirection(d Direction) bool { return d == Forward || d == Backward }

// Edge is one input line, reduced to the two ends a walk cares about.
type Edge struct {
	OutputBatchID string
	InputBatchID  string
	Consumed      quantity.Quantity
}

// Neighbours loads every input line touching the given batches on the given
// side. One call per level of the walk, rather than one per batch: a silo that
// took forty tankers is one query, not forty.
//
// It is a function rather than a repository interface so the walk can be tested
// against a graph written down in a test file, which is where the shapes that
// break traversals — diamonds, deep chains, disconnected islands — are easy to
// state and hard to get wrong.
type Neighbours func(ids []string) ([]Edge, error)

// Limit is where a walk gives up.
//
// Both bounds are required and there is no default, because a default here is a
// silent decision about how much of a recall gets answered. A caller that wants
// the whole plant says so with a number large enough to cover it, and finds out
// from Trace.Complete whether it was.
type Limit struct {
	// MaxDepth is how many process steps out the walk goes.
	MaxDepth int
	// MaxBatches is how many batches it will name.
	MaxBatches int
}

var ErrNoLimit = errors.New("a walk must be given a depth and a batch bound; leaving them unset would mean the platform choosing how much of a recall to answer")

// Reached is one batch the walk arrived at.
type Reached struct {
	BatchID string
	// Depth is the fewest process steps from the origin. A batch reachable by
	// two routes — the ordinary case, a silo feeding two lines that meet again
	// — appears once, at the shorter one.
	Depth int
	// Via is a batch one step nearer the origin. One of possibly several: it is
	// enough to explain the connection, not to enumerate every route.
	Via string
}

// Trace is what a walk found.
type Trace struct {
	Origin    string
	Direction Direction
	Reached   []Reached

	// Complete is false when the walk stopped at a bound.
	//
	// This field is the reason the package exists. A traversal that gave up at
	// ten hops and returned what it had would look exactly like one that
	// finished, and the list it returned would be acted on.
	Complete bool
	// Frontier is what had not been followed when the walk stopped. Empty on a
	// complete walk. Somebody continuing the recall by hand starts here.
	Frontier []string
	// StoppedBecause says which bound was hit, in words.
	StoppedBecause string
}

// Walk follows the genealogy out from one batch.
//
// Breadth-first, so a batch is reported at its shortest distance from the
// origin, and so that a bounded walk stops at a ring around the origin rather
// than partway down one arbitrary arm. Depth-first under a bound would return a
// deep sliver of the plant and call it the answer.
//
// A batch already seen is not followed again. The database refuses cycles, but
// this does not rely on that: a restore, a migration or a backfill can put a
// loop into a table that the trigger would have refused, and a recall is not the
// moment to find out that the genealogy is a graph with a knot in it.
func Walk(origin string, dir Direction, next Neighbours, lim Limit) (*Trace, error) {
	if origin == "" {
		return nil, errors.New("a walk must start somewhere")
	}
	if !ValidDirection(dir) {
		return nil, errors.New("a walk must say whether it goes forward into what was made from this batch, or backward into what it was made from")
	}
	if lim.MaxDepth <= 0 || lim.MaxBatches <= 0 {
		return nil, ErrNoLimit
	}

	t := &Trace{Origin: origin, Direction: dir, Complete: true}
	seen := map[string]bool{origin: true}
	frontier := []string{origin}

walk:
	for depth := 1; len(frontier) > 0; depth++ {
		if depth > lim.MaxDepth {
			t.stop(frontier, fmt.Sprintf(
				"the walk reached %d process steps from batch %s and stopped there; %d batches at "+
					"that distance were not followed, so anything beyond them is not in this list",
				lim.MaxDepth, origin, len(frontier)))
			break
		}

		edges, err := next(frontier)
		if err != nil {
			return nil, fmt.Errorf("loading level %d of the genealogy from %s: %w", depth, origin, err)
		}

		reachedFrom := map[string]bool{}
		for _, id := range frontier {
			reachedFrom[id] = true
		}

		var nextFrontier []string
		for _, e := range edges {
			from, to := e.InputBatchID, e.OutputBatchID
			if dir == Backward {
				from, to = e.OutputBatchID, e.InputBatchID
			}
			// An edge the loader returned that does not touch this level is not
			// this level's business. Ignoring it keeps a sloppy loader from
			// inventing depths.
			if !reachedFrom[from] || seen[to] {
				continue
			}
			seen[to] = true
			t.Reached = append(t.Reached, Reached{BatchID: to, Depth: depth, Via: from})
			nextFrontier = append(nextFrontier, to)

			if len(t.Reached) >= lim.MaxBatches {
				// Two things are unexplored, and both belong in the frontier.
				// The batches just named, which have not been followed; and the
				// batches of the level being processed, because the remaining
				// edges out of them were never looked at — some of their
				// neighbours have not even been named. Restarting from both,
				// with a larger bound, finds everything this walk missed.
				t.stop(append(append([]string(nil), frontier...), nextFrontier...), fmt.Sprintf(
					"the walk named %d batches, which is as many as it was allowed, and stopped; "+
						"the genealogy from batch %s continues past them and some batches one step "+
						"out were never reached at all",
					lim.MaxBatches, origin))
				break walk
			}
		}
		frontier = nextFrontier
	}

	sort.Slice(t.Reached, func(i, j int) bool {
		if t.Reached[i].Depth != t.Reached[j].Depth {
			return t.Reached[i].Depth < t.Reached[j].Depth
		}
		return t.Reached[i].BatchID < t.Reached[j].BatchID
	})
	sort.Strings(t.Frontier)
	return t, nil
}

func (t *Trace) stop(frontier []string, because string) {
	t.Complete = false
	t.Frontier = append([]string(nil), frontier...)
	t.StoppedBecause = because
}

// IDs is every batch the walk reached, in report order.
func (t *Trace) IDs() []string {
	out := make([]string, 0, len(t.Reached))
	for _, r := range t.Reached {
		out = append(out, r.BatchID)
	}
	return out
}

// ---------------------------------------------------------------------------
// Yield
// ---------------------------------------------------------------------------

// Yield is what a process actually gave, set against what was expected of it if
// anything was.
type Yield struct {
	BatchID string
	Output  quantity.Quantity

	// Consumed is everything that went in, totalled. Zero when the inputs could
	// not be totalled, which UnavailableReason then explains.
	Consumed quantity.Quantity

	// ObservedPPM is output over input in parts per million. One kilogram of
	// paneer from six litres of milk is 166666.
	ObservedPPM *int64
	// UnavailableReason says why there is no observed figure. The one that
	// happens in a real plant: milk measured in litres going into a product
	// weighed in kilograms, with nobody having supplied a density.
	UnavailableReason string

	ExpectedPPM *int64
	// VariancePPM is observed minus expected, in the same parts per million.
	// Nil when either side is missing.
	VariancePPM *int64
	// NoExpectationDeclared is true where the plant declared none.
	//
	// Reported rather than filled in. A variance against an invented target is
	// a number somebody is asked to explain and cannot, and the second time it
	// happens the report stops being read.
	NoExpectationDeclared bool
}

const ppm = 1_000_000

// ComputeYield totals what went into a batch and compares it with what came out.
//
// Inputs in a unit different from the output are converted, and converting needs
// a density that the caller supplies. Where none is supplied the yield is not
// computed and says so, rather than being computed on the three per cent this
// platform spends its time refusing to assume.
func ComputeYield(b *Batch, inputs []Input, d *quantity.Density, mode money.RoundingMode) (*Yield, error) {
	if b == nil {
		return nil, errors.New("no batch")
	}
	y := &Yield{
		BatchID:               b.ID,
		Output:                b.Produced,
		ExpectedPPM:           b.ExpectedYieldPPM,
		NoExpectationDeclared: b.ExpectedYieldPPM == nil,
	}
	if len(inputs) == 0 {
		y.UnavailableReason = fmt.Sprintf(
			"batch %s has no inputs recorded, so there is nothing to have yielded from", b.Code)
		return y, nil
	}

	total := quantity.Zero(b.Produced.Unit)
	for _, in := range inputs {
		q := in.Consumed
		if q.Unit != b.Produced.Unit {
			if d == nil {
				y.UnavailableReason = fmt.Sprintf(
					"batch %s is measured in %s and one of its inputs in %s; totalling them needs a "+
						"density and none was supplied, so no yield is reported rather than one "+
						"computed on an assumed figure",
					b.Code, b.Produced.Unit, q.Unit)
				return y, nil
			}
			conv, err := quantity.Convert(q, b.Produced.Unit, *d, mode)
			if err != nil {
				return nil, fmt.Errorf("converting an input of batch %s: %w", b.Code, err)
			}
			q = conv
		}
		sum, err := quantity.Add(total, q)
		if err != nil {
			return nil, fmt.Errorf("totalling the inputs of batch %s: %w", b.Code, err)
		}
		total = sum
	}
	y.Consumed = total

	if total.Value() == 0 {
		y.UnavailableReason = fmt.Sprintf(
			"the inputs of batch %s total nothing, and a yield from nothing has no figure", b.Code)
		return y, nil
	}

	// Both sides are at quantity.Scale, so the scales cancel and this is an
	// exact ratio of the measured integers.
	observed := b.Produced.Value() * ppm / total.Value()
	y.ObservedPPM = &observed

	if b.ExpectedYieldPPM != nil {
		v := observed - *b.ExpectedYieldPPM
		y.VariancePPM = &v
	}
	return y, nil
}

// PercentString renders a parts-per-million figure the way a plant says it out
// loud: 166666 ppm is "16.6666%".
func PercentString(p int64) string {
	sign := ""
	if p < 0 {
		sign, p = "-", -p
	}
	return fmt.Sprintf("%s%d.%04d%%", sign, p/10000, p%10000)
}
