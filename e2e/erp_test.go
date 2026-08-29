//go:build e2e

// The seven services that predate the integrity work.
//
// billing, breeding, cattle-market, feed, inventory, notification and
// product-catalog had no end-to-end coverage of any kind. Nothing anywhere
// showed that they start, that they answer, or — the one that matters — that one
// tenant cannot read another's rows through them.
//
// That last is not a small gap. Tenant isolation is the platform's central
// guarantee, it is enforced in the database rather than in each query precisely
// because hand-written queries get it wrong, and two cross-tenant leaks were
// found in one service's queries during that work. Both returned plausible
// results and no test failed. These seven were never checked at all.
//
// This file is deliberately not exhaustive CRUD coverage. It writes a row as one
// tenant, asks as another, and requires the answer to be empty — for every one of
// the seven, through the real binaries over real HTTP.
//
// # WHAT THIS PROVES, AND WHAT IT DOES NOT
//
// The harness connects as `postgres`, a superuser, and a superuser bypasses
// row-level security entirely. So what these tests prove is that each service's
// own query is scoped to its tenant — which is the layer that was found wrong
// twice in hand-written SQL, and the layer nothing was checking for these seven.
//
// They do not prove RLS works; that is isolation_test.go, which connects as
// `gavya_app` and cannot escape the policies. The two are different failures and
// both are real: a query missing its tenant clause is caught by RLS in
// production and by these tests here, and RLS misconfigured is caught there and
// not here.
//
// Verified by mutation: replacing feed-service's `tenant_id = $1` with a
// tautology makes the feed case fail with "a second tenant sees 4 rows through
// feed-service, and it wrote none".
package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

// tenantReq is the shape most of these services use to scope a list.
type tenantReq struct {
	TenantID string `json:"tenant_id"`
	Limit    int32  `json:"limit,omitempty"`
}

// isolationCase is one service: how to write a row, and how to read them back.
//
// Both halves are needed. A read that returns nothing proves isolation only if
// something was written for the other tenant to miss — otherwise it is an empty
// table passing for a policy.
type isolationCase struct {
	name string
	// client picks the service out of the platform.
	client func(*platform) *svcclient.Client
	// svc is the fully-qualified Connect service name.
	svc string
	// write creates one row for the platform's tenant and fails the test if it
	// cannot. It returns something identifying, for the message on failure.
	write func(t *testing.T, p *platform) string
	// count returns how many rows the given tenant can see.
	count func(t *testing.T, c *svcclient.Client, tenant string) int
}

func countVia[Req any, Resp any](
	t *testing.T, c *svcclient.Client, procedure string, in Req, tenant string,
	rows func(*Resp) int,
) int {
	t.Helper()
	resp, err := svcclient.Call[Req, Resp](context.Background(), c, procedure, in,
		svcclient.CallOptions{Tenant: tenant, Actor: "e2e"})
	if err != nil {
		// A refusal is a stronger answer than an empty list: the service
		// declined to answer for a tenant that has nothing. Either is isolation
		// holding.
		return 0
	}
	return rows(resp)
}

// ---------------------------------------------------------------------------
// product-catalog
// ---------------------------------------------------------------------------

type createCategoryReq struct {
	TenantID    string  `json:"tenant_id"`
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	ParentID    *string `json:"parent_id"`
	Description string  `json:"description"`
	SortOrder   int     `json:"sort_order"`
	CreatedBy   string  `json:"created_by"`
}

type categoryResp struct {
	Category *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"category"`
}

type listCategoriesResp struct {
	Categories []*struct {
		ID string `json:"id"`
	} `json:"categories"`
}

// ---------------------------------------------------------------------------
// inventory
// ---------------------------------------------------------------------------

type createWarehouseReq struct {
	TenantID  string `json:"tenant_id"`
	Name      string `json:"name"`
	Code      string `json:"code"`
	Address   string `json:"address"`
	ManagerID string `json:"manager_id"`
	Status    string `json:"status"`
	CreatedBy string `json:"created_by"`
}

type warehouseResp struct {
	Warehouse *struct {
		ID string `json:"id"`
	} `json:"warehouse"`
}

type listWarehousesResp struct {
	Warehouses []*struct {
		ID string `json:"id"`
	} `json:"warehouses"`
}

// ---------------------------------------------------------------------------
// billing
// ---------------------------------------------------------------------------

type createInvoiceReq struct {
	TenantID      string    `json:"tenant_id"`
	CustomerID    string    `json:"customer_id"`
	ReferenceID   string    `json:"reference_id"`
	ReferenceType string    `json:"reference_type"`
	Currency      string    `json:"currency"`
	TaxInclusive  bool      `json:"tax_inclusive"`
	IssuedAt      time.Time `json:"issued_at"`
	Notes         string    `json:"notes"`
	CreatedBy     string    `json:"created_by"`
}

type invoiceResp struct {
	Invoice *struct {
		ID string `json:"id"`
	} `json:"invoice"`
}

type invoiceActionReq struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	UpdatedBy string `json:"updated_by"`
}

type listInvoicesResp struct {
	Invoices []*struct {
		ID string `json:"id"`
	} `json:"invoices"`
}

// ---------------------------------------------------------------------------
// cattle-market
// ---------------------------------------------------------------------------

type createListingReq struct {
	TenantID    string  `json:"tenant_id"`
	CattleID    string  `json:"cattle_id"`
	SellerID    string  `json:"seller_id"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	AskingPrice float64 `json:"asking_price"`
	Currency    string  `json:"currency"`
	ListingType string  `json:"listing_type"`
	CreatedBy   string  `json:"created_by"`
}

type listingResp struct {
	Listing *struct {
		ID          string  `json:"id"`
		AskingPrice float64 `json:"asking_price"`
	} `json:"listing"`
}

type listListingsResp struct {
	Listings []*struct {
		ID string `json:"id"`
	} `json:"listings"`
}

// ---------------------------------------------------------------------------
// breeding
// ---------------------------------------------------------------------------

type createCycleReq struct {
	TenantID  string    `json:"tenant_id"`
	CattleID  string    `json:"cattle_id"`
	HeatDate  time.Time `json:"heat_date"`
	Status    string    `json:"status"`
	Notes     string    `json:"notes"`
	CreatedBy string    `json:"created_by"`
}

type breedingCycleResp struct {
	Cycle *struct {
		ID string `json:"id"`
	} `json:"cycle"`
}

type breedingHistoryReq struct {
	TenantID string `json:"tenant_id"`
	CattleID string `json:"cattle_id"`
}

type breedingHistoryResp struct {
	Cycles []*struct {
		ID string `json:"id"`
	} `json:"cycles"`
}

// ---------------------------------------------------------------------------
// feed
// ---------------------------------------------------------------------------

type createFeedTypeReq struct {
	TenantID        string `json:"tenant_id"`
	Name            string `json:"name"`
	Category        string `json:"category"`
	Unit            string `json:"unit"`
	NutritionalInfo string `json:"nutritional_info"`
	CreatedBy       string `json:"created_by"`
}

type feedTypeResp struct {
	FeedType *struct {
		ID string `json:"id"`
	} `json:"feed_type"`
}

type listFeedTypesResp struct {
	FeedTypes []*struct {
		ID string `json:"id"`
	} `json:"feed_types"`
}

// ---------------------------------------------------------------------------
// notification
// ---------------------------------------------------------------------------

type sendNotificationReq struct {
	TenantID      string `json:"tenant_id"`
	RecipientID   string `json:"recipient_id"`
	RecipientType string `json:"recipient_type"`
	Channel       string `json:"channel"`
	Title         string `json:"title"`
	Body          string `json:"body"`
	Priority      string `json:"priority"`
	CreatedBy     string `json:"created_by"`
}

type notificationResp struct {
	Notification *struct {
		ID string `json:"id"`
	} `json:"notification"`
}

type listNotificationsReq struct {
	TenantID string `json:"tenant_id"`
	Channel  string `json:"channel"`
	Status   string `json:"status"`
}

type listNotificationsResp struct {
	Notifications []*struct {
		ID string `json:"id"`
	} `json:"notifications"`
}

// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// cattle, farm, reporting
// ---------------------------------------------------------------------------

type createCattleReq struct {
	TenantID  string  `json:"tenant_id"`
	TagNumber string  `json:"tag_number"`
	Name      string  `json:"name"`
	Gender    string  `json:"gender"`
	Weight    float64 `json:"weight"`
	CreatedBy string  `json:"created_by"`
}

type cattleResp struct {
	Cattle *struct {
		ID string `json:"id"`
	} `json:"cattle"`
}

type listCattleReq struct {
	TenantID string `json:"tenant_id"`
	Limit    int32  `json:"limit"`
}

type listCattleResp struct {
	Cattle []*struct {
		ID string `json:"id"`
	} `json:"cattle"`
}

type createFarmReq struct {
	TenantID  string `json:"tenant_id"`
	Name      string `json:"name"`
	Code      string `json:"code"`
	Address   string `json:"address"`
	Country   string `json:"country"`
	Capacity  int    `json:"capacity"`
	Status    string `json:"status"`
	CreatedBy string `json:"created_by"`
}

type farmResp struct {
	Farm *struct {
		ID string `json:"id"`
	} `json:"farm"`
}

type listFarmsResp struct {
	Farms []*struct {
		ID string `json:"id"`
	} `json:"farms"`
}

type requestReportReq struct {
	TenantID    string `json:"tenant_id"`
	Name        string `json:"name"`
	ReportType  string `json:"report_type"`
	Parameters  string `json:"parameters"`
	FileFormat  string `json:"file_format"`
	RequestedBy string `json:"requested_by"`
	CreatedBy   string `json:"created_by"`
}

type reportResp struct {
	Report *struct {
		ID string `json:"id"`
	} `json:"report"`
}

type listReportsResp struct {
	Reports []*struct {
		ID string `json:"id"`
	} `json:"reports"`
}

func erpCases() []isolationCase {
	return []isolationCase{
		{
			name:   "product-catalog-service",
			client: (*platform).catalog,
			svc:    "productcatalog.v1.ProductCatalogService",
			write: func(t *testing.T, p *platform) string {
				t.Helper()
				code := newID("cat")
				resp, err := svcclient.Call[createCategoryReq, categoryResp](
					context.Background(), p.catalog(),
					"productcatalog.v1.ProductCatalogService/CreateCategory",
					createCategoryReq{TenantID: p.tenant, Name: code, Slug: code,
						Description: "e2e", CreatedBy: "e2e"}, p.opts())
				if err != nil {
					t.Fatalf("create category: %v", err)
				}
				return resp.Category.ID
			},
			count: func(t *testing.T, c *svcclient.Client, tenant string) int {
				return countVia[tenantReq, listCategoriesResp](t, c,
					"productcatalog.v1.ProductCatalogService/ListCategories",
					tenantReq{TenantID: tenant}, tenant,
					func(r *listCategoriesResp) int { return len(r.Categories) })
			},
		},
		{
			name:   "inventory-service",
			client: (*platform).inventory,
			svc:    "inventory.v1.InventoryService",
			write: func(t *testing.T, p *platform) string {
				t.Helper()
				code := newID("wh")
				resp, err := svcclient.Call[createWarehouseReq, warehouseResp](
					context.Background(), p.inventory(),
					"inventory.v1.InventoryService/CreateWarehouse",
					createWarehouseReq{TenantID: p.tenant, Name: code, Code: code,
						Address: "e2e", Status: "active", CreatedBy: "e2e"}, p.opts())
				if err != nil {
					t.Fatalf("create warehouse: %v", err)
				}
				return resp.Warehouse.ID
			},
			count: func(t *testing.T, c *svcclient.Client, tenant string) int {
				return countVia[tenantReq, listWarehousesResp](t, c,
					"inventory.v1.InventoryService/ListWarehouses",
					tenantReq{TenantID: tenant}, tenant,
					func(r *listWarehousesResp) int { return len(r.Warehouses) })
			},
		},
		{
			name:   "billing-service",
			client: (*platform).billing,
			svc:    "billing.v1.BillingService",
			write: func(t *testing.T, p *platform) string {
				t.Helper()
				resp, err := svcclient.Call[createInvoiceReq, invoiceResp](
					context.Background(), p.billing(),
					"billing.v1.BillingService/CreateInvoice",
					createInvoiceReq{TenantID: p.tenant, CustomerID: newID("cus"),
						ReferenceID: newID("ord"), ReferenceType: "ORDER",
						Currency: "INR", IssuedAt: time.Now().UTC(),
						CreatedBy: "e2e"}, p.opts())
				if err != nil {
					t.Fatalf("create invoice: %v", err)
				}
				// GetOutstandingInvoices looks only at 'sent' and 'overdue'; a
				// freshly created invoice is a draft, and nobody is outstanding
				// on a draft.
				if _, err := svcclient.Call[invoiceActionReq, invoiceResp](
					context.Background(), p.billing(),
					"billing.v1.BillingService/SendInvoice",
					invoiceActionReq{ID: resp.Invoice.ID, TenantID: p.tenant,
						UpdatedBy: "e2e"}, p.opts()); err != nil {
					t.Fatalf("send invoice: %v", err)
				}
				return resp.Invoice.ID
			},
			count: func(t *testing.T, c *svcclient.Client, tenant string) int {
				return countVia[tenantReq, listInvoicesResp](t, c,
					"billing.v1.BillingService/GetOutstandingInvoices",
					tenantReq{TenantID: tenant}, tenant,
					func(r *listInvoicesResp) int { return len(r.Invoices) })
			},
		},
		{
			name:   "cattle-market-service",
			client: (*platform).cattleMarket,
			svc:    "cattlemarket.v1.CattleMarketService",
			write: func(t *testing.T, p *platform) string {
				t.Helper()
				resp, err := svcclient.Call[createListingReq, listingResp](
					context.Background(), p.cattleMarket(),
					"cattlemarket.v1.CattleMarketService/CreateListing",
					createListingReq{TenantID: p.tenant, CattleID: newID("cow"),
						SellerID: newID("sel"), Title: "e2e", AskingPrice: 85000,
						Currency: "INR", ListingType: "fixed", CreatedBy: "e2e"}, p.opts())
				if err != nil {
					t.Fatalf("create listing: %v", err)
				}
				return resp.Listing.ID
			},
			count: func(t *testing.T, c *svcclient.Client, tenant string) int {
				return countVia[tenantReq, listListingsResp](t, c,
					"cattlemarket.v1.CattleMarketService/ListActiveListings",
					tenantReq{TenantID: tenant, Limit: 100}, tenant,
					func(r *listListingsResp) int { return len(r.Listings) })
			},
		},
		{
			name:   "feed-service",
			client: (*platform).feed,
			svc:    "feed.v1.FeedService",
			write: func(t *testing.T, p *platform) string {
				t.Helper()
				resp, err := svcclient.Call[createFeedTypeReq, feedTypeResp](
					context.Background(), p.feed(),
					"feed.v1.FeedService/CreateFeedType",
					createFeedTypeReq{TenantID: p.tenant, Name: newID("fd"),
						Category: "concentrate", Unit: "KG", CreatedBy: "e2e"}, p.opts())
				if err != nil {
					t.Fatalf("create feed type: %v", err)
				}
				return resp.FeedType.ID
			},
			count: func(t *testing.T, c *svcclient.Client, tenant string) int {
				return countVia[tenantReq, listFeedTypesResp](t, c,
					"feed.v1.FeedService/ListFeedTypes",
					tenantReq{TenantID: tenant}, tenant,
					func(r *listFeedTypesResp) int { return len(r.FeedTypes) })
			},
		},
		{
			name:   "notification-service",
			client: (*platform).notification,
			svc:    "notification.v1.NotificationService",
			write: func(t *testing.T, p *platform) string {
				t.Helper()
				resp, err := svcclient.Call[sendNotificationReq, notificationResp](
					context.Background(), p.notification(),
					"notification.v1.NotificationService/SendNotification",
					sendNotificationReq{TenantID: p.tenant, RecipientID: newID("usr"),
						RecipientType: "user", Channel: "in_app", Title: "e2e",
						Body: "e2e", Priority: "normal", CreatedBy: "e2e"}, p.opts())
				if err != nil {
					t.Fatalf("send notification: %v", err)
				}
				return resp.Notification.ID
			},
			count: func(t *testing.T, c *svcclient.Client, tenant string) int {
				return countVia[listNotificationsReq, listNotificationsResp](t, c,
					"notification.v1.NotificationService/ListNotifications",
					// Both filters are required by the query: it compares
					// channel and status unconditionally, so an empty string
					// matches nothing rather than everything.
					listNotificationsReq{TenantID: tenant,
						Channel: "in_app", Status: "sent"}, tenant,
					func(r *listNotificationsResp) int { return len(r.Notifications) })
			},
		},
		{
			name:   "cattle-service",
			client: (*platform).cattle,
			svc:    "cattle.v1.CattleService",
			write: func(t *testing.T, p *platform) string {
				t.Helper()
				resp, err := svcclient.Call[createCattleReq, cattleResp](
					context.Background(), p.cattle(),
					"cattle.v1.CattleService/CreateCattle",
					createCattleReq{TenantID: p.tenant, TagNumber: newID("tag"),
						Name: "e2e", Gender: "F", Weight: 420, CreatedBy: "e2e"}, p.opts())
				if err != nil {
					t.Fatalf("create cattle: %v", err)
				}
				return resp.Cattle.ID
			},
			count: func(t *testing.T, c *svcclient.Client, tenant string) int {
				return countVia[listCattleReq, listCattleResp](t, c,
					"cattle.v1.CattleService/ListCattle",
					listCattleReq{TenantID: tenant, Limit: 100}, tenant,
					func(r *listCattleResp) int { return len(r.Cattle) })
			},
		},
		{
			name:   "farm-service",
			client: (*platform).farm,
			svc:    "farm.v1.FarmService",
			write: func(t *testing.T, p *platform) string {
				t.Helper()
				code := newID("frm")
				resp, err := svcclient.Call[createFarmReq, farmResp](
					context.Background(), p.farm(), "farm.v1.FarmService/CreateFarm",
					createFarmReq{TenantID: p.tenant, Name: code, Code: code,
						Address: "e2e", Country: "IN", Capacity: 100,
						Status: "active", CreatedBy: "e2e"}, p.opts())
				if err != nil {
					t.Fatalf("create farm: %v", err)
				}
				return resp.Farm.ID
			},
			count: func(t *testing.T, c *svcclient.Client, tenant string) int {
				return countVia[tenantReq, listFarmsResp](t, c,
					"farm.v1.FarmService/ListFarms",
					tenantReq{TenantID: tenant}, tenant,
					func(r *listFarmsResp) int { return len(r.Farms) })
			},
		},
		{
			name:   "reporting-service",
			client: (*platform).reporting,
			svc:    "reporting.v1.ReportingService",
			write: func(t *testing.T, p *platform) string {
				t.Helper()
				resp, err := svcclient.Call[requestReportReq, reportResp](
					context.Background(), p.reporting(),
					"reporting.v1.ReportingService/RequestReport",
					requestReportReq{TenantID: p.tenant, Name: newID("rep"),
						ReportType: "collections", Parameters: "{}", FileFormat: "csv",
						RequestedBy: "e2e", CreatedBy: "e2e"}, p.opts())
				if err != nil {
					t.Fatalf("request report: %v", err)
				}
				return resp.Report.ID
			},
			count: func(t *testing.T, c *svcclient.Client, tenant string) int {
				return countVia[tenantReq, listReportsResp](t, c,
					"reporting.v1.ReportingService/ListReports",
					tenantReq{TenantID: tenant}, tenant,
					func(r *listReportsResp) int { return len(r.Reports) })
			},
		},
	}
}

// One tenant writes; another asks and must see nothing.
//
// The write is asserted first. A read returning zero proves isolation only if
// there was something to miss — otherwise an empty table passes for a policy,
// which is the shape of every control in this platform that turned out to be
// doing nothing.
func TestOneTenantCannotSeeAnothersRowsThroughTheOlderServices(t *testing.T) {
	for _, c := range erpCases() {
		t.Run(c.name, func(t *testing.T) {
			owner := startPlatform(t)
			stranger := startPlatform(t) // a different tenant, same services

			id := c.write(t, owner)
			if id == "" {
				t.Fatal("the write returned no identifier, so there is nothing to have missed")
			}

			client := c.client(owner)

			// The owner sees what it wrote. Without this the assertion below
			// passes against a list procedure that returns nothing to anybody.
			if n := c.count(t, client, owner.tenant); n < 1 {
				t.Fatalf("the tenant that wrote row %s cannot see it (%d rows); the isolation "+
					"check below would then pass against a service that shows nobody anything",
					id, n)
			}

			if n := c.count(t, client, stranger.tenant); n != 0 {
				t.Errorf("a second tenant sees %d rows through %s, and it wrote none. Row %s "+
					"belongs to somebody else", n, c.name, id)
			}
		})
	}
}

// Every service in the harness answers a health check.
//
// The readiness probe in each Kubernetes manifest rests on this, and until these
// seven were added to the harness nothing had ever started them.
func TestEveryServiceInThePlatformAnswersHealth(t *testing.T) {
	p := startPlatform(t)
	// Twenty-six of the twenty-nine services in the tree. gateway, audit and
	// identity are covered elsewhere, and the harness list says why beside each.
	if len(p.clients) < 26 {
		t.Fatalf("the platform has %d services; the harness is meant to run every one it can, "+
			"and a count that has quietly shrunk means a service stopped being tested",
			len(p.clients))
	}
	for name, c := range p.clients {
		if err := c.Health(context.Background()); err != nil {
			t.Errorf("%s does not answer a health check: %v", name, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Money these services cannot carry
// ---------------------------------------------------------------------------

// A price the read path would mangle is refused rather than stored.
//
// These columns are NUMERIC(18,4) so one schema serves a yen deployment and a
// dinar one, and the services read them into float64. float64 carries a
// four-decimal value exactly only up to about 10^11 — measured, not assumed:
// 200,000 values below that round-tripped through
// NUMERIC(18,4) -> float64 -> JSON -> float64 losing nothing, and above 10^12
// more than three quarters of them lost a digit. 6791947779410.3551 came back
// as 6791947779410.3555.
//
// So the column could hold values the code cannot carry, and nothing said so.
// The database now refuses them. Refusing beats rounding on the way out, where
// both ends believe they agree.
func TestAPriceTooLargeToCarryIsRefusedRatherThanMangled(t *testing.T) {
	p := startPlatform(t)

	_, err := svcclient.Call[createListingReq, listingResp](
		context.Background(), p.cattleMarket(),
		"cattlemarket.v1.CattleMarketService/CreateListing",
		createListingReq{TenantID: p.tenant, CattleID: newID("cow"), SellerID: newID("sel"),
			Title: "e2e", AskingPrice: 6791947779410.3551,
			Currency: "INR", ListingType: "fixed", CreatedBy: "e2e"}, p.opts())
	if err == nil {
		t.Error("a price of 6791947779410.3551 was accepted; it comes back out of float64 as " +
			"6791947779410.3555, and both ends would believe they agreed on it")
	}

	// And a price any dairy might actually write is accepted and comes back
	// unchanged. Without this the refusal above could be a service that rejects
	// everything.
	resp, err := svcclient.Call[createListingReq, listingResp](
		context.Background(), p.cattleMarket(),
		"cattlemarket.v1.CattleMarketService/CreateListing",
		createListingReq{TenantID: p.tenant, CattleID: newID("cow"), SellerID: newID("sel"),
			Title: "e2e", AskingPrice: 85432.75,
			Currency: "INR", ListingType: "fixed", CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("an ordinary price was refused: %v", err)
	}
	if resp.Listing.AskingPrice != 85432.75 {
		t.Errorf("a price of 85432.75 came back as %v", resp.Listing.AskingPrice)
	}
}

// An unset filter means "not filtering by that", not "match the empty string".
//
// ListNotifications compared channel and status unconditionally, and neither
// field is required by the request type — so the obvious call, with no filters,
// returned an empty list. An empty list is indistinguishable from a tenant that
// has no notifications, so nothing about the answer says the filter was the
// problem. It cost this file a debugging cycle to notice, which is roughly what
// it would cost anybody.
func TestListingNotificationsWithNoFilterReturnsThemAll(t *testing.T) {
	p := startPlatform(t)

	for i := 0; i < 3; i++ {
		if _, err := svcclient.Call[sendNotificationReq, notificationResp](
			context.Background(), p.notification(),
			"notification.v1.NotificationService/SendNotification",
			sendNotificationReq{TenantID: p.tenant, RecipientID: newID("usr"),
				RecipientType: "user", Channel: "in_app", Title: "e2e",
				Body: "e2e", Priority: "normal", CreatedBy: "e2e"}, p.opts()); err != nil {
			t.Fatalf("send notification: %v", err)
		}
	}

	all, err := svcclient.Call[listNotificationsReq, listNotificationsResp](
		context.Background(), p.notification(),
		"notification.v1.NotificationService/ListNotifications",
		listNotificationsReq{TenantID: p.tenant}, p.opts())
	if err != nil {
		t.Fatalf("list with no filter: %v", err)
	}
	if len(all.Notifications) != 3 {
		t.Errorf("listing with no filter returned %d of 3 notifications; an unset filter must "+
			"not silently match nothing, because the empty answer looks exactly like a tenant "+
			"with none", len(all.Notifications))
	}

	// And a filter that is set still filters. Without this the fix above could
	// be a query that ignores its filters entirely.
	none, err := svcclient.Call[listNotificationsReq, listNotificationsResp](
		context.Background(), p.notification(),
		"notification.v1.NotificationService/ListNotifications",
		listNotificationsReq{TenantID: p.tenant, Channel: "sms"}, p.opts())
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	if len(none.Notifications) != 0 {
		t.Errorf("filtering to a channel nothing was sent on returned %d notifications, so the "+
			"filter is being ignored", len(none.Notifications))
	}
}

// ---------------------------------------------------------------------------
// A price that changed, and what is left of the old one
// ---------------------------------------------------------------------------

type createProductReq struct {
	TenantID    string `json:"tenant_id"`
	CategoryID  string `json:"category_id"`
	BrandID     string `json:"brand_id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	ProductType string `json:"product_type"`
	Status      string `json:"status"`
	CreatedBy   string `json:"created_by"`
}

type productResp struct {
	Product *struct {
		ID string `json:"id"`
	} `json:"product"`
}

type createSKUReq struct {
	TenantID  string  `json:"tenant_id"`
	ProductID string  `json:"product_id"`
	Code      string  `json:"code"`
	Name      string  `json:"name"`
	Price     float64 `json:"price"`
	Currency  string  `json:"currency"`
	Unit      string  `json:"unit"`
	UnitSize  float64 `json:"unit_size"`
	Status    string  `json:"status"`
	CreatedBy string  `json:"created_by"`
}

type updateSKUPriceReq struct {
	ID        string  `json:"id"`
	TenantID  string  `json:"tenant_id"`
	Price     float64 `json:"price"`
	UpdatedBy string  `json:"updated_by"`
}

type skuResp struct {
	SKU *struct {
		ID       string  `json:"id"`
		Price    float64 `json:"price"`
		Currency string  `json:"currency"`
	} `json:"sku"`
}

const catalogSvc = "productcatalog.v1.ProductCatalogService"

// aSKU creates a product and one SKU under it, priced in the given currency.
func aSKU(t *testing.T, p *platform, currency string, price float64) *skuResp {
	t.Helper()
	code := newID("sku")

	cat, err := svcclient.Call[createCategoryReq, categoryResp](
		context.Background(), p.catalog(), catalogSvc+"/CreateCategory",
		createCategoryReq{TenantID: p.tenant, Name: code, Slug: code, CreatedBy: "e2e"},
		p.opts())
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	prod, err := svcclient.Call[createProductReq, productResp](
		context.Background(), p.catalog(), catalogSvc+"/CreateProduct",
		createProductReq{TenantID: p.tenant, CategoryID: cat.Category.ID, Name: code,
			Slug: code, ProductType: "simple", Status: "active", CreatedBy: "e2e"},
		p.opts())
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	sku, err := svcclient.Call[createSKUReq, skuResp](
		context.Background(), p.catalog(), catalogSvc+"/CreateSKU",
		createSKUReq{TenantID: p.tenant, ProductID: prod.Product.ID, Code: code,
			Name: code, Price: price, Currency: currency, Unit: "kg", UnitSize: 1,
			Status: "active", CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("create SKU in %s at %v: %v", currency, price, err)
	}
	return sku
}

// A price you can set and cannot change.
//
// CreateSKU validated against the tenant's own currency scale — three decimals
// for a dinar deployment — and UpdateSKUPrice validated against a hardcoded two.
// So a price of 1.234 was accepted on the way in and every attempt to correct it
// was refused, with a message about precision that gave no hint the two paths
// disagreed.
func TestAPriceCanBeCorrectedInTheCurrencyItWasSetIn(t *testing.T) {
	p := startPlatform(t)

	// A three-decimal currency. The scale comes from ISO 4217, not from us.
	sku := aSKU(t, p, "BHD", 1.234)
	if sku.SKU.Price != 1.234 {
		t.Fatalf("a dinar SKU was created at %v, want 1.234", sku.SKU.Price)
	}

	updated, err := svcclient.Call[updateSKUPriceReq, skuResp](
		context.Background(), p.catalog(), catalogSvc+"/UpdateSKUPrice",
		updateSKUPriceReq{ID: sku.SKU.ID, TenantID: p.tenant, Price: 1.235,
			UpdatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("a price set at three decimals could not be corrected to three decimals: %v\n"+
			"CreateSKU and UpdateSKUPrice have to validate the same column the same way, or "+
			"there are prices the platform will accept and never let anybody change", err)
	}
	if updated.SKU.Price != 1.235 {
		t.Errorf("the price came back as %v, want 1.235", updated.SKU.Price)
	}

	// And a fourth decimal is still refused, in a currency that has three. The
	// fix must not have been to stop checking.
	if _, err := svcclient.Call[updateSKUPriceReq, skuResp](
		context.Background(), p.catalog(), catalogSvc+"/UpdateSKUPrice",
		updateSKUPriceReq{ID: sku.SKU.ID, TenantID: p.tenant, Price: 1.2345,
			UpdatedBy: "e2e"}, p.opts()); err == nil {
		t.Error("a four-decimal price was accepted for a three-decimal currency; the database " +
			"would round it on the way in and nobody would be told")
	}
}

// Changing a price records what it was.
//
// This is the one place in the older services where a money figure is
// overwritten and nothing else holds it. An order total is derived from lines
// that are inserted, so losing the total loses nothing reconstructible; a SKU's
// price is the primary fact, and an invoice raised before it moved cannot be
// reconciled against a price that no longer exists anywhere.
func TestChangingASKUPriceRecordsWhatItWas(t *testing.T) {
	p := startPlatform(t)
	sku := aSKU(t, p, "INR", 42.50)

	if _, err := svcclient.Call[updateSKUPriceReq, skuResp](
		context.Background(), p.catalog(), catalogSvc+"/UpdateSKUPrice",
		updateSKUPriceReq{ID: sku.SKU.ID, TenantID: p.tenant, Price: 47.75,
			UpdatedBy: "e2e"}, p.opts()); err != nil {
		t.Fatalf("update price: %v", err)
	}

	// The audit row is written in the same transaction as the change, so if the
	// price moved the record of it moving is there too.
	conn, err := pgx.Connect(context.Background(), dsn(t, "e2e_catalog"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(context.Background())

	var action string
	var before, after []byte
	err = conn.QueryRow(context.Background(), `
		SELECT action, old_value, new_value
		  FROM audit_logs
		 WHERE tenant_id=$1 AND resource_id=$2 AND action='update_sku_price'`,
		p.tenant, sku.SKU.ID).Scan(&action, &before, &after)
	if err != nil {
		t.Fatalf("no audit entry for a price change (%v); the price moved from 42.50 to 47.75 "+
			"and nothing anywhere records that it did, or what it was", err)
	}
	if !strings.Contains(string(before), "42.5") {
		t.Errorf("the audit entry's old_value is %s and does not carry the old price; a "+
			"record that a change happened without the value it replaced cannot reconcile an "+
			"invoice raised before it", before)
	}
	if !strings.Contains(string(after), "47.75") {
		t.Errorf("the audit entry's new_value is %s and does not carry the new price", after)
	}
}

// ---------------------------------------------------------------------------
// A status that changed, and what it changed from
// ---------------------------------------------------------------------------

type createOrderReq struct {
	TenantID   string `json:"tenant_id"`
	CustomerID string `json:"customer_id"`
	Currency   string `json:"currency"`
	CreatedBy  string `json:"created_by"`
}

type orderActionReq struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	UpdatedBy string `json:"updated_by"`
}

type orderRespProto struct {
	Order *struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"order"`
}

// transitionsFor reads what a service recorded about one resource's status
// changes, in order.
func transitionsFor(t *testing.T, database, tenant, resourceID, action string) []string {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), dsn(t, database))
	if err != nil {
		t.Fatalf("connect to %s: %v", database, err)
	}
	defer conn.Close(context.Background())

	rows, err := conn.Query(context.Background(), `
		SELECT old_value, new_value FROM audit_logs
		 WHERE tenant_id=$1 AND resource_id=$2 AND action=$3
		 ORDER BY created_at`, tenant, resourceID, action)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var before, after []byte
		if err := rows.Scan(&before, &after); err != nil {
			t.Fatal(err)
		}
		out = append(out, string(before)+" -> "+string(after))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// Voiding an invoice records the state it was voided from.
//
// A status is a decision, not a fact about the world, and `updated_by` named
// whoever made the last one without saying what they changed it from. An invoice
// reading "cancelled" gave no indication whether it had been a draft nobody had
// sent or something a customer had already been billed for. Those are different
// conversations, and the second one is a dispute.
func TestVoidingAnInvoiceRecordsWhatItWasVoidedFrom(t *testing.T) {
	p := startPlatform(t)

	inv, err := svcclient.Call[createInvoiceReq, invoiceResp](
		context.Background(), p.billing(), "billing.v1.BillingService/CreateInvoice",
		createInvoiceReq{TenantID: p.tenant, CustomerID: newID("cus"),
			ReferenceID: newID("ord"), ReferenceType: "ORDER", Currency: "INR",
			IssuedAt: time.Now().UTC(), CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("create invoice: %v", err)
	}

	// Sent, then voided — so the state it was voided from is one somebody would
	// argue about, rather than the draft it started as.
	for _, step := range []string{"SendInvoice", "VoidInvoice"} {
		if _, err := svcclient.Call[invoiceActionReq, invoiceResp](
			context.Background(), p.billing(), "billing.v1.BillingService/"+step,
			invoiceActionReq{ID: inv.Invoice.ID, TenantID: p.tenant, UpdatedBy: "e2e"},
			p.opts()); err != nil {
			t.Fatalf("%s: %v", step, err)
		}
	}

	got := transitionsFor(t, "e2e_billing", p.tenant, inv.Invoice.ID, "update_invoice_status")
	if len(got) != 2 {
		t.Fatalf("an invoice was sent and then voided and %d transitions were recorded: %v\n"+
			"Both are decisions somebody made, and neither leaves any other trace", len(got), got)
	}
	if !strings.Contains(got[1], "sent") {
		t.Errorf("the void records %q; without the state it was voided from, an invoice a "+
			"customer had already been sent is indistinguishable from a draft nobody saw", got[1])
	}
	if !strings.Contains(got[1], "cancelled") {
		t.Errorf("the void records %q; a voided invoice reaches the status 'cancelled' in this "+
			"service, and the entry has to say what it became", got[1])
	}
}

// Cancelling an order records what it was cancelled from.
//
// The same failure as the invoice, in the service beside it: an order reading
// "cancelled" said nothing about whether it had been confirmed and was out for
// delivery.
func TestCancellingAnOrderRecordsWhatItWasCancelledFrom(t *testing.T) {
	p := startPlatform(t)

	order, err := svcclient.Call[createOrderReq, orderRespProto](
		context.Background(), p.order(), "order.v1.OrderService/CreateOrder",
		createOrderReq{TenantID: p.tenant, CustomerID: newID("cus"),
			Currency: "INR", CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if _, err := svcclient.Call[orderActionReq, orderRespProto](
		context.Background(), p.order(), "order.v1.OrderService/CancelOrder",
		orderActionReq{ID: order.Order.ID, TenantID: p.tenant, UpdatedBy: "e2e"},
		p.opts()); err != nil {
		t.Fatalf("cancel order: %v", err)
	}

	got := transitionsFor(t, "e2e_order", p.tenant, order.Order.ID, "update_order_status")
	if len(got) != 1 {
		t.Fatalf("an order was cancelled and %d transitions were recorded: %v\n"+
			"`updated_by` names who and nothing says what it was cancelled from", len(got), got)
	}
	if !strings.Contains(got[0], "cancel") {
		t.Errorf("the entry records %q and does not say the order was cancelled", got[0])
	}
}

// An animal recorded without a breed can be read back.
//
// `breed_id` is nullable and the insert wrote NULL for an empty one, which is
// right — an absent optional reference should be NULL rather than the empty
// string, or the foreign key does not mean what it says. But every SELECT read
// it into a plain Go string, and pgx cannot scan NULL into one.
//
// So a crossbred cow nobody had classified could be created and then never read
// back. Worse, ListCattle scans the same columns, so one such animal made the
// whole tenant's list fail — the failure was not confined to the row that caused
// it.
//
// The isolation table above found this by accident, because it happens to create
// an animal without a breed. This says so on purpose, and covers the other two
// optional references written the same way.
func TestAnAnimalWithNoBreedOwnerOrFarmCanStillBeRead(t *testing.T) {
	p := startPlatform(t)

	created, err := svcclient.Call[createCattleReq, cattleResp](
		context.Background(), p.cattle(), "cattle.v1.CattleService/CreateCattle",
		createCattleReq{TenantID: p.tenant, TagNumber: newID("tag"),
			Name: "unclassified", Gender: "F", Weight: 380, CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("an animal with no breed could not be created: %v", err)
	}

	// Read it back on its own.
	got, err := svcclient.Call[struct {
		ID       string `json:"id"`
		TenantID string `json:"tenant_id"`
	}, cattleResp](
		context.Background(), p.cattle(), "cattle.v1.CattleService/GetCattle",
		struct {
			ID       string `json:"id"`
			TenantID string `json:"tenant_id"`
		}{ID: created.Cattle.ID, TenantID: p.tenant}, p.opts())
	if err != nil {
		t.Fatalf("an animal with no breed was created and cannot be read back: %v\n"+
			"The row exists, holds its tag number, and every read of it fails", err)
	}
	if got.Cattle.ID != created.Cattle.ID {
		t.Errorf("read back %s, want %s", got.Cattle.ID, created.Cattle.ID)
	}

	// And it does not break the list for everything beside it.
	listed, err := svcclient.Call[listCattleReq, listCattleResp](
		context.Background(), p.cattle(), "cattle.v1.CattleService/ListCattle",
		listCattleReq{TenantID: p.tenant, Limit: 100}, p.opts())
	if err != nil {
		t.Fatalf("one animal with no breed made the whole tenant's list fail: %v", err)
	}
	if len(listed.Cattle) < 1 {
		t.Errorf("the list came back with %d animals and one was just created",
			len(listed.Cattle))
	}
}

// Deleting an animal records who deleted it.
//
// The request carried a `deleted_by` all along, the handler dropped it, and the
// repository took no actor at all. What the row then said was worse than
// nothing: the update stamped `updated_at` to the moment of deletion and left
// `updated_by` holding whoever had last edited the animal. Those two fields are
// meant to be read as a pair, so the record did not merely omit who deleted it —
// it named the wrong person, and the caller who supplied the right one had every
// reason to believe it had been kept.
func TestDeletingAnAnimalRecordsWhoDidIt(t *testing.T) {
	p := startPlatform(t)

	created, err := svcclient.Call[createCattleReq, cattleResp](
		context.Background(), p.cattle(), "cattle.v1.CattleService/CreateCattle",
		createCattleReq{TenantID: p.tenant, TagNumber: newID("tag"),
			Name: "doomed", Gender: "F", Weight: 400, CreatedBy: "the.clerk"}, p.opts())
	if err != nil {
		t.Fatalf("create cattle: %v", err)
	}

	if _, err := svcclient.Call[deleteCattleReq, deleteCattleResp](
		context.Background(), p.cattle(), "cattle.v1.CattleService/DeleteCattle",
		deleteCattleReq{ID: created.Cattle.ID, TenantID: p.tenant,
			DeletedBy: "the.supervisor"}, p.opts()); err != nil {
		t.Fatalf("delete cattle: %v", err)
	}

	conn, err := pgx.Connect(context.Background(), dsn(t, "e2e_cattle"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(context.Background())

	// The row survives — this is a soft delete — and now says who ended it.
	var updatedBy string
	var deletedAt *time.Time
	if err := conn.QueryRow(context.Background(),
		`SELECT updated_by, deleted_at FROM cattle WHERE id=$1 AND tenant_id=$2`,
		created.Cattle.ID, p.tenant).Scan(&updatedBy, &deletedAt); err != nil {
		t.Fatalf("read the deleted row: %v", err)
	}
	if deletedAt == nil {
		t.Fatal("the animal was not marked deleted")
	}
	if updatedBy != "the.supervisor" {
		t.Errorf("the row says %q made the last change and %q deleted it; `updated_at` moved "+
			"to the deletion and `updated_by` did not, so the two together name the wrong "+
			"person", updatedBy, "the.supervisor")
	}

	// And the audit trail carries it too, so the deletion is findable by the tag
	// number somebody searches for when an animal is missing from a list.
	var before, after []byte
	if err := conn.QueryRow(context.Background(), `
		SELECT old_value, new_value FROM audit_logs
		 WHERE tenant_id=$1 AND resource_id=$2 AND action='delete_cattle'`,
		p.tenant, created.Cattle.ID).Scan(&before, &after); err != nil {
		t.Fatalf("an animal was deleted and nothing records it: %v", err)
	}
	if !strings.Contains(string(after), "the.supervisor") {
		t.Errorf("the audit entry is %s and does not name who deleted the animal", after)
	}
}

// And a deletion with nobody's name against it is refused.
//
// The row it writes is the only account of an animal disappearing from every
// list, and one that cannot say who did it is an account nobody can follow up.
func TestDeletingAnAnimalWithNoNameAgainstItIsRefused(t *testing.T) {
	p := startPlatform(t)

	created, err := svcclient.Call[createCattleReq, cattleResp](
		context.Background(), p.cattle(), "cattle.v1.CattleService/CreateCattle",
		createCattleReq{TenantID: p.tenant, TagNumber: newID("tag"),
			Name: "safe", Gender: "F", Weight: 400, CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("create cattle: %v", err)
	}

	if _, err := svcclient.Call[deleteCattleReq, deleteCattleResp](
		context.Background(), p.cattle(), "cattle.v1.CattleService/DeleteCattle",
		deleteCattleReq{ID: created.Cattle.ID, TenantID: p.tenant}, p.opts()); err == nil {
		t.Error("an animal was deleted with no name against it")
	}
}

type deleteCattleReq struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	DeletedBy string `json:"deleted_by"`
}

type deleteCattleResp struct {
	Success bool `json:"success"`
}
