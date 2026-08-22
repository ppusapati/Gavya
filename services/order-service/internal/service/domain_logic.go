package service

import (
	"context"
	"errors"
	"time"

	"github.com/ppusapati/gavya/services/order-service/internal/domain"
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

func (s *Service) AddOrderItem(ctx context.Context, item *domain.OrderItem) (*domain.OrderItem, error) {
	if item.OrderID == "" || item.TenantID == "" {
		return nil, invalid("order_id and tenant_id are required")
	}
	// Validate order is still in draft
	order, err := s.repo.GetOrder(ctx, item.OrderID, item.TenantID)
	if err != nil {
		return nil, err
	}
	if order.Status != "draft" {
		return nil, invalid("can only add items to draft orders")
	}
	item.ID = ulidpkg.New().String()
	item.TotalPrice = item.Quantity * item.UnitPrice
	if item.Status == "" {
		item.Status = "pending"
	}
	if item.CreatedBy == "" {
		item.CreatedBy = "system"
	}
	item.UpdatedBy = item.CreatedBy

	result, err := s.repo.CreateOrderItem(ctx, item)
	if err != nil {
		return nil, err
	}

	// Recalculate order totals
	subTotal, err := s.repo.SumOrderItems(ctx, item.OrderID, item.TenantID)
	if err != nil {
		s.log.Errorf("failed to sum order items: %v", err)
	} else {
		taxAmount := subTotal * 0.18
		totalAmount := subTotal + taxAmount
		if _, err := s.repo.UpdateOrderTotals(ctx, item.OrderID, item.TenantID, subTotal, taxAmount, totalAmount, item.CreatedBy); err != nil {
			s.log.Errorf("failed to update order totals: %v", err)
		}
	}

	return result, nil
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
