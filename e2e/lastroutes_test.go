//go:build e2e

// The last six routes, and the one of them that is not like the others.
//
// Five are ordinary reads and one write: the invoice an order is billed on, a
// return read back, a product and its SKUs, a notification. The sixth is
// SignInService, which is how one service proves to another who it is — the
// credential every service-to-service call in this platform ultimately rests
// on, and the only one of the six where being wrong is a security failure
// rather than a reporting one.
//
// That one has four refusals worth holding in place, and one of them is a
// design decision rather than a validation. A service identity issued across
// the whole platform — the settlement recomputer that reconciles every tenant —
// cannot open a session at all, because a session must name one tenant and
// every policy downstream compares against that value. Giving it an empty scope
// instead would read as "no rows" everywhere and look like a data problem.
package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ppusapati/gavya/libs/integrity/credential"
	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

type generateInvoiceReq struct {
	OrderID   string `json:"order_id"`
	TenantID  string `json:"tenant_id"`
	CreatedBy string `json:"created_by"`
}

type invoiceView struct {
	ID            string `json:"id"`
	TenantID      string `json:"tenant_id"`
	OrderID       string `json:"order_id"`
	InvoiceNumber string `json:"invoice_number"`
	Status        string `json:"status"`
	SubTotal      string `json:"sub_total"`
	TaxAmount     string `json:"tax_amount"`
	TotalAmount   string `json:"total_amount"`
	Currency      string `json:"currency"`
}

// fullInvoiceResp reads the totals, which erp_test's invoiceResp does not
// carry — and the totals are the reason an invoice exists.
type fullInvoiceResp struct {
	Invoice *invoiceView `json:"invoice"`
}

type returnView struct {
	ID           string `json:"id"`
	TenantID     string `json:"tenant_id"`
	OrderID      string `json:"order_id"`
	Reason       string `json:"reason"`
	Status       string `json:"status"`
	RefundAmount string `json:"refund_amount"`
	Currency     string `json:"currency"`
}

type getReturnResp struct {
	Return *returnView `json:"return"`
}

type productProto struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenant_id"`
	CategoryID  string `json:"category_id"`
	BrandID     string `json:"brand_id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	ProductType string `json:"product_type"`
	Status      string `json:"status"`
}

// fullProductResp reads the whole product; erp_test's productResp carries
// only the id, which is all its own tests need.
type fullProductResp struct {
	Product *productProto `json:"product"`
}

// listProductSKUsReq and listSKUsResp are declared in lifecycle_test.go and
// were never used: types written for a test nobody got round to, which is
// itself how this route came to have no coverage. They are used here.

type notificationProto struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenant_id"`
	Channel     string `json:"channel"`
	RecipientID string `json:"recipient_id"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	Status      string `json:"status"`
}

// fullNotificationResp reads the recipient and body, which is what makes a
// cross-tenant read of one a disclosure rather than a lookup.
type fullNotificationResp struct {
	Notification *notificationProto `json:"notification"`
}

// An order is invoiced for what it holds, and its return reads back.
//
// The invoice is the bill. Its totals are decimal literals at the currency's
// scale, not JSON numbers, and they must agree with the order they were
// generated from — an invoice that disagrees with its order is a document
// somebody is asked to pay against a figure the platform does not hold.
func TestAnOrderIsInvoicedForWhatItHoldsAndItsReturnReadsBack(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	orderID := anOrderWorth(t, p, "100.00")

	invoice, err := svcclient.Call[generateInvoiceReq, fullInvoiceResp](ctx, p.order(),
		orderSvc+"/GenerateInvoice", generateInvoiceReq{
			OrderID: orderID, TenantID: p.tenant, CreatedBy: "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("GenerateInvoice: %v", err)
	}
	inv := invoice.Invoice
	if inv == nil {
		t.Fatal("GenerateInvoice answered with no invoice at all")
	}
	if inv.OrderID != orderID {
		t.Errorf("the invoice is against order %s, want %s", inv.OrderID, orderID)
	}
	// One line at 100.00 with no tax. The invoice must bill that, not a figure
	// of its own: an invoice that disagrees with its order is a document
	// somebody is asked to pay against a number the platform does not hold.
	if inv.TotalAmount != "100.00" {
		t.Errorf("the invoice totals %q and the order holds one line at 100.00",
			inv.TotalAmount)
	}
	if inv.SubTotal != "100.00" || inv.TaxAmount != "0.00" {
		t.Errorf("the invoice breaks down as %q plus %q tax, want 100.00 plus 0.00",
			inv.SubTotal, inv.TaxAmount)
	}
	if inv.Currency != "INR" {
		t.Errorf("the invoice is in %q, want INR", inv.Currency)
	}
	if inv.InvoiceNumber == "" {
		t.Error("the invoice has no number, so nothing on paper points back to this row")
	}

	// A return against the same order, read back by its own route.
	requested, err := svcclient.Call[requestReturnReq, getReturnResp](ctx, p.order(),
		orderSvc+"/RequestReturn", requestReturnReq{
			TenantID: p.tenant, OrderID: orderID,
			Reason: "two crates arrived warm", RefundAmount: "40.00",
			CreatedBy: "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("RequestReturn: %v", err)
	}

	got, err := svcclient.Call[idTenantReq, getReturnResp](ctx, p.order(),
		orderSvc+"/GetReturn", idTenantReq{ID: requested.Return.ID, TenantID: p.tenant},
		p.opts())
	if err != nil {
		t.Fatalf("GetReturn: %v", err)
	}
	r := got.Return
	if r.ID != requested.Return.ID || r.OrderID != orderID {
		t.Errorf("the return reads back as %+v, want the one just requested", r)
	}
	if r.Reason != "two crates arrived warm" {
		t.Errorf("the return's reason reads back as %q", r.Reason)
	}
	if r.Status != "requested" {
		t.Errorf("a return nobody has acted on is %q, want requested", r.Status)
	}

	stranger := newID("ten")
	if _, err := svcclient.Call[idTenantReq, getReturnResp](ctx, p.order(),
		orderSvc+"/GetReturn", idTenantReq{ID: requested.Return.ID, TenantID: stranger},
		svcclient.CallOptions{Tenant: stranger, Actor: "e2e", RequestID: newID("req")}); err == nil {
		t.Error("another tenant read this return, which names what a customer is owed")
	}
}

// A product reads back, and its SKUs are its own.
//
// Two products in the same catalogue, each with a SKU. The SKU is what carries
// the price, so a listing that returned another product's SKUs would price one
// thing at another's rate — and the response would look entirely ordinary.
func TestAProductReadsBackAndItsSKUsAreItsOwn(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	tenant := newID("ten")
	opts := svcclient.CallOptions{Tenant: tenant, Actor: "e2e", RequestID: newID("req")}

	code := newID("cat")
	cat, err := svcclient.Call[createCategoryReq, categoryResp](ctx, p.catalog(),
		catalogSvc+"/CreateCategory", createCategoryReq{
			TenantID: tenant, Name: code, Slug: code, CreatedBy: "e2e",
		}, opts)
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	category := cat.Category.ID

	product := func(name string) string {
		t.Helper()
		out, err := svcclient.Call[createProductReq, fullProductResp](ctx, p.catalog(),
			catalogSvc+"/CreateProduct", createProductReq{
				TenantID: tenant, CategoryID: category,
				Name: name, Slug: newID("sl"), ProductType: "simple",
				Status: "active", CreatedBy: "e2e",
			}, opts)
		if err != nil {
			t.Fatalf("CreateProduct %s: %v", name, err)
		}
		return out.Product.ID
	}

	sku := func(productID, code, price string) string {
		t.Helper()
		out, err := svcclient.Call[createSKUReq, skuResp](ctx, p.catalog(),
			catalogSvc+"/CreateSKU", createSKUReq{
				TenantID: tenant, ProductID: productID, Code: code,
				Name: code, Price: price, Currency: "INR",
				Unit: "kg", UnitSize: 50, Status: "active", CreatedBy: "e2e",
			}, opts)
		if err != nil {
			t.Fatalf("CreateSKU %s: %v", code, err)
		}
		return out.SKU.ID
	}

	mine := product("cottonseed cake")
	theirs := product("maize silage")
	mySKU := sku(mine, newID("sk"), "1250.00")
	theirSKU := sku(theirs, newID("sk"), "480.00")

	got, err := svcclient.Call[idTenantReq, fullProductResp](ctx, p.catalog(),
		catalogSvc+"/GetProduct", idTenantReq{ID: mine, TenantID: tenant}, opts)
	if err != nil {
		t.Fatalf("GetProduct: %v", err)
	}
	if got.Product.ID != mine || got.Product.Name != "cottonseed cake" {
		t.Errorf("the product reads back as %+v, want the cottonseed cake", got.Product)
	}
	if got.Product.CategoryID != category {
		t.Errorf("the product reads back under category %s, want %s",
			got.Product.CategoryID, category)
	}

	listed, err := svcclient.Call[listProductSKUsReq, listSKUsResp](ctx, p.catalog(),
		catalogSvc+"/ListProductSKUs", listProductSKUsReq{ProductID: mine, TenantID: tenant}, opts)
	if err != nil {
		t.Fatalf("ListProductSKUs: %v", err)
	}
	if len(listed.SKUs) != 1 {
		t.Fatalf("this product has %d SKUs, want 1 — another product was created "+
			"in the same moment and its SKU is not this product's", len(listed.SKUs))
	}
	if listed.SKUs[0].ID != mySKU {
		t.Errorf("the listing holds SKU %s, want %s", listed.SKUs[0].ID, mySKU)
	}
	if listed.SKUs[0].Price != "1250.00" {
		t.Errorf("the SKU prices at %q, want 1250.00 — the other product's SKU is "+
			"480.00, and pricing one thing at another's rate is the failure this "+
			"listing can hide", listed.SKUs[0].Price)
	}
	_ = theirSKU
}

// A notification reads back to the tenant it was sent for.
//
// A notification carries a recipient's address and the message sent to them, so
// reading one is reading somebody's correspondence.
func TestANotificationReadsBackToItsOwnTenant(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	sent, err := svcclient.Call[sendNotificationReq, fullNotificationResp](ctx, p.notification(),
		notifySvc+"/SendNotification", sendNotificationReq{
			TenantID: p.tenant, RecipientID: "+919000000000",
			RecipientType: "producer", Channel: "sms",
			Title: "collection receipt", Body: "12.5 litres at 06:14",
			Priority: "normal", CreatedBy: "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("SendNotification: %v", err)
	}

	got, err := svcclient.Call[idTenantReq, fullNotificationResp](ctx, p.notification(),
		notifySvc+"/GetNotification", idTenantReq{
			ID: sent.Notification.ID, TenantID: p.tenant,
		}, p.opts())
	if err != nil {
		t.Fatalf("GetNotification: %v", err)
	}
	n := got.Notification
	if n.ID != sent.Notification.ID {
		t.Errorf("GetNotification asked for %s and answered with %s",
			sent.Notification.ID, n.ID)
	}
	if n.RecipientID != "+919000000000" || n.Body != "12.5 litres at 06:14" {
		t.Errorf("the notification reads back as %+v, losing its recipient or body", n)
	}
	if n.Channel != "sms" {
		t.Errorf("the notification's channel reads back as %q, want sms", n.Channel)
	}

	stranger := newID("ten")
	if _, err := svcclient.Call[idTenantReq, fullNotificationResp](ctx, p.notification(),
		notifySvc+"/GetNotification", idTenantReq{ID: sent.Notification.ID, TenantID: stranger},
		svcclient.CallOptions{Tenant: stranger, Actor: "e2e", RequestID: newID("req")}); err == nil {
		t.Error("another tenant read this notification, which carries a producer's " +
			"telephone number and the message sent to it")
	}
}

// seedServiceIdentity puts one service credential in the database, hashed the
// way the service hashes it.
//
// There is no route that creates these on purpose: a service credential is
// issued by whoever runs the deployment, not by the platform's own API, so the
// only honest way to set one up in a test is the way an operator would.
func seedServiceIdentity(t *testing.T, owner *pgx.Conn, name, secret, tenant, status string, expires time.Time) string {
	t.Helper()
	cheap := credential.Params{Memory: 8 * 1024, Time: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	hash, err := credential.HashWith(secret, cheap)
	if err != nil {
		t.Fatal(err)
	}
	id := newID("SI")
	var tenantArg any
	if tenant != "" {
		tenantArg = tenant
	}
	if _, err := owner.Exec(context.Background(),
		`INSERT INTO service_identities
		   (id,tenant_id,name,secret_hash,status,expires_at,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,'seed','seed')`,
		id, tenantArg, name, hash, status, expires); err != nil {
		t.Fatalf("seed service identity %s: %v", name, err)
	}
	return id
}

// One service proves who it is to another, and four kinds of credential are
// refused.
//
// This is the bottom of the platform's trust: every service-to-service call
// carries a session opened here. The four refusals are the ones that matter,
// and the last is a design decision rather than a validation — a credential
// issued across the whole platform cannot open a session, because a session
// names one tenant and every policy downstream compares against that value.
func TestAServiceSignsInAndFourKindsOfCredentialAreRefused(t *testing.T) {
	base, owner := identityAt(t)

	future := time.Now().UTC().AddDate(1, 0, 0)
	good := newID("svc")
	seedServiceIdentity(t, owner, good, "a service secret", alpha, "active", future)
	seedServiceIdentity(t, owner, "revoked-"+good, "a service secret", alpha, "revoked", future)
	seedServiceIdentity(t, owner, "expired-"+good, "a service secret", alpha, "active",
		time.Now().UTC().AddDate(0, 0, -1))
	// No tenant: the recomputer that reconciles every tenant.
	seedServiceIdentity(t, owner, "platform-"+good, "a service secret", "", "active", future)

	code, body := call(t, base, "SignInService", map[string]string{
		"name": good, "secret": "a service secret",
	})
	if code != 200 {
		t.Fatalf("a valid service credential was refused: %d %v", code, body)
	}
	if body["session_id"] == nil || body["session_id"] == "" {
		t.Error("the sign-in succeeded and returned no session id")
	}
	if body["tenant_id"] != alpha {
		t.Errorf("the session is for tenant %v, want %s — the session's tenant is "+
			"what every policy downstream compares against", body["tenant_id"], alpha)
	}
	if body["expires_at"] == nil || body["expires_at"] == "" {
		t.Error("the session has no expiry, so it is a credential that never ends")
	}

	for _, c := range []struct {
		what, name, secret string
	}{
		{"a wrong secret", good, "not the secret"},
		{"a revoked identity", "revoked-" + good, "a service secret"},
		{"an expired credential", "expired-" + good, "a service secret"},
		{"a name nobody issued", "no-such-service-" + good, "a service secret"},
		{"a platform-wide identity with no tenant", "platform-" + good, "a service secret"},
		{"an empty secret", good, ""},
	} {
		code, body := call(t, base, "SignInService", map[string]string{
			"name": c.name, "secret": c.secret,
		})
		if code == 200 {
			t.Errorf("%s opened a session: %v", c.what, body)
		}
	}

	// The refusals do not distinguish a wrong secret from an unknown name.
	// Telling them apart turns the sign-in route into a list of which services
	// exist.
	_, wrongSecret := call(t, base, "SignInService", map[string]string{
		"name": good, "secret": "not the secret",
	})
	_, unknownName := call(t, base, "SignInService", map[string]string{
		"name": "no-such-service-" + good, "secret": "a service secret",
	})
	// Both must actually say something: comparing "" against "" would pass
	// however the service behaved.
	if wrongSecret["message"] == nil || wrongSecret["message"] == "" {
		t.Fatalf("a refused sign-in carried no message: %v", wrongSecret)
	}
	if wrongSecret["message"] != unknownName["message"] {
		t.Errorf("a wrong secret is refused with %q and an unknown name with %q\n"+
			"Distinguishing them turns this route into a list of which services "+
			"exist and which secrets are close.",
			wrongSecret["message"], unknownName["message"])
	}
}
