//go:build e2e

// Money that goes in comes back out unchanged.
//
// order-service and billing-service already did their arithmetic in the
// database, in the columns' own NUMERIC type — a line total multiplied in
// float64 and stored into a NUMERIC column writes a rounded version of a number
// that was already slightly wrong, and on an invoice that is what a customer is
// asked to pay. What they still did was read the result back into float64, which
// carries a four-decimal value exactly only up to about 10^11. So the exactness
// stopped at the edge of the database.
//
// These tests take the whole path: a decimal literal in the request, NUMERIC in
// the column, arithmetic in SQL, and a decimal literal in the reply. Nothing in
// between is a float.
//
// Neither service had any end-to-end coverage of a line item or a payment at
// all. The e2e suite created an order and an invoice, moved their statuses, and
// stopped — which is to say it never once checked that either service can add up
// what it is for.
package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

const (
	orderSvc   = "order.v1.OrderService"
	billingSvc = "billing.v1.BillingService"
	healthSvc  = "health.v1.HealthService"
)

type addOrderItemReq struct {
	TenantID  string  `json:"tenant_id"`
	OrderID   string  `json:"order_id"`
	SKUID     string  `json:"sku_id"`
	ProductID string  `json:"product_id"`
	Quantity  float64 `json:"quantity"`
	UnitPrice string  `json:"unit_price"`
	TaxRate   float64 `json:"tax_rate"`
	CreatedBy string  `json:"created_by"`
}

type orderItemResp struct {
	Item *struct {
		ID         string `json:"id"`
		UnitPrice  string `json:"unit_price"`
		TotalPrice string `json:"total_price"`
		Currency   string `json:"currency"`
	} `json:"item"`
	Order *struct {
		ID          string `json:"id"`
		SubTotal    string `json:"sub_total"`
		TaxAmount   string `json:"tax_amount"`
		TotalAmount string `json:"total_amount"`
		Currency    string `json:"currency"`
	} `json:"order"`
}

type addInvoiceItemReq struct {
	TenantID    string  `json:"tenant_id"`
	InvoiceID   string  `json:"invoice_id"`
	Description string  `json:"description"`
	Quantity    float64 `json:"quantity"`
	UnitPrice   string  `json:"unit_price"`
	TaxRate     float64 `json:"tax_rate"`
	CreatedBy   string  `json:"created_by"`
}

type invoiceItemResp struct {
	Item *struct {
		ID         string `json:"id"`
		TotalPrice string `json:"total_price"`
		Currency   string `json:"currency"`
	} `json:"item"`
	Invoice *invoiceTotals `json:"invoice"`
}

type invoiceTotals struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	SubTotal    string `json:"sub_total"`
	TaxAmount   string `json:"tax_amount"`
	TotalAmount string `json:"total_amount"`
	Currency    string `json:"currency"`
}

type recordPaymentReq struct {
	TenantID      string    `json:"tenant_id"`
	InvoiceID     string    `json:"invoice_id"`
	Amount        string    `json:"amount"`
	PaymentMethod string    `json:"payment_method"`
	ReferenceNo   string    `json:"reference_no"`
	PaidAt        time.Time `json:"paid_at"`
	CreatedBy     string    `json:"created_by"`
}

type paymentResp struct {
	Payment *struct {
		ID       string `json:"id"`
		Amount   string `json:"amount"`
		Currency string `json:"currency"`
	} `json:"payment"`
	Invoice *invoiceTotals `json:"invoice"`
}

type scheduleVetVisitReq struct {
	TenantID       string    `json:"tenant_id"`
	CattleID       string    `json:"cattle_id"`
	VeterinarianID string    `json:"veterinarian_id"`
	VisitDate      time.Time `json:"visit_date"`
	Purpose        string    `json:"purpose"`
	Notes          string    `json:"notes"`
	Cost           string    `json:"cost"`
	Currency       string    `json:"currency"`
	CreatedBy      string    `json:"created_by"`
}

type vetVisitResp struct {
	VetVisit *struct {
		ID       string `json:"id"`
		Cost     string `json:"cost"`
		Currency string `json:"currency"`
	} `json:"vet_visit"`
}

// An order's totals are the sum of its lines, exactly.
//
// The figures here are chosen so a float64 would show: 0.1 + 0.2 is famously not
// 0.3 in binary, and three lines of 19.99 is 59.97 only in decimal. The tax is
// eighteen per cent, which is India's standard GST rate on most processed dairy.
func TestAnOrdersTotalsAreExact(t *testing.T) {
	p := startPlatform(t)

	order, err := svcclient.Call[createOrderReq, orderRespProto](
		context.Background(), p.order(), orderSvc+"/CreateOrder",
		createOrderReq{TenantID: p.tenant, CustomerID: newID("cus"),
			Currency: "INR", CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("create order: %v", err)
	}

	// Three lines whose decimal sum is exact and whose binary sum is not.
	lines := []struct {
		quantity  float64
		unitPrice string
		want      string // the running sub-total after this line
	}{
		{3, "19.99", "59.97"},
		{1, "0.10", "60.07"},
		{1, "0.20", "60.27"},
	}
	var last *orderItemResp
	for i, l := range lines {
		out, err := svcclient.Call[addOrderItemReq, orderItemResp](
			context.Background(), p.order(), orderSvc+"/AddOrderItem",
			addOrderItemReq{TenantID: p.tenant, OrderID: order.Order.ID,
				SKUID: newID("sku"), ProductID: newID("prd"),
				Quantity: l.quantity, UnitPrice: l.unitPrice, TaxRate: 18,
				CreatedBy: "e2e"}, p.opts())
		if err != nil {
			t.Fatalf("add line %d: %v", i, err)
		}
		if out.Order.SubTotal != l.want {
			t.Fatalf("after line %d the sub total is %s, want %s — the order no longer "+
				"agrees with the sum of its own lines", i, out.Order.SubTotal, l.want)
		}
		if out.Item.Currency != "INR" || out.Order.Currency != "INR" {
			t.Errorf("line %d came back in %s / order in %s, want INR",
				i, out.Item.Currency, out.Order.Currency)
		}
		last = out
	}

	// Eighteen per cent of 60.27. Per line that is 10.79 + 0.02 + 0.04; on the
	// total it is 10.8486 rounded to 10.85. Both come to 10.85, so this figure
	// does not distinguish the two — TestTaxIsRoundedPerLineNotOnTheTotal is
	// what pins that.
	if last.Order.TaxAmount != "10.85" {
		t.Errorf("tax = %s, want 10.85", last.Order.TaxAmount)
	}
	if last.Order.TotalAmount != "71.12" {
		t.Errorf("total = %s, want 71.12", last.Order.TotalAmount)
	}
}

// Tax is worked out per line and rounded there, not on the order's total.
//
// It has to be, because lines carry their own rates: a dairy catalogue mixes
// exempt and rated goods, and one rate applied to a whole order would tax the
// exempt half. The consequence is that the tax on an order is the sum of
// rounded line taxes rather than the rounded tax on a sum, and the two are not
// always the same figure.
//
// Three lines of 0.10 at eighteen per cent is where they part: 0.018 rounds to
// 0.02 on each line, giving 0.06, while 0.30 at eighteen per cent is 0.054 and
// rounds to 0.05. A penny, and it is the kind of penny that makes an invoice
// disagree with the lines printed on it.
func TestTaxIsRoundedPerLineNotOnTheTotal(t *testing.T) {
	p := startPlatform(t)

	order, err := svcclient.Call[createOrderReq, orderRespProto](
		context.Background(), p.order(), orderSvc+"/CreateOrder",
		createOrderReq{TenantID: p.tenant, CustomerID: newID("cus"),
			Currency: "INR", CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("create order: %v", err)
	}

	var last *orderItemResp
	for i := 0; i < 3; i++ {
		out, err := svcclient.Call[addOrderItemReq, orderItemResp](
			context.Background(), p.order(), orderSvc+"/AddOrderItem",
			addOrderItemReq{TenantID: p.tenant, OrderID: order.Order.ID,
				SKUID: newID("sku"), ProductID: newID("prd"),
				Quantity: 1, UnitPrice: "0.10", TaxRate: 18,
				CreatedBy: "e2e"}, p.opts())
		if err != nil {
			t.Fatalf("add line %d: %v", i, err)
		}
		last = out
	}

	if last.Order.SubTotal != "0.30" {
		t.Fatalf("sub total = %s, want 0.30", last.Order.SubTotal)
	}
	if last.Order.TaxAmount != "0.06" {
		t.Errorf("tax = %s, want 0.06 — three lines of 0.018 each rounded to 0.02. "+
			"0.05 would mean the tax was worked out on the order's total instead, "+
			"which cannot be right for an order whose lines carry different rates",
			last.Order.TaxAmount)
	}
	if last.Order.TotalAmount != "0.36" {
		t.Errorf("total = %s, want 0.36", last.Order.TotalAmount)
	}
}

// An invoice is settled when the payments against it cover it, and not before.
//
// The amounts are picked so the last payment lands exactly on the total. Whether
// an invoice is covered used to be decided by comparing two float64 values read
// out of NUMERIC columns, which is the one comparison where being out by a
// hundredth means a customer is chased for a bill they have paid.
func TestAnInvoiceIsSettledExactlyWhenItIsCovered(t *testing.T) {
	p := startPlatform(t)

	inv, err := svcclient.Call[createInvoiceReq, invoiceResp](
		context.Background(), p.billing(), billingSvc+"/CreateInvoice",
		createInvoiceReq{TenantID: p.tenant, CustomerID: newID("cus"),
			Currency: "INR", IssuedAt: time.Now(), CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	line, err := svcclient.Call[addInvoiceItemReq, invoiceItemResp](
		context.Background(), p.billing(), billingSvc+"/AddInvoiceItem",
		addInvoiceItemReq{TenantID: p.tenant, InvoiceID: inv.Invoice.ID,
			Description: "1L toned milk x 33", Quantity: 33, UnitPrice: "3.03",
			TaxRate: 5, CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("add invoice line: %v", err)
	}
	// 33 * 3.03 is exactly 99.99 in decimal. In float64 it is 99.99000000000001.
	if line.Item.TotalPrice != "99.99" {
		t.Errorf("line total = %s, want 99.99 — 33 x 3.03 is exact in decimal and is not "+
			"in binary", line.Item.TotalPrice)
	}
	// Five per cent of 99.99 is 4.9995, which rounds to 5.00.
	if line.Invoice.TaxAmount != "5.00" {
		t.Errorf("tax = %s, want 5.00", line.Invoice.TaxAmount)
	}
	if line.Invoice.TotalAmount != "104.99" {
		t.Fatalf("total = %s, want 104.99", line.Invoice.TotalAmount)
	}

	// A part payment leaves it unsettled. One paisa short is still short.
	part, err := svcclient.Call[recordPaymentReq, paymentResp](
		context.Background(), p.billing(), billingSvc+"/RecordPayment",
		recordPaymentReq{TenantID: p.tenant, InvoiceID: inv.Invoice.ID,
			Amount: "104.98", PaymentMethod: "upi", PaidAt: time.Now(),
			CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("record part payment: %v", err)
	}
	if part.Payment.Amount != "104.98" || part.Payment.Currency != "INR" {
		t.Errorf("payment came back as %s %s, want 104.98 INR",
			part.Payment.Amount, part.Payment.Currency)
	}
	if part.Invoice.Status == "paid" {
		t.Fatalf("an invoice for 104.99 was marked paid by 104.98; a hundredth short is "+
			"short, and this is the comparison that decides whether a customer is chased "+
			"for a bill they have settled (status=%s)", part.Invoice.Status)
	}

	// And the paisa settles it.
	rest, err := svcclient.Call[recordPaymentReq, paymentResp](
		context.Background(), p.billing(), billingSvc+"/RecordPayment",
		recordPaymentReq{TenantID: p.tenant, InvoiceID: inv.Invoice.ID,
			Amount: "0.01", PaymentMethod: "upi", PaidAt: time.Now(),
			CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("record final payment: %v", err)
	}
	if rest.Invoice.Status != "paid" {
		t.Errorf("after 104.98 + 0.01 against a total of 104.99 the invoice is %s, want "+
			"paid — the payments cover it exactly", rest.Invoice.Status)
	}
}

// A price finer than its currency is refused rather than rounded.
//
// A rupee has two decimals. The column holds four, so one schema serves a dinar
// deployment too, and PostgreSQL would round a third decimal into it without a
// word — leaving the service reporting one figure while the database held
// another.
func TestAnOrderLinePriceFinerThanTheCurrencyIsRefused(t *testing.T) {
	p := startPlatform(t)

	order, err := svcclient.Call[createOrderReq, orderRespProto](
		context.Background(), p.order(), orderSvc+"/CreateOrder",
		createOrderReq{TenantID: p.tenant, CustomerID: newID("cus"),
			Currency: "INR", CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("create order: %v", err)
	}

	if _, err := svcclient.Call[addOrderItemReq, orderItemResp](
		context.Background(), p.order(), orderSvc+"/AddOrderItem",
		addOrderItemReq{TenantID: p.tenant, OrderID: order.Order.ID,
			SKUID: newID("sku"), ProductID: newID("prd"),
			Quantity: 1, UnitPrice: "19.995", TaxRate: 0,
			CreatedBy: "e2e"}, p.opts()); err == nil {
		t.Error("a three-decimal unit price was accepted in rupees")
	}
}

// A vet visit's cost survives the round trip, in the currency it was recorded in.
//
// health-service was the last of the five to read a money column into a float64,
// and its cost column is the smallest money surface in the platform: one field,
// one endpoint, no arithmetic. It had no end-to-end coverage of either — which is
// how it stayed the exception long enough to need mentioning in two documents.
func TestAVetVisitsCostIsExact(t *testing.T) {
	p := startPlatform(t)

	// A three-decimal currency, so the column's fourth decimal is padding and
	// the third is real. Reading this back at two decimals would drop a digit
	// the currency has.
	visit, err := svcclient.Call[scheduleVetVisitReq, vetVisitResp](
		context.Background(), p.health(), healthSvc+"/ScheduleVetVisit",
		scheduleVetVisitReq{TenantID: p.tenant, CattleID: newID("cow"),
			VeterinarianID: newID("vet"), VisitDate: time.Now(),
			Purpose: "mastitis follow-up", Cost: "412.375", Currency: "BHD",
			CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("schedule vet visit: %v", err)
	}
	if visit.VetVisit.Cost != "412.375" || visit.VetVisit.Currency != "BHD" {
		t.Errorf("cost came back as %s %s, want 412.375 BHD",
			visit.VetVisit.Cost, visit.VetVisit.Currency)
	}

	// A fourth decimal is refused, in a currency that has three. The column
	// would hold it and round nothing; the currency cannot express it.
	if _, err := svcclient.Call[scheduleVetVisitReq, vetVisitResp](
		context.Background(), p.health(), healthSvc+"/ScheduleVetVisit",
		scheduleVetVisitReq{TenantID: p.tenant, CattleID: newID("cow"),
			VeterinarianID: newID("vet"), VisitDate: time.Now(),
			Purpose: "e2e", Cost: "412.3755", Currency: "BHD",
			CreatedBy: "e2e"}, p.opts()); err == nil {
		t.Error("a four-decimal cost was accepted for a three-decimal currency")
	}
}
