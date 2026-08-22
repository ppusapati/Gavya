package service

import (
	"context"
	"errors"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/exact"
	"github.com/ppusapati/gavya/services/order-service/internal/domain"
	"github.com/ppusapati/gavya/services/order-service/internal/repository"
	ulidpkg "p9e.in/samavaya/packages/ULID"
)

// ErrInvalidArgument marks a caller mistake. Without it the handler cannot tell
// "you did not supply a customer_id" from "the query failed", and would have to
// report both the same way. A violated business rule — confirming an order that
// is no longer a draft — is a caller mistake too, not an internal failure.
var ErrInvalidArgument = errors.New("invalid argument")

// invalidArgument carries the reason alone. The marker is matched through Is,
// so errors.Is finds it while the message stays free of a prefix the error code
// already conveys.
type invalidArgument struct{ reason string }

func (e *invalidArgument) Error() string { return e.reason }

func (e *invalidArgument) Is(target error) bool { return target == ErrInvalidArgument }

func invalid(msg string) error { return &invalidArgument{reason: msg} }

func (s *Service) CreateOrder(ctx context.Context, o *domain.Order) (*domain.Order, error) {
	if o.TenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if o.CustomerID == "" {
		return nil, invalid("customer_id is required")
	}
	o.ID = ulidpkg.New().String()
	o.OrderNumber = "ORD-" + ulidpkg.New().String()
	o.Status = "draft"
	o.SubTotal = 0
	o.TaxAmount = 0
	o.TotalAmount = 0
	if o.Currency == "" {
		o.Currency = "INR"
	}
	if o.OrderedAt.IsZero() {
		o.OrderedAt = time.Now()
	}
	if o.CreatedBy == "" {
		o.CreatedBy = "system"
	}
	o.UpdatedBy = o.CreatedBy
	return s.repo.CreateOrder(ctx, o)
}

func (s *Service) GetOrder(ctx context.Context, id, tenantID string) (*domain.Order, error) {
	if id == "" || tenantID == "" {
		return nil, invalid("id and tenant_id are required")
	}
	return s.repo.GetOrder(ctx, id, tenantID)
}

// DefaultTaxRate is the rate applied to every order.
//
// It is a placeholder, not a tax model: one rate for every line of every order
// cannot be right for a catalogue that mixes exempt and rated goods. It is kept
// here, named and in one place, so that it is visible as a decision rather than
// buried as a literal in an expression — and so that replacing it with a real
// per-line rate changes one thing.
const DefaultTaxRate = "0.18"

// AddOrderItem adds a line and brings the order's totals back in step with its
// contents.
//
// Neither the line total nor the order totals are computed here. Multiplying a
// quantity by a price in float64 and storing the result in a NUMERIC(12,2)
// column writes a rounded version of a number that was already inexact, and
// this is money. The repository does the arithmetic in the database, in the
// column's own type, inside the transaction that writes it.
func (s *Service) AddOrderItem(ctx context.Context, item *domain.OrderItem) (*repository.ItemOutcome, error) {
	if item.OrderID == "" || item.TenantID == "" {
		return nil, invalid("order_id and tenant_id are required")
	}
	if item.Quantity <= 0 {
		return nil, invalid("quantity must be more than zero")
	}

	// The wire carries these as JSON numbers, so they arrive as float64. They are
	// converted to the exact decimals the columns hold, and a value finer than
	// that is refused rather than rounded on the way in without telling anyone.
	quantity, err := exact.NonNegativeDecimal(item.Quantity, 3, 10)
	if err != nil {
		return nil, invalid(exact.Field("quantity", err).Error())
	}
	unitPrice, err := exact.NonNegativeDecimal(item.UnitPrice, 2, 12)
	if err != nil {
		return nil, invalid(exact.Field("unit_price", err).Error())
	}

	item.ID = ulidpkg.New().String()
	if item.Status == "" {
		item.Status = "pending"
	}
	if item.CreatedBy == "" {
		item.CreatedBy = "system"
	}
	item.UpdatedBy = item.CreatedBy

	return s.repo.AddItemAndRetotal(ctx, item, quantity, unitPrice, DefaultTaxRate)
}

func (s *Service) ConfirmOrder(ctx context.Context, id, tenantID, updatedBy string) (*domain.Order, error) {
	order, err := s.repo.GetOrder(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}
	if order.Status != "draft" {
		return nil, invalid("can only confirm draft orders")
	}
	if updatedBy == "" {
		updatedBy = "system"
	}
	return s.repo.UpdateOrderStatus(ctx, id, tenantID, "confirmed", updatedBy)
}

func (s *Service) CancelOrder(ctx context.Context, id, tenantID, updatedBy string) (*domain.Order, error) {
	order, err := s.repo.GetOrder(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}
	if order.Status != "draft" && order.Status != "confirmed" {
		return nil, invalid("can only cancel draft or confirmed orders")
	}
	if updatedBy == "" {
		updatedBy = "system"
	}
	return s.repo.UpdateOrderStatus(ctx, id, tenantID, "cancelled", updatedBy)
}

func (s *Service) GenerateInvoice(ctx context.Context, orderID, tenantID, createdBy string) (*domain.Invoice, error) {
	order, err := s.repo.GetOrder(ctx, orderID, tenantID)
	if err != nil {
		return nil, err
	}
	if order.Status != "confirmed" {
		return nil, invalid("can only generate invoice for confirmed orders")
	}
	now := time.Now()
	inv := &domain.Invoice{
		ID:            ulidpkg.New().String(),
		TenantID:      tenantID,
		OrderID:       orderID,
		InvoiceNumber: "INV-" + ulidpkg.New().String(),
		Status:        "draft",
		SubTotal:      order.SubTotal,
		TaxAmount:     order.TaxAmount,
		TotalAmount:   order.TotalAmount,
		Currency:      order.Currency,
		IssuedAt:      now,
		DueAt:         now.AddDate(0, 0, 30),
		CreatedBy:     createdBy,
		UpdatedBy:     createdBy,
	}
	if inv.CreatedBy == "" {
		inv.CreatedBy = "system"
		inv.UpdatedBy = "system"
	}
	return s.repo.CreateInvoice(ctx, inv)
}
