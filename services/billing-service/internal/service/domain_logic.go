package service

import (
	"context"
	"errors"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/currency"
	"github.com/ppusapati/gavya/libs/integrity/exact"
	"github.com/ppusapati/gavya/services/billing-service/internal/domain"
	"github.com/ppusapati/gavya/services/billing-service/internal/repository"
	ulidpkg "p9e.in/samavaya/packages/ulid"
)

// ErrInvalidArgument marks a caller mistake. Without it the handler cannot tell
// "you did not supply a customer_id" from "the query failed", and would have to
// report both the same way. A violated business rule — voiding an invoice that
// is already paid — is a caller mistake too, not an internal failure.
var ErrInvalidArgument = errors.New("invalid argument")

// invalidArgument carries the reason alone. The marker is matched through Is,
// so errors.Is finds it while the message stays free of a prefix the error code
// already conveys.
type invalidArgument struct{ reason string }

func (e *invalidArgument) Error() string { return e.reason }

func (e *invalidArgument) Is(target error) bool { return target == ErrInvalidArgument }

func invalid(msg string) error { return &invalidArgument{reason: msg} }

func (s *Service) CreateInvoice(ctx context.Context, inv *domain.Invoice) (*domain.Invoice, error) {
	if inv.TenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if inv.CustomerID == "" {
		return nil, invalid("customer_id is required")
	}
	// An invoice states the currency it is in, and the first one for a tenant
	// fixes it. There is no default: a deployment outside India that silently
	// billed in rupees would produce invoices nobody could pay.
	code, err := currency.Normalise(inv.Currency)
	if err != nil {
		return nil, invalid("currency: " + err.Error())
	}
	scale, err := currency.Scale(code)
	if err != nil {
		return nil, invalid("currency: " + err.Error())
	}
	if err := s.repo.PinTenantMoney(ctx, inv.TenantID, repository.Money{Code: code, Scale: scale}); err != nil {
		return nil, err
	}
	inv.Currency = code

	inv.ID = ulidpkg.New().String()
	inv.InvoiceNumber = "INV-" + ulidpkg.New().String()
	if inv.Status == "" {
		inv.Status = "draft"
	}

	if inv.IssuedAt.IsZero() {
		inv.IssuedAt = time.Now()
	}
	inv.DueAt = inv.IssuedAt.AddDate(0, 0, 30)
	if inv.CreatedBy == "" {
		inv.CreatedBy = "system"
	}
	inv.UpdatedBy = inv.CreatedBy
	return s.repo.CreateInvoice(ctx, inv)
}

// AddInvoiceItem adds a line and brings the invoice's totals back in step with
// its lines.
//
// The arithmetic is deliberately not here. A line total multiplied in float64
// and stored into NUMERIC(12,2) is a rounded version of an already-inexact
// product, and on an invoice that is what someone is asked to pay. The
// repository multiplies in the database, in the columns' own type, in the
// transaction that writes both the line and the totals.
// moneyFor reports the currency this tenant records money in.
//
// It comes from the tenant's own pinned currency rather than from the request,
// so a line cannot be added in a currency the invoice is not in — and it is
// read locally rather than from tenant-service, so billing does not stop
// working when tenant-service is unreachable.
func (s *Service) moneyFor(ctx context.Context, tenantID string) (repository.Money, error) {
	money, err := s.repo.TenantMoney(ctx, tenantID)
	if errors.Is(err, repository.ErrNotFound) {
		return repository.Money{}, invalid(
			"this tenant has not recorded any money yet; its first invoice must state a currency")
	}
	return money, err
}

func (s *Service) AddInvoiceItem(ctx context.Context, item *domain.InvoiceItem) (*repository.ItemOutcome, error) {
	if item.InvoiceID == "" || item.TenantID == "" {
		return nil, invalid("invoice_id and tenant_id are required")
	}
	if item.Quantity <= 0 {
		return nil, invalid("quantity must be more than zero")
	}

	money, err := s.moneyFor(ctx, item.TenantID)
	if err != nil {
		return nil, err
	}

	quantity, err := exact.NonNegativeDecimal(item.Quantity, 3, 10)
	if err != nil {
		return nil, invalid(exact.Field("quantity", err).Error())
	}
	// Prices are held to the currency's own precision: a yen price has no
	// decimals, a dinar price has three. Validating everything at two would
	// accept a yen price of 100.50 and refuse a legitimate dinar price.
	unitPrice, err := exact.NonNegativeDecimal(item.UnitPrice, money.Scale, 18)
	if err != nil {
		return nil, invalid(exact.Field("unit_price", err).Error())
	}
	// A tax rate is a percentage, not money, so its precision is its own.
	taxRate, err := exact.NonNegativeDecimal(item.TaxRate, 3, 6)
	if err != nil {
		return nil, invalid(exact.Field("tax_rate", err).Error())
	}
	if item.TaxRate >= 1000 {
		return nil, invalid("tax_rate must be a percentage, and 1000% is not one")
	}

	item.ID = ulidpkg.New().String()
	if item.CreatedBy == "" {
		item.CreatedBy = "system"
	}
	item.UpdatedBy = item.CreatedBy

	return s.repo.AddItemAndRetotal(ctx, item, quantity, unitPrice, taxRate, money)
}

func (s *Service) SendInvoice(ctx context.Context, id, tenantID, updatedBy string) (*domain.Invoice, error) {
	inv, err := s.repo.GetInvoice(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}
	if inv.Status != "draft" {
		return nil, invalid("can only send draft invoices")
	}
	if updatedBy == "" {
		updatedBy = "system"
	}
	return s.repo.UpdateInvoiceStatus(ctx, id, tenantID, "sent", updatedBy)
}

// RecordPayment writes a payment and settles the invoice if it is now covered.
//
// Both happen in one transaction in the repository. Deciding whether an invoice
// is paid by comparing two float64 values read out of NUMERIC columns, in a
// separate call from the one that wrote the payment, left an invoice's state
// able to disagree with the money against it.
func (s *Service) RecordPayment(ctx context.Context, p *domain.Payment) (*repository.PaymentOutcome, error) {
	if p.InvoiceID == "" || p.TenantID == "" {
		return nil, invalid("invoice_id and tenant_id are required")
	}
	if p.Amount <= 0 {
		return nil, invalid("a payment must be for more than zero")
	}
	money, err := s.moneyFor(ctx, p.TenantID)
	if err != nil {
		return nil, err
	}
	amount, err := exact.NonNegativeDecimal(p.Amount, money.Scale, 18)
	if err != nil {
		return nil, invalid(exact.Field("amount", err).Error())
	}

	p.ID = ulidpkg.New().String()
	p.Currency = money.Code
	if p.PaidAt.IsZero() {
		p.PaidAt = time.Now()
	}
	if p.CreatedBy == "" {
		p.CreatedBy = "system"
	}
	p.UpdatedBy = p.CreatedBy

	return s.repo.RecordPaymentAndSettle(ctx, p, amount, money)
}

func (s *Service) VoidInvoice(ctx context.Context, id, tenantID, updatedBy string) (*domain.Invoice, error) {
	inv, err := s.repo.GetInvoice(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}
	if inv.Status == "paid" {
		return nil, invalid("cannot void a paid invoice")
	}
	if updatedBy == "" {
		updatedBy = "system"
	}
	return s.repo.UpdateInvoiceStatus(ctx, id, tenantID, "cancelled", updatedBy)
}

func (s *Service) GetOutstandingInvoices(ctx context.Context, tenantID string) ([]*domain.Invoice, error) {
	if tenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	return s.repo.ListOutstandingInvoices(ctx, tenantID)
}
