// Package service prices collections against the card that was in force when
// the milk arrived.
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/origin"
	"github.com/ppusapati/gavya/libs/integrity/ratecard"

	"github.com/ppusapati/gavya/services/procurement-service/internal/domain"
	"github.com/ppusapati/gavya/services/procurement-service/internal/repository"
)

type IDs interface{ New() string }

type Clock interface{ Now() time.Time }

type Logger interface {
	Infof(string, ...any)
	Errorf(string, ...any)
}

type Service struct {
	repo  repository.Repository
	ids   IDs
	clock Clock
	log   Logger
}

func New(r repository.Repository, ids IDs, clock Clock, log Logger) *Service {
	return &Service{repo: r, ids: ids, clock: clock, log: log}
}

// DeclareRateCard records what a society's milk is worth.
func (s *Service) DeclareRateCard(ctx context.Context, c *ratecard.Card, actor string) (*ratecard.Card, error) {
	if actor == "" {
		return nil, fmt.Errorf("actor is required: a rate card is the policy every payment is " +
			"computed from, so who set it has to be on the record")
	}
	return s.repo.CreateRateCard(ctx, c, actor)
}

func (s *Service) GetRateCard(ctx context.Context, tenantID, id string) (*ratecard.Card, error) {
	return s.repo.GetRateCard(ctx, tenantID, id)
}

func (s *Service) ListRateCards(ctx context.Context, tenantID string, limit, offset int) ([]*ratecard.Card, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.repo.ListRateCards(ctx, tenantID, limit, offset)
}

// CardFor returns the card that priced milk collected on a day.
func (s *Service) CardFor(ctx context.Context, tenantID string, at time.Time) (*ratecard.Card, error) {
	return s.repo.CardInForce(ctx, tenantID, domain.Day(at))
}

// RecordCollection prices one delivery and stores it.
//
// The card is chosen by when the milk was collected, not by when this is being
// entered. A society catching up on a week of paper slips must price each day at
// what it was worth that day, and an operator entering yesterday's milk after a
// rate change must not be paid today's rate for it.
func (s *Service) RecordCollection(ctx context.Context, in domain.Collection) (*domain.PricedCollection, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	day := domain.Day(in.CollectedOn)

	card, err := s.repo.CardInForce(ctx, in.TenantID, day)
	if err != nil {
		return nil, err
	}

	priced, err := ratecard.Price(card, ratecard.Collection{
		Quantity: in.Quantity, Unit: in.Unit,
		Fat: in.Fat, SNF: in.SNF,
		FatKg: in.FatKg, SNFKg: in.SNFKg,
	})
	if err != nil {
		// Returned as it is rather than wrapped into something generic. These
		// errors say which reading was off the chart or which decision the card
		// has not made, and that is what somebody needs in order to fix it.
		return nil, err
	}

	kind := in.Origin.Kind
	if kind == "" {
		kind = origin.Native
	}

	return s.repo.SaveCollection(ctx, &domain.PricedCollection{
		ID:       s.ids.New(),
		TenantID: in.TenantID,

		ProducerRef: in.ProducerRef,
		SocietyCode: in.SocietyCode,
		CollectedOn: day,
		Shift:       in.Shift,

		Quantity: in.Quantity, Unit: in.Unit, Fat: in.Fat, SNF: in.SNF,

		RateCardID:  card.ID,
		Rate:        priced.Rate,
		Amount:      priced.Amount,
		Explanation: priced.Explanation,

		Origin:         origin.Origin{Kind: kind},
		SourceSystemID: in.SourceSystemID,
		ImportBatchID:  in.ImportBatchID,
		SourceRecordID: in.SourceRecordID,

		CreatedAt: s.clock.Now(),
		CreatedBy: in.Actor,
	})
}

func (s *Service) GetCollection(ctx context.Context, tenantID, id string) (*domain.PricedCollection, error) {
	return s.repo.GetCollection(ctx, tenantID, id)
}

func (s *Service) ListCollections(ctx context.Context, tenantID, producerRef string, from, to time.Time, limit, offset int) ([]*domain.PricedCollection, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	if from.IsZero() {
		return nil, fmt.Errorf("a period has to be given: listing every collection a society has " +
			"ever taken is not a question anybody is asking")
	}
	if to.IsZero() {
		to = s.clock.Now()
	}
	if to.Before(from) {
		return nil, fmt.Errorf("the period ends before it starts")
	}
	return s.repo.ListCollections(ctx, tenantID, producerRef, domain.Day(from), domain.Day(to), limit, offset)
}
