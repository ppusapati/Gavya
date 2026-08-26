// Package service runs a settlement: read the period's milk, recover what is
// owed, and write down what each producer takes home.
package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/money"

	"github.com/ppusapati/gavya/services/settlement-service/internal/domain"
	"github.com/ppusapati/gavya/services/settlement-service/internal/procurement"
	"github.com/ppusapati/gavya/services/settlement-service/internal/repository"
	"github.com/ppusapati/gavya/services/settlement-service/internal/statement"
)

type IDs interface{ New() string }

type Clock interface{ Now() time.Time }

type Logger interface {
	Infof(string, ...any)
	Errorf(string, ...any)
}

// Collections is where a period's priced milk comes from.
type Collections interface {
	Collections(ctx context.Context, tenantID, societyCode string, from, to time.Time) ([]procurement.Collection, error)
}

type Service struct {
	repo  repository.Repository
	milk  Collections
	ids   IDs
	clock Clock
	log   Logger
}

func New(r repository.Repository, milk Collections, ids IDs, clock Clock, log Logger) *Service {
	return &Service{repo: r, milk: milk, ids: ids, clock: clock, log: log}
}

// ErrNoMilkSource says the service was started without anywhere to read
// collections from.
var ErrNoMilkSource = errors.New("this service has no procurement to read collections from, " +
	"so it can gather nothing; set PROCUREMENT_URL")

func (s *Service) OpenCycle(ctx context.Context, c *domain.Cycle) (*domain.Cycle, error) {
	c.PeriodStart = domain.Day(c.PeriodStart)
	c.PeriodEnd = domain.Day(c.PeriodEnd)
	return s.repo.CreateCycle(ctx, c)
}

func (s *Service) GetCycle(ctx context.Context, tenantID, id string) (*domain.Cycle, error) {
	return s.repo.GetCycle(ctx, tenantID, id)
}

func (s *Service) ListCycles(ctx context.Context, tenantID, societyCode string, limit, offset int) ([]*domain.Cycle, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.repo.ListCycles(ctx, tenantID, societyCode, limit, offset)
}

func (s *Service) OpenRecovery(ctx context.Context, r *domain.Recovery) (*domain.Recovery, error) {
	r.OpenedOn = domain.Day(r.OpenedOn)
	if r.Recovered.Currency == "" {
		r.Recovered = money.Zero(r.Principal.Scale, r.Principal.Currency)
	}
	if r.Instalment.Currency == "" {
		r.Instalment = money.Zero(r.Principal.Scale, r.Principal.Currency)
	}
	return s.repo.CreateRecovery(ctx, r)
}

func (s *Service) ListRecoveries(ctx context.Context, tenantID, producerRef string, outstandingOnly bool) ([]*domain.Recovery, error) {
	return s.repo.ListRecoveries(ctx, tenantID, producerRef, outstandingOnly)
}

// Gather runs the settlement for a cycle.
//
// The order of operations is the design:
//
//  1. Read the period's collections from procurement. Settlement does not price
//     milk; a second implementation of the rate card would eventually disagree
//     with the first, and the producer would be holding the slip that proves it.
//
//  2. Total each producer's milk exactly. Every amount is an integer of minor
//     units and they are added, not averaged or rounded, so the sum of the
//     payables is the sum of the collections to the paisa.
//
//  3. Recover what each producer owes, in the cycle's declared policy and the
//     recoveries' declared order.
//
//  4. Write all of it in one transaction. Every partial state is a state in
//     which somebody has been charged and not credited, or credited and not
//     charged.
func (s *Service) Gather(ctx context.Context, tenantID, cycleID, actor string) (*domain.Cycle, error) {
	if s.milk == nil {
		return nil, ErrNoMilkSource
	}
	if actor == "" {
		return nil, errors.New("actor is required: a settlement decides what several hundred " +
			"people are paid, so who ran it has to be on the record")
	}

	cycle, err := s.repo.GetCycle(ctx, tenantID, cycleID)
	if err != nil {
		return nil, err
	}
	if cycle.Status != domain.CycleOpen {
		return nil, &domain.ErrWrongStatus{
			What: "cycle", ID: cycleID, Is: string(cycle.Status),
			Wanted: string(domain.CycleOpen), Action: "gather into",
		}
	}

	collections, err := s.milk.Collections(ctx, tenantID, cycle.SocietyCode,
		cycle.PeriodStart, cycle.PeriodEnd)
	if err != nil {
		return nil, err
	}

	// Grouped by producer, and the producers taken in a fixed order. The amounts
	// do not depend on the order, but the ids assigned to rows do, and a
	// settlement that produces different ids for the same input is one nobody
	// can diff against a re-run.
	byProducer := map[string][]procurement.Collection{}
	for _, c := range collections {
		byProducer[c.ProducerRef] = append(byProducer[c.ProducerRef], c)
	}
	producers := make([]string, 0, len(byProducer))
	for ref := range byProducer {
		producers = append(producers, ref)
	}
	sort.Strings(producers)

	g := &repository.Gathered{
		Recovered: map[string]money.Money{},
		At:        s.clock.Now(),
		Actor:     actor,
	}

	for _, ref := range producers {
		lines := byProducer[ref]
		sort.SliceStable(lines, func(i, j int) bool {
			if !lines[i].CollectedOn.Equal(lines[j].CollectedOn) {
				return lines[i].CollectedOn.Before(lines[j].CollectedOn)
			}
			if lines[i].Shift != lines[j].Shift {
				return lines[i].Shift < lines[j].Shift
			}
			return lines[i].ID < lines[j].ID
		})

		gross := money.Zero(cycle.AmountScale, cycle.Currency)
		for _, c := range lines {
			gross, err = money.Add(gross, c.Amount)
			if err != nil {
				// A collection priced in another currency or to another scale is
				// not something to sum past. It means the tenant's currency
				// changed mid-period, which is a question for a person.
				return nil, fmt.Errorf("producer %s, collection %s: %w", ref, c.ID, err)
			}
			g.Lines = append(g.Lines, &domain.Line{
				TenantID: tenantID, CycleID: cycleID, ProducerRef: ref,
				CollectionID: c.ID, CollectedOn: c.CollectedOn, Shift: c.Shift,
				Quantity: c.Quantity, QuantityUnit: c.QuantityUnit, Rate: c.Rate,
				Amount: c.Amount,
			})
		}

		debts, err := s.repo.ListRecoveries(ctx, tenantID, ref, true)
		if err != nil {
			return nil, err
		}
		settled, err := domain.Settle(gross, debts, cycle.Policy)
		if err != nil {
			return nil, fmt.Errorf("settle producer %s: %w", ref, err)
		}

		byID := map[string]*domain.Recovery{}
		for _, d := range debts {
			byID[d.ID] = d
		}
		for _, tk := range settled.Taken {
			g.Deductions = append(g.Deductions, &domain.Deduction{
				TenantID: tenantID, CycleID: cycleID, RecoveryID: tk.RecoveryID,
				ProducerRef: ref, Kind: tk.Kind, Reference: tk.Reference,
				Amount: tk.Amount,
			})
			d := byID[tk.RecoveryID]
			advanced, err := money.Add(d.Recovered, tk.Amount)
			if err != nil {
				return nil, err
			}
			g.Recovered[tk.RecoveryID] = advanced
		}

		g.Payables = append(g.Payables, &domain.ProducerPayable{
			TenantID: tenantID, CycleID: cycleID, ProducerRef: ref,
			Gross: settled.Gross, Deducted: settled.Deducted, Net: settled.Net,
			CarriedForward: settled.CarriedForward,
		})
	}

	if err := s.repo.Gather(ctx, cycleID, g); err != nil {
		return nil, err
	}
	s.log.Infof("gathered cycle %s: %d producers, %d collections, %d deductions",
		cycleID, len(producers), len(g.Lines), len(g.Deductions))
	return s.repo.GetCycle(ctx, tenantID, cycleID)
}

func (s *Service) AbandonCycle(ctx context.Context, tenantID, cycleID, actor string) error {
	if actor == "" {
		return errors.New("actor is required")
	}
	return s.repo.AbandonCycle(ctx, tenantID, cycleID, actor)
}

// ApproveCycle signs off the figures.
//
// The actor is required and is not the same person as whoever gathered. This
// service does not enforce that they differ — a one-person society is a real
// thing and refusing to let them approve their own work would make the platform
// unusable there — but it records both, so a society that wants the separation
// can see whether it has it.
func (s *Service) ApproveCycle(ctx context.Context, tenantID, cycleID, actor string) (*domain.Cycle, error) {
	if actor == "" {
		return nil, errors.New("actor is required: approval is a person taking responsibility " +
			"for what several hundred people are about to be paid")
	}
	return s.repo.ApproveCycle(ctx, tenantID, cycleID, actor)
}

func (s *Service) ListPayables(ctx context.Context, tenantID, cycleID string) ([]*domain.ProducerPayable, error) {
	return s.repo.ListPayables(ctx, tenantID, cycleID)
}

func (s *Service) GetPayable(ctx context.Context, tenantID, id string) (*domain.ProducerPayable, error) {
	return s.repo.GetPayable(ctx, tenantID, id)
}

func (s *Service) MarkPaid(ctx context.Context, tenantID, id, reference, actor string) (*domain.ProducerPayable, error) {
	if actor == "" {
		return nil, errors.New("actor is required")
	}
	return s.repo.MarkPaid(ctx, tenantID, id, reference, actor, s.clock.Now())
}

// RaiseAdjustment records money owed after a cycle was already settled.
//
// This is the remedy the paid-is-final trigger names. That trigger refuses to
// edit a payable that has been paid and says a payment that turned out to be
// wrong is corrected by a further payment rather than by editing the record of
// the one that happened — and until this existed, there was no further payment
// to make, so the refusal named a remedy the platform did not have.
func (s *Service) RaiseAdjustment(ctx context.Context, p *domain.ProducerPayable, actor string) (*domain.ProducerPayable, error) {
	if actor == "" {
		return nil, errors.New("actor is required: an adjustment moves money outside the " +
			"settlement that computed it, so who raised it has to be on the record")
	}
	if p.ProducerRef == "" {
		return nil, domain.ErrNoProducer
	}
	if p.Reason == "" {
		return nil, domain.ErrNoAdjustmentReason
	}
	if p.Net.IsZero() {
		return nil, domain.ErrZeroAdjustment
	}
	// Gross and deducted are the net for an adjustment: it is a bare amount,
	// not a fortnight with recoveries taken out of it.
	p.Gross, p.Deducted = p.Net, money.Zero(p.Net.Scale, p.Net.Currency)
	p.CarriedForward = money.Zero(p.Net.Scale, p.Net.Currency)
	return s.repo.RaiseAdjustment(ctx, p, actor)
}

// ApprovePayable signs off one payable, which is how an adjustment raised
// against a finished cycle is approved.
func (s *Service) ApprovePayable(ctx context.Context, tenantID, id, actor string) (*domain.ProducerPayable, error) {
	if actor == "" {
		return nil, errors.New("actor is required")
	}
	return s.repo.ApprovePayable(ctx, tenantID, id, actor)
}

func (s *Service) HoldPayable(ctx context.Context, tenantID, id, reason, actor string) (*domain.ProducerPayable, error) {
	if actor == "" {
		return nil, errors.New("actor is required")
	}
	return s.repo.HoldPayable(ctx, tenantID, id, reason, actor)
}

func (s *Service) Statement(ctx context.Context, tenantID, cycleID, producerRef string) (*domain.Statement, error) {
	if producerRef == "" {
		return nil, domain.ErrNoProducer
	}
	return s.repo.Statement(ctx, tenantID, cycleID, producerRef)
}

// PrintStatement lays a producer's settlement out as the page they are handed.
//
// The same figures as Statement, arranged for a printer rather than for a
// program. It is a separate procedure rather than a field on the JSON because
// the two have different failure modes: a statement that cannot be laid out on
// a narrow page is still perfectly good data, and a caller reading the figures
// should not be refused because somebody else's printer is 40 columns.
func (s *Service) PrintStatement(ctx context.Context, tenantID, cycleID, producerRef string, o statement.Options) (string, error) {
	st, err := s.Statement(ctx, tenantID, cycleID, producerRef)
	if err != nil {
		return "", err
	}
	return statement.Render(st, o)
}

// PrintCycle lays out every producer's statement for a cycle, in one run.
//
// This is what a society actually does: one press at the end of the fortnight
// and a stack of pages to hand out. Producing them one call at a time works and
// means a clerk discovers the two hundredth is unprintable after handing out
// a hundred and ninety-nine.
//
// So a page that will not lay out fails the whole run. A partial stack is worse
// than no stack: the members who got one believe the settlement is done and the
// members who did not have nothing to compare against.
func (s *Service) PrintCycle(ctx context.Context, tenantID, cycleID string, o statement.Options) ([]PrintedStatement, error) {
	payables, err := s.repo.ListPayables(ctx, tenantID, cycleID)
	if err != nil {
		return nil, err
	}
	if len(payables) == 0 {
		return nil, fmt.Errorf("this cycle has no payables, so there is nothing to print: " +
			"it has not been gathered")
	}
	out := make([]PrintedStatement, 0, len(payables))
	for _, p := range payables {
		page, err := s.PrintStatement(ctx, tenantID, cycleID, p.ProducerRef, o)
		if err != nil {
			return nil, fmt.Errorf("producer %s: %w", p.ProducerRef, err)
		}
		out = append(out, PrintedStatement{ProducerRef: p.ProducerRef, Page: page})
	}
	return out, nil
}

// PrintedStatement is one member's page.
type PrintedStatement struct {
	ProducerRef string
	Page        string
}
