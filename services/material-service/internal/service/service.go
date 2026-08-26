// Package service records milk moving between places, and works out what the
// two ends of a movement came to.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ppusapati/gavya/services/material-service/internal/domain"
	"github.com/ppusapati/gavya/services/material-service/internal/repository"
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

func (s *Service) RegisterNode(ctx context.Context, n *domain.Node) (*domain.Node, error) {
	return s.repo.CreateNode(ctx, n)
}

func (s *Service) GetNode(ctx context.Context, tenantID, id string) (*domain.Node, error) {
	return s.repo.GetNode(ctx, tenantID, id)
}

func (s *Service) ListNodes(ctx context.Context, tenantID string, kind domain.NodeKind) ([]*domain.Node, error) {
	if kind != "" && !domain.ValidNodeKind(kind) {
		return nil, fmt.Errorf("%q is not a kind of node this platform knows", kind)
	}
	return s.repo.ListNodes(ctx, tenantID, kind)
}

// Dispatch records milk leaving a node.
//
// Both ends are resolved before anything is written, so a movement naming a
// node that does not exist is refused with the code somebody typed rather than
// with a foreign key violation naming a constraint.
func (s *Service) Dispatch(ctx context.Context, in domain.Dispatch) (*domain.Movement, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	from, err := s.repo.GetNode(ctx, in.TenantID, in.FromNodeID)
	if err != nil {
		return nil, fmt.Errorf("the node the milk left: %w", err)
	}
	to, err := s.repo.GetNode(ctx, in.TenantID, in.ToNodeID)
	if err != nil {
		return nil, fmt.Errorf("the node the milk is going to: %w", err)
	}
	if !from.Active || !to.Active {
		return nil, fmt.Errorf("one of %s and %s is not in service", from.Code, to.Code)
	}

	// A vessel that cannot hold what is being put into it is a reading somebody
	// should look at before it becomes a transit loss. Refused rather than
	// warned: a 12,000 litre load into a 10,000 litre tanker is a mis-keyed
	// figure roughly every time, and accepting it puts 2,000 litres of
	// phantom milk into the balance.
	if to.Capacity != nil && to.Capacity.Unit == in.Quantity.Unit {
		if in.Quantity.Value() > to.Capacity.Value() {
			return nil, fmt.Errorf("%s holds %s and this movement puts %s into it",
				to.Code, to.Capacity.Describe(), in.Quantity.Describe())
		}
	}

	return s.repo.Dispatch(ctx, &domain.Movement{
		TenantID: in.TenantID, FromNodeID: in.FromNodeID, ToNodeID: in.ToNodeID,
		DispatchedAt: in.At, Dispatched: in.Quantity, DispatchMethod: in.Method,
		Holdup: in.Holdup, CreatedBy: in.Actor,
	})
}

// Receive records milk arriving and what the two ends came to.
func (s *Service) Receive(ctx context.Context, in domain.Receipt) (*domain.Movement, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	m, err := s.repo.GetMovement(ctx, in.TenantID, in.MovementID)
	if err != nil {
		return nil, err
	}
	if m.Status != domain.InTransit {
		return nil, fmt.Errorf("%w: it is %s", domain.ErrAlreadyClosed, m.Status)
	}
	if in.At.Before(m.DispatchedAt) {
		return nil, fmt.Errorf("%w: it left at %s and this receipt is timed %s",
			domain.ErrArrivedBefore,
			m.DispatchedAt.UTC().Format(time.RFC3339), in.At.UTC().Format(time.RFC3339))
	}

	variance, why := domain.Reconcile(m.Dispatched, &in)
	if variance == nil && why == "" {
		// Neither a figure nor a reason is the one outcome that must not reach
		// the database: the constraint there would refuse it, and the message
		// would name a constraint rather than the thing that went wrong.
		return nil, errors.New("the movement could not be reconciled and no reason was produced")
	}

	m.ReceivedAt, m.Received, m.ReceiptMethod = &in.At, &in.Quantity, in.Method
	m.Density, m.Variance, m.VarianceUnavailableReason = in.Density, variance, why

	saved, err := s.repo.Receive(ctx, m, in.Actor)
	if err != nil {
		return nil, err
	}
	if saved.Variance == nil {
		s.log.Infof("movement %s received and not reconciled: %s", saved.ID, saved.VarianceUnavailableReason)
	}
	return saved, nil
}

func (s *Service) Abandon(ctx context.Context, tenantID, id, reason, actor string) (*domain.Movement, error) {
	if actor == "" {
		return nil, errors.New("actor is required")
	}
	return s.repo.Abandon(ctx, tenantID, id, reason, actor)
}

func (s *Service) GetMovement(ctx context.Context, tenantID, id string) (*domain.Movement, error) {
	return s.repo.GetMovement(ctx, tenantID, id)
}

func (s *Service) ListMovements(ctx context.Context, tenantID, nodeID string, from, to time.Time, limit int) ([]*domain.Movement, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	if from.IsZero() {
		return nil, errors.New("a period has to be given: listing every movement a society has " +
			"ever made is not a question anybody is asking")
	}
	if to.IsZero() {
		to = s.clock.Now()
	}
	if to.Before(from) {
		return nil, errors.New("the period ends before it starts")
	}
	return s.repo.ListMovements(ctx, tenantID, nodeID, from, to, limit)
}
