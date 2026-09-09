package service

import (
	"context"
	"errors"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/currency"
	"github.com/ppusapati/gavya/libs/integrity/exact"
	"github.com/ppusapati/gavya/libs/integrity/money"
	"github.com/ppusapati/gavya/services/order-service/internal/domain"
	"github.com/ppusapati/gavya/services/order-service/internal/repository"
	ulidpkg "p9e.in/samavaya/packages/ulid"
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

// CreateOrder takes the currency as an argument rather than a field on the
// order, because an order's amounts each carry their own currency now and there
// is nothing to put it in until they exist. A new order's totals are zero, and
// zero of what is the question this answers.
func (s *Service) CreateOrder(ctx context.Context, o *domain.Order, currencyCode string) (*domain.Order, error) {
	if o.TenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if o.CustomerID == "" {
		return nil, invalid("customer_id is required")
	}
	o.ID = ulidpkg.New().String()
	o.OrderNumber = "ORD-" + ulidpkg.New().String()
	o.Status = "draft"
	// An order states the currency it is in, and the first one for a tenant
	// fixes it. There is no default: an order silently priced in rupees outside
	// India is an order nobody can fulfil.
	code, err := currency.Normalise(currencyCode)
	if err != nil {
		return nil, invalid("currency: " + err.Error())
	}
	scale, err := currency.Scale(code)
	if err != nil {
		return nil, invalid("currency: " + err.Error())
	}
	if err := s.repo.PinTenantMoney(ctx, o.TenantID, repository.Money{Code: code, Scale: scale}); err != nil {
		return nil, err
	}
	// A new order has no lines, so its totals are zero — but zero rupees, not a
	// bare zero. The currency has to be on them from the start, or the first
	// line added would be adding to an amount that is not in any currency.
	o.SubTotal = money.Zero(scale, code)
	o.TaxAmount = money.Zero(scale, code)
	o.TotalAmount = money.Zero(scale, code)
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

// moneyFor reports the currency this tenant records money in.
//
// It comes from the tenant's own pinned currency rather than from the request,
// and is read locally rather than from tenant-service, so adding a line does
// not stop working when another service is unreachable.
func (s *Service) moneyFor(ctx context.Context, tenantID string) (repository.Money, error) {
	denom, err := s.repo.TenantMoney(ctx, tenantID)
	if errors.Is(err, repository.ErrNotFound) {
		return repository.Money{}, invalid(
			"this tenant has not recorded any money yet; its first order must state a currency")
	}
	return denom, err
}

// AddOrderItem adds a line and brings the order's totals back in step with its
// contents.
//
// Neither the line total nor the order totals are computed here. Multiplying a
// quantity by a price in float64 and storing the result in a NUMERIC(12,2)
// column writes a rounded version of a number that was already inexact, and
// this is money. The repository does the arithmetic in the database, in the
// column's own type, inside the transaction that writes it.
// unitPrice arrives as a decimal literal rather than on the item, because the
// scale it is held to comes from the tenant's currency, which is read below.
func (s *Service) AddOrderItem(ctx context.Context, item *domain.OrderItem, unitPriceLiteral string) (*repository.ItemOutcome, error) {
	if item.OrderID == "" || item.TenantID == "" {
		return nil, invalid("order_id and tenant_id are required")
	}
	if item.Quantity <= 0 {
		return nil, invalid("quantity must be more than zero")
	}

	denom, err := s.moneyFor(ctx, item.TenantID)
	if err != nil {
		return nil, err
	}

	// The quantity and the tax rate still cross the wire as JSON numbers, so
	// they arrive as float64. That is survivable here and checked: both columns
	// are NUMERIC(_,3) holding values far below where float64 loses a digit, and
	// exact.NonNegativeDecimal refuses anything finer than the column rather
	// than letting PostgreSQL round it in silence. The price is the one that
	// could not be, and it is a literal now.
	quantity, err := exact.NonNegativeDecimal(item.Quantity, 3, 10)
	if err != nil {
		return nil, invalid(exact.Field("quantity", err).Error())
	}
	// Prices are held to the currency's own precision: a yen price has no
	// decimals, a dinar price has three. Parse refuses a finer literal rather
	// than rounding it.
	price, err := money.Parse(unitPriceLiteral, denom.Scale, denom.Code)
	if err != nil {
		return nil, invalid("unit_price: " + err.Error())
	}
	if price.Value < 0 {
		return nil, invalid("unit_price must not be negative")
	}
	item.UnitPrice = price
	taxRate, err := exact.NonNegativeDecimal(item.TaxRate, 3, 6)
	if err != nil {
		return nil, invalid(exact.Field("tax_rate", err).Error())
	}
	if item.TaxRate >= 1000 {
		return nil, invalid("tax_rate must be a percentage, and 1000% is not one")
	}

	item.ID = ulidpkg.New().String()
	if item.Status == "" {
		item.Status = "pending"
	}
	if item.CreatedBy == "" {
		item.CreatedBy = "system"
	}
	item.UpdatedBy = item.CreatedBy

	return s.repo.AddItemAndRetotal(ctx, item, quantity, price.String(), taxRate, denom)
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
