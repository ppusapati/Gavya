package service

import (
	"context"
	"errors"
	"time"

	"github.com/ppusapati/gavya/services/billing-service/internal/domain"
	ulidpkg "p9e.in/samavaya/packages/ULID"
)

func (s *Service) CreateInvoice(ctx context.Context, inv *domain.Invoice) (*domain.Invoice, error) {
	if inv.TenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if inv.CustomerID == "" {
		return nil, errors.New("customer_id is required")
	}
	inv.ID = ulidpkg.New().String()
	inv.InvoiceNumber = "INV-" + ulidpkg.New().String()
	if inv.Status == "" {
		inv.Status = "draft"
	}
	if inv.Currency == "" {
		inv.Currency = "INR"
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

func (s *Service) AddInvoiceItem(ctx context.Context, item *domain.InvoiceItem) (*domain.InvoiceItem, error) {
	if item.InvoiceID == "" || item.TenantID == "" {
		return nil, errors.New("invoice_id and tenant_id are required")
	}
	inv, err := s.repo.GetInvoice(ctx, item.InvoiceID, item.TenantID)
	if err != nil {
		return nil, err
	}
	if inv.Status != "draft" {
		return nil, errors.New("can only add items to draft invoices")
	}
	item.ID = ulidpkg.New().String()
	item.TotalPrice = item.Quantity * item.UnitPrice
	if item.CreatedBy == "" {
		item.CreatedBy = "system"
	}
	item.UpdatedBy = item.CreatedBy
	result, err := s.repo.CreateInvoiceItem(ctx, item)
	if err != nil {
		return nil, err
	}
	// recalculate totals
	subTotal, err := s.repo.SumInvoiceItems(ctx, item.InvoiceID, item.TenantID)
	if err == nil {
		taxAmount := subTotal * 0.18
		totalAmount := subTotal + taxAmount
		if _, err := s.repo.UpdateInvoiceTotals(ctx, item.InvoiceID, item.TenantID, subTotal, taxAmount, totalAmount, item.CreatedBy); err != nil {
			s.log.Errorf("failed to update invoice totals: %v", err)
		}
	}
	return result, nil
}

func (s *Service) SendInvoice(ctx context.Context, id, tenantID, updatedBy string) (*domain.Invoice, error) {
	inv, err := s.repo.GetInvoice(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}
	if inv.Status != "draft" {
		return nil, errors.New("can only send draft invoices")
	}
	if updatedBy == "" {
		updatedBy = "system"
	}
	return s.repo.UpdateInvoiceStatus(ctx, id, tenantID, "sent", updatedBy)
}

func (s *Service) RecordPayment(ctx context.Context, p *domain.Payment) (*domain.Payment, error) {
	if p.InvoiceID == "" || p.TenantID == "" {
		return nil, errors.New("invoice_id and tenant_id are required")
	}
	p.ID = ulidpkg.New().String()
	if p.Currency == "" {
		p.Currency = "INR"
	}
	if p.PaidAt.IsZero() {
		p.PaidAt = time.Now()
	}
	if p.CreatedBy == "" {
		p.CreatedBy = "system"
	}
	p.UpdatedBy = p.CreatedBy
	result, err := s.repo.CreatePayment(ctx, p)
	if err != nil {
		return nil, err
	}
	// Check if fully paid
	inv, err := s.repo.GetInvoice(ctx, p.InvoiceID, p.TenantID)
	if err != nil {
		return result, nil
	}
	paidTotal, err := s.repo.SumPayments(ctx, p.InvoiceID, p.TenantID)
	if err == nil && paidTotal >= inv.TotalAmount {
		if _, err := s.repo.MarkInvoicePaid(ctx, p.InvoiceID, p.TenantID, p.CreatedBy, p.PaidAt); err != nil {
			s.log.Errorf("failed to mark invoice paid: %v", err)
		}
	}
	return result, nil
}

func (s *Service) VoidInvoice(ctx context.Context, id, tenantID, updatedBy string) (*domain.Invoice, error) {
	inv, err := s.repo.GetInvoice(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}
	if inv.Status == "paid" {
		return nil, errors.New("cannot void a paid invoice")
	}
	if updatedBy == "" {
		updatedBy = "system"
	}
	return s.repo.UpdateInvoiceStatus(ctx, id, tenantID, "cancelled", updatedBy)
}

func (s *Service) GetOutstandingInvoices(ctx context.Context, tenantID string) ([]*domain.Invoice, error) {
	if tenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	return s.repo.ListOutstandingInvoices(ctx, tenantID)
}
