//go:build e2e

// Somebody is told.
//
// notification-service has held inboxes for as long as this platform has
// existed, and nothing anywhere wrote to them. A payment was held, a fortnight
// approved, money marked as paid, and the only way to learn any of it was to go
// and look. These drive a money event in settlement and read the message out of
// the inbox in notification-service — the join, which is the thing neither
// service's own tests can see.
package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

type inboxReq struct {
	TenantID      string `json:"tenant_id"`
	RecipientID   string `json:"recipient_id,omitempty"`
	RecipientType string `json:"recipient_type,omitempty"`
}

type inboxItem struct {
	ID            string `json:"id"`
	RecipientID   string `json:"recipient_id"`
	RecipientType string `json:"recipient_type"`
	Title         string `json:"title"`
	Body          string `json:"body"`
	Priority      string `json:"priority"`
	ReferenceID   string `json:"reference_id"`
	ReferenceType string `json:"reference_type"`
}

type inboxResp struct {
	Notifications []inboxItem `json:"notifications"`
}

// inbox reads a role's messages in one tenant.
func inbox(t *testing.T, p *platform, opts svcclient.CallOptions, role string) []inboxItem {
	t.Helper()
	out, err := svcclient.Call[inboxReq, inboxResp](context.Background(), p.notification(),
		notifySvc+"/ListNotifications",
		inboxReq{TenantID: opts.Tenant, RecipientType: "role", RecipientID: role}, opts)
	if err != nil {
		t.Fatalf("ListNotifications for %s: %v", role, err)
	}
	return out.Notifications
}

// awaitMessage polls a role's inbox for a message about one reference.
//
// Polled because delivery is asynchronous by design: the message is queued in
// the transaction that makes the change and delivered by a sweep afterwards. A
// commit kicks the sweep, so this normally returns on the first or second look;
// the deadline is for the case where it does not, and it fails with the inbox
// as it stood rather than with a timeout.
func awaitMessage(t *testing.T, p *platform, role, referenceID string) inboxItem {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var last []inboxItem
	for time.Now().Before(deadline) {
		last = inbox(t, p, p.opts(), role)
		for _, m := range last {
			if m.ReferenceID == referenceID {
				return m
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("no message about %s reached the %s inbox within 15s; it holds %d messages: %+v",
		referenceID, role, len(last), last)
	return inboxItem{}
}

// Holding a payment tells the people who can act on it.
//
// A hold is a decision about one member's money that somebody has to resolve.
// The supervisor decides disputed things and the accountant owns the money, so
// both are told; the auditor is not, because nothing has moved yet.
func TestHoldingAPaymentTellsTheSupervisorAndTheAccountant(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "March", "2026-03-01T00:00:00Z", "")

	producer := newID("prod")
	deliverFortnight(t, p, producer, marchFirstTen[:2])
	cycle := mustOpenCycle(t, p, "March 1-15", "2026-03-01", "2026-03-15", "CAP_AT_EARNINGS")
	if _, err := gather(t, p, cycle.ID); err != nil {
		t.Fatalf("GatherCycle: %v", err)
	}
	pay := payableFor(t, p, cycle.ID, producer)

	const reason = "member disputes the fat reading on the 2nd"
	if _, err := svcclient.Call[holdPayableReq, payableResp](context.Background(), p.settlement(),
		settlementSvc+"/HoldPayable", holdPayableReq{
			TenantID: p.tenant, ID: pay.ID, Reason: reason, Actor: "US_ACCOUNTANT",
		}, p.opts()); err != nil {
		t.Fatalf("HoldPayable: %v", err)
	}

	for _, role := range []string{"supervisor", "accountant"} {
		m := awaitMessage(t, p, role, pay.ID)
		if m.RecipientType != "role" || m.RecipientID != role {
			t.Errorf("the message in the %s inbox is addressed to %s %s", role, m.RecipientType, m.RecipientID)
		}
		if !strings.Contains(m.Title, producer) || !strings.Contains(m.Title, "held") {
			t.Errorf("%s: title %q does not say whose payment was held", role, m.Title)
		}
		if !strings.Contains(m.Body, reason) {
			t.Errorf("%s: body %q does not carry the reason, which is the one thing the reader "+
				"needs in order to act", role, m.Body)
		}
		if !strings.Contains(m.Body, pay.Net) {
			t.Errorf("%s: body %q does not say how much is held (%s)", role, m.Body, pay.Net)
		}
		if m.ReferenceType != "producer_payable" {
			t.Errorf("%s: reference_type %q, want producer_payable", role, m.ReferenceType)
		}
		if m.Priority != "high" {
			t.Errorf("%s: a hold is priority %q; it is the one message that needs somebody today", role, m.Priority)
		}
	}

	// Nothing has moved, so the auditor has nothing to see yet. Checked after
	// the two positives so it cannot pass merely because delivery is slow.
	for _, m := range inbox(t, p, p.opts(), "auditor") {
		if m.ReferenceID == pay.ID {
			t.Errorf("the auditor was told about a hold, and nothing has moved: %+v", m)
		}
	}

	// And another tenant's supervisor sees none of it.
	stranger := actingAs(newID("ten"), "e2e")
	if got := inbox(t, p, stranger, "supervisor"); len(got) != 0 {
		t.Errorf("another tenant's supervisor inbox holds %d of this society's messages", len(got))
	}
}

// Paying somebody tells the auditor, and the message survives being queued.
//
// The second half is the point of the outbox. The message is written in the
// same transaction as the payment and marked delivered only when
// notification-service has accepted it — so a row exists for every payment,
// and every row is either delivered or still owed. This reads the outbox
// directly to see both halves.
func TestPayingSomebodyTellsTheAuditorThroughTheOutbox(t *testing.T) {
	p := startPlatform(t)
	declareChart(t, p, "March", "2026-03-01T00:00:00Z", "")
	ctx := context.Background()

	producer := newID("prod")
	deliverFortnight(t, p, producer, marchFirstTen[:2])
	cycle := mustOpenCycle(t, p, "March 1-15", "2026-03-01", "2026-03-15", "CAP_AT_EARNINGS")
	if _, err := gather(t, p, cycle.ID); err != nil {
		t.Fatalf("GatherCycle: %v", err)
	}
	if _, err := approve(t, p, cycle.ID); err != nil {
		t.Fatalf("ApproveCycle: %v", err)
	}
	pay := payableFor(t, p, cycle.ID, producer)

	if _, err := svcclient.Call[markPaidReq, payableResp](ctx, p.settlement(),
		settlementSvc+"/MarkPaid", markPaidReq{
			TenantID: p.tenant, ID: pay.ID, PaymentReference: "CHQ-4471", Actor: "US_ACCOUNTANT",
		}, p.opts()); err != nil {
		t.Fatalf("MarkPaid: %v", err)
	}

	m := awaitMessage(t, p, "auditor", pay.ID)
	if !strings.Contains(m.Body, "CHQ-4471") {
		t.Errorf("the auditor's message %q does not carry the payment reference", m.Body)
	}
	if !strings.Contains(m.Body, pay.Net) {
		t.Errorf("the auditor's message %q does not say how much was paid (%s)", m.Body, pay.Net)
	}

	// The outbox, read directly: one row per recipient of this event, written
	// with the payment, and marked delivered with a count of tries.
	conn, err := pgx.Connect(ctx, dsn(t, "e2e_settlement"))
	if err != nil {
		t.Fatalf("connect to settlement's database: %v", err)
	}
	defer conn.Close(ctx)

	deadline := time.Now().Add(10 * time.Second)
	for {
		var rows, delivered, attempts int
		if err := conn.QueryRow(ctx, `
			SELECT count(*), count(delivered_at), COALESCE(sum(attempts), 0)
			  FROM notification_outbox
			 WHERE tenant_id = $1 AND reference_id = $2 AND event = 'payable_paid'`,
			p.tenant, pay.ID).Scan(&rows, &delivered, &attempts); err != nil {
			t.Fatalf("read the outbox: %v", err)
		}
		if rows != 1 {
			t.Fatalf("%d outbox rows for one payment to one role, want 1", rows)
		}
		if delivered == 1 {
			if attempts < 1 {
				t.Errorf("delivered with %d attempts recorded; the constraint should have refused this", attempts)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the outbox row is still not marked delivered after the message reached the inbox")
		}
		time.Sleep(200 * time.Millisecond)
	}

	// The cycle's approval was a message too, to the same inbox.
	found := false
	for _, m := range inbox(t, p, p.opts(), "auditor") {
		if m.ReferenceID == cycle.ID && m.ReferenceType == "payment_cycle" {
			found = true
			if !strings.Contains(m.Body, "1 producers") && !strings.Contains(m.Body, "1 producer") {
				t.Errorf("the approval message %q does not say how many producers it approved", m.Body)
			}
		}
	}
	if !found {
		t.Error("approving the cycle left nothing in the auditor's inbox")
	}
}
