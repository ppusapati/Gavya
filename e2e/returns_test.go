//go:build e2e

// Returns, which did not exist.
//
// order-service has had a `returns` table and a `domain.Return` since it was
// written, and nothing else: no repository method, no service method, no
// endpoint, no reference anywhere in the tree. Refunds were a shape that looked
// like a feature.
//
// What is wired is the state machine the type already declared in a comment —
// requested, then approved or rejected, then completed — and nothing beyond it.
// In particular, completing a return does not touch the invoice or the order's
// totals. What a refund should do to a customer's account is an accounting
// decision nobody has made here, and the tests below are careful not to imply
// one.
package e2e

import (
	"context"
	"sync"
	"testing"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

type requestReturnReq struct {
	TenantID     string `json:"tenant_id"`
	OrderID      string `json:"order_id"`
	Reason       string `json:"reason"`
	RefundAmount string `json:"refund_amount"`
	CreatedBy    string `json:"created_by"`
}

type decideReturnReq struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	Status    string `json:"status"`
	UpdatedBy string `json:"updated_by"`
}

type returnResp struct {
	Return *struct {
		ID           string `json:"id"`
		Status       string `json:"status"`
		RefundAmount string `json:"refund_amount"`
		Currency     string `json:"currency"`
		Reason       string `json:"reason"`
	} `json:"return"`
}

type listReturnsResp struct {
	Returns []*struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"returns"`
}

// anOrderWorth confirms an order carrying one line at the given price, and
// returns its id. A return needs something to be returned from.
func anOrderWorth(t *testing.T, p *platform, unitPrice string) string {
	t.Helper()
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
			Quantity: 1, UnitPrice: unitPrice, TaxRate: 0,
			CreatedBy: "e2e"}, p.opts()); err != nil {
		t.Fatalf("add line: %v", err)
	}
	if _, err := svcclient.Call[orderActionReq, orderRespProto](
		context.Background(), p.order(), orderSvc+"/ConfirmOrder",
		orderActionReq{ID: order.Order.ID, TenantID: p.tenant, UpdatedBy: "e2e"},
		p.opts()); err != nil {
		t.Fatalf("confirm order: %v", err)
	}
	return order.Order.ID
}

func request(t *testing.T, p *platform, orderID, amount string) *returnResp {
	t.Helper()
	out, err := svcclient.Call[requestReturnReq, returnResp](
		context.Background(), p.order(), orderSvc+"/RequestReturn",
		requestReturnReq{TenantID: p.tenant, OrderID: orderID,
			Reason: "the pouches arrived split", RefundAmount: amount,
			CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("request a return of %s: %v", amount, err)
	}
	return out
}

func decide(t *testing.T, p *platform, id, status string) (*returnResp, error) {
	t.Helper()
	return svcclient.Call[decideReturnReq, returnResp](
		context.Background(), p.order(), orderSvc+"/DecideReturn",
		decideReturnReq{ID: id, TenantID: p.tenant, Status: status,
			UpdatedBy: "supervisor"}, p.opts())
}

// A return is requested, approved and carried out.
func TestAReturnIsRequestedApprovedAndCompleted(t *testing.T) {
	p := startPlatform(t)
	order := anOrderWorth(t, p, "100.00")

	asked := request(t, p, order, "40.00")
	if asked.Return.Status != "requested" {
		t.Fatalf("a new return is %s, want requested", asked.Return.Status)
	}
	if asked.Return.RefundAmount != "40.00" || asked.Return.Currency != "INR" {
		t.Errorf("the return is for %s %s, want 40.00 INR",
			asked.Return.RefundAmount, asked.Return.Currency)
	}

	approved, err := decide(t, p, asked.Return.ID, "approved")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if approved.Return.Status != "approved" {
		t.Fatalf("status = %s, want approved", approved.Return.Status)
	}

	done, err := decide(t, p, asked.Return.ID, "completed")
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if done.Return.Status != "completed" {
		t.Errorf("status = %s, want completed", done.Return.Status)
	}

	// The order's own totals are untouched. A refund is not a change to what
	// was charged, and deciding what it does to a customer's account is an
	// accounting decision nobody has made here.
	after, err := svcclient.Call[idTenantReq, orderItemResp](
		context.Background(), p.order(), orderSvc+"/GetOrder",
		idTenantReq{ID: order, TenantID: p.tenant}, p.opts())
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	if after.Order.TotalAmount != "100.00" {
		t.Errorf("the order now totals %s, want 100.00 — completing a refund changed "+
			"what the order says was charged, which is an accounting decision this "+
			"service has not been told to make", after.Order.TotalAmount)
	}
}

// Refunds cannot add up to more than the order charged.
//
// This is arithmetic rather than policy: refunding more than was taken is wrong
// under any refund policy, which is why it is a database trigger rather than a
// check in one code path.
func TestRefundsCannotExceedWhatTheOrderCharged(t *testing.T) {
	p := startPlatform(t)
	order := anOrderWorth(t, p, "100.00")

	first := request(t, p, order, "60.00")
	if _, err := decide(t, p, first.Return.ID, "approved"); err != nil {
		t.Fatalf("approve the first 60: %v", err)
	}

	// Another 60 takes it to 120 against an order of 100.
	second := request(t, p, order, "60.00")
	if _, err := decide(t, p, second.Return.ID, "approved"); err == nil {
		t.Error("a second refund of 60 was approved against an order of 100; the " +
			"customer is now owed more than they ever paid")
	}

	// But the exact remainder is fine. Without this the refusal above could be
	// a service that approves nothing.
	rest := request(t, p, order, "40.00")
	if _, err := decide(t, p, rest.Return.ID, "approved"); err != nil {
		t.Errorf("the exact remaining 40 was refused: %v", err)
	}
}

// A request may ask for more than the order charged; only approving it is
// refused.
//
// What somebody asked for is part of the record of what was decided. Refusing to
// write the request down loses the fact that it was made, and the person who made
// it is then arguing about a conversation with no record.
func TestAnOverlargeRequestIsRecordedAndThenRefused(t *testing.T) {
	p := startPlatform(t)
	order := anOrderWorth(t, p, "100.00")

	asked := request(t, p, order, "500.00")
	if asked.Return.Status != "requested" {
		t.Fatalf("status = %s, want requested", asked.Return.Status)
	}

	if _, err := decide(t, p, asked.Return.ID, "approved"); err == nil {
		t.Error("a refund of 500 was approved against an order of 100")
	}

	// And it can be rejected, which is what actually happens to it.
	rejected, err := decide(t, p, asked.Return.ID, "rejected")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if rejected.Return.Status != "rejected" {
		t.Errorf("status = %s, want rejected", rejected.Return.Status)
	}
}

// Four approvals fired at once leave one committed.
//
// This does not prove the race is closed, and saying so matters. Fired through
// HTTP the window is too narrow to hit: this passes whichever way round the
// trigger's two statements are written, which was measured rather than assumed
// by writing them the wrong way round and watching it pass. It is a smoke test
// — four calls, one survivor — and not a concurrency guarantee.
//
// The guarantee is TestTwoApprovalsRacingCannotBothFindRoom in order-service's
// own repository tests, which holds one transaction open and steps the other
// into it. That one fails the moment the sum is taken before the order is
// locked, which is the shape that let two approvals of 60 both through against
// an order of 100.
func TestConcurrentApprovalsCannotBothFindRoom(t *testing.T) {
	p := startPlatform(t)
	order := anOrderWorth(t, p, "100.00")

	const attempts = 4
	ids := make([]string, attempts)
	for i := range ids {
		ids[i] = request(t, p, order, "60.00").Return.ID
	}

	var wg sync.WaitGroup
	errs := make([]error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = decide(t, p, ids[i], "approved")
		}(i)
	}
	wg.Wait()

	approved := 0
	for _, err := range errs {
		if err == nil {
			approved++
		}
	}
	if approved != 1 {
		t.Errorf("%d of %d concurrent approvals of 60 succeeded against an order of "+
			"100, want exactly 1 — the rest are money the order never took",
			approved, attempts)
	}

	list, err := svcclient.Call[listOrderReturnsReq, listReturnsResp](
		context.Background(), p.order(), orderSvc+"/ListOrderReturns",
		listOrderReturnsReq{OrderID: order, TenantID: p.tenant}, p.opts())
	if err != nil {
		t.Fatalf("list returns: %v", err)
	}
	committed := 0
	for _, r := range list.Returns {
		if r.Status == "approved" || r.Status == "completed" {
			committed++
		}
	}
	if committed != 1 {
		t.Errorf("%d returns of 60 are committed against an order of 100", committed)
	}
}

// A return moves only where it may.
func TestAReturnMovesOnlyWhereItMay(t *testing.T) {
	p := startPlatform(t)
	order := anOrderWorth(t, p, "100.00")

	// Completed without being approved first: refused. A refund carried out
	// that nobody approved is the whole reason there are two steps.
	straight := request(t, p, order, "10.00")
	if _, err := decide(t, p, straight.Return.ID, "completed"); err == nil {
		t.Error("a return went straight from requested to completed; the approval " +
			"step is what somebody would be asked to defend")
	}

	// Rejected is final. Reopening one by moving it on would leave no record
	// that it had been refused.
	refused := request(t, p, order, "10.00")
	if _, err := decide(t, p, refused.Return.ID, "rejected"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	for _, to := range []string{"approved", "completed", "requested"} {
		if _, err := decide(t, p, refused.Return.ID, to); err == nil {
			t.Errorf("a rejected return was moved to %s", to)
		}
	}
}

// A return says why, or it is not recorded.
func TestAReturnMustSayWhy(t *testing.T) {
	p := startPlatform(t)
	order := anOrderWorth(t, p, "100.00")

	if _, err := svcclient.Call[requestReturnReq, returnResp](
		context.Background(), p.order(), orderSvc+"/RequestReturn",
		requestReturnReq{TenantID: p.tenant, OrderID: order, Reason: "   ",
			RefundAmount: "10.00", CreatedBy: "e2e"}, p.opts()); err == nil {
		t.Error("a return with no reason was recorded; a refund nobody stated a " +
			"reason for is a payment nobody can account for afterwards")
	}
}

// An order with nothing sent has nothing to return.
func TestADraftOrderCannotBeReturned(t *testing.T) {
	p := startPlatform(t)
	order, err := svcclient.Call[createOrderReq, orderRespProto](
		context.Background(), p.order(), orderSvc+"/CreateOrder",
		createOrderReq{TenantID: p.tenant, CustomerID: newID("cus"),
			Currency: "INR", CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("create order: %v", err)
	}

	if _, err := svcclient.Call[requestReturnReq, returnResp](
		context.Background(), p.order(), orderSvc+"/RequestReturn",
		requestReturnReq{TenantID: p.tenant, OrderID: order.Order.ID,
			Reason: "changed my mind", RefundAmount: "10.00",
			CreatedBy: "e2e"}, p.opts()); err == nil {
		t.Error("a draft order was returned; nothing has been sent and nothing paid")
	}
}

type listOrderReturnsReq struct {
	OrderID  string `json:"order_id"`
	TenantID string `json:"tenant_id"`
}
