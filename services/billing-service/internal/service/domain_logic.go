package service

import (
	"context"
	"errors"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/currency"
	"github.com/ppusapati/gavya/libs/integrity/exact"
	"github.com/ppusapati/gavya/libs/integrity/money"
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

// CreateInvoice takes the currency as an argument rather than a field on the
// invoice, because an invoice's amounts each carry their own currency now and
// there is nothing to put it in until they exist. A new invoice's totals are
// zero, and zero of what is the question this answers.
func (s *Service) CreateInvoice(ctx context.Context, inv *domain.Invoice, currencyCode string) (*domain.Invoice, error) {
	if inv.TenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if inv.CustomerID == "" {
		return nil, invalid("customer_id is required")
	}
	// An invoice states the currency it is in, and the first one for a tenant
	// fixes it. There is no default: a deployment outside India that silently
	// billed in rupees would produce invoices nobody could pay.
	code, err := currency.Normalise(currencyCode)
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
	// A new invoice has no lines, so its totals are zero — but zero rupees, not
	// a bare zero. The currency has to be on them from the start, or the first
	// line added would be adding to an amount that is not in any currency.
	inv.SubTotal = money.Zero(scale, code)
	inv.TaxAmount = money.Zero(scale, code)
	inv.TotalAmount = money.Zero(scale, code)

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
	denom, err := s.repo.TenantMoney(ctx, tenantID)
	if errors.Is(err, repository.ErrNotFound) {
		return repository.Money{}, invalid(
			"this tenant has not recorded any money yet; its first invoice must state a currency")
	}
	return denom, err
}

func (s *Service) AddInvoiceItem(ctx context.Context, item *domain.InvoiceItem, unitPriceLiteral string) (*repository.ItemOutcome, error) {
	if item.InvoiceID == "" || item.TenantID == "" {
		return nil, invalid("invoice_id and tenant_id are required")
	}
	if item.Quantity.Sign() <= 0 {
		return nil, invalid("quantity must be more than zero")
	}

	denom, err := s.moneyFor(ctx, item.TenantID)
	if err != nil {
		return nil, err
	}

	// The quantity and the tax rate arrive at whatever scale they were written
	// to and are held at their columns'. A value finer than the column is
	// refused rather than rounded into it in silence; a value wider than the
	// column is refused rather than reported as a constraint violation.
	if item.Quantity, err = item.Quantity.NonNegativeColumn(domain.QuantityScale, domain.QuantityPrecision); err != nil {
		return nil, invalid(exact.Field("quantity", err).Error())
	}
	// Prices are held to the currency's own precision: a yen price has no
	// decimals, a dinar price has three. Validating everything at two would
	// accept a yen price of 100.50 and refuse a legitimate dinar price.
	price, err := money.Parse(unitPriceLiteral, denom.Scale, denom.Code)
	if err != nil {
		return nil, invalid("unit_price: " + err.Error())
	}
	if price.Value < 0 {
		return nil, invalid("unit_price must not be negative")
	}
	item.UnitPrice = price
	// A tax rate is a percentage, not money, so its precision is its own.
	if item.TaxRate, err = item.TaxRate.NonNegativeColumn(domain.TaxRateScale, domain.TaxRatePrecision); err != nil {
		return nil, invalid(exact.Field("tax_rate", err).Error())
	}
	if c, _ := item.TaxRate.Cmp(domain.TaxRateCeiling); c >= 0 {
		return nil, invalid("tax_rate must be a percentage, and 1000% is not one")
	}

	item.ID = ulidpkg.New().String()
	if item.CreatedBy == "" {
		item.CreatedBy = "system"
	}
	item.UpdatedBy = item.CreatedBy

	return s.repo.AddItemAndRetotal(ctx, item, price.String(), denom)
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
func (s *Service) RecordPayment(ctx context.Context, p *domain.Payment, amountLiteral string) (*repository.PaymentOutcome, error) {
	if p.InvoiceID == "" || p.TenantID == "" {
		return nil, invalid("invoice_id and tenant_id are required")
	}
	denom, err := s.moneyFor(ctx, p.TenantID)
	if err != nil {
		return nil, err
	}
	amount, err := money.Parse(amountLiteral, denom.Scale, denom.Code)
	if err != nil {
		return nil, invalid("amount: " + err.Error())
	}
	if amount.Value <= 0 {
		return nil, invalid("a payment must be for more than zero")
	}
	p.Amount = amount

	p.ID = ulidpkg.New().String()
	if p.PaidAt.IsZero() {
		p.PaidAt = time.Now()
	}
	if p.CreatedBy == "" {
		p.CreatedBy = "system"
	}
	p.UpdatedBy = p.CreatedBy

	return s.repo.RecordPaymentAndSettle(ctx, p, amount.String(), denom)
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
