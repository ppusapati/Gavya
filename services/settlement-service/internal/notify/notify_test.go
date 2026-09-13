package notify

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/ppusapati/gavya/libs/integrity/tenantctx"

	"github.com/ppusapati/gavya/services/settlement-service/internal/domain"
)

// An in-memory outbox with the same semantics as the table.
type fakeStore struct {
	mu   sync.Mutex
	rows map[string]*domain.QueuedNotification
	// tenantSeen records which tenant each mark ran under, because the sweep
	// reads across tenants and the marks must not.
	tenantSeen map[string]string
}

func newStore(rows ...*domain.QueuedNotification) *fakeStore {
	s := &fakeStore{rows: map[string]*domain.QueuedNotification{}, tenantSeen: map[string]string{}}
	for _, r := range rows {
		s.rows[r.ID] = r
	}
	return s
}

func (s *fakeStore) NotificationsToDeliver(_ context.Context, limit int) ([]*domain.QueuedNotification, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*domain.QueuedNotification
	for _, r := range s.rows {
		if r.DeliveredAt == nil && len(out) < limit {
			c := *r
			out = append(out, &c)
		}
	}
	return out, nil
}

func (s *fakeStore) NotificationDelivered(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, _ := tenantctx.From(ctx)
	s.tenantSeen[id] = t
	r := s.rows[id]
	r.Attempts++
	now := r.CreatedAt
	r.DeliveredAt = &now
	return nil
}

func (s *fakeStore) NotificationFailed(ctx context.Context, id, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, _ := tenantctx.From(ctx)
	s.tenantSeen[id] = t
	r := s.rows[id]
	r.Attempts++
	r.LastError = reason
	return nil
}

// A sender that fails a set number of times per message, then accepts.
type flakySender struct {
	mu       sync.Mutex
	failures int
	tried    map[string]int
	sent     []string
}

func (f *flakySender) Send(_ context.Context, n *domain.QueuedNotification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tried == nil {
		f.tried = map[string]int{}
	}
	f.tried[n.ID]++
	if f.tried[n.ID] <= f.failures {
		return fmt.Errorf("inbox service unavailable (try %d)", f.tried[n.ID])
	}
	f.sent = append(f.sent, n.ID)
	return nil
}

type lines struct {
	mu   sync.Mutex
	errs []string
}

func (l *lines) Infof(string, ...any) {}
func (l *lines) Errorf(f string, a ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errs = append(l.errs, fmt.Sprintf(f, a...))
}

func queued(id, tenant string) *domain.QueuedNotification {
	return &domain.QueuedNotification{
		ID: id, TenantID: tenant, Event: domain.EventPayableHeld,
		RecipientType: domain.RecipientRole, RecipientID: "supervisor",
		Title: "held", Body: "because", ReferenceType: "producer_payable", ReferenceID: "PY1",
	}
}

// A message that fails to send is kept and tried again, and the tries are
// recorded.
//
// This is the whole reason for an outbox. Sending from the handler would have
// lost the message the moment notification-service was unreachable, and lost it
// silently: the hold would succeed and nobody would be told.
func TestAMessageThatFailsToSendIsKeptAndTriedAgain(t *testing.T) {
	store := newStore(queued("N1", "TN1"))
	sender := &flakySender{failures: 2}
	log := &lines{}
	d := New(store, sender, log, 0)

	for sweep := 1; sweep <= 3; sweep++ {
		delivered, failed := d.Once(context.Background())
		row := store.rows["N1"]
		switch sweep {
		case 1, 2:
			if delivered != 0 || failed != 1 {
				t.Errorf("sweep %d: delivered %d failed %d, want 0 and 1", sweep, delivered, failed)
			}
			if row.DeliveredAt != nil {
				t.Fatalf("sweep %d: marked delivered after a failed send", sweep)
			}
			if row.Attempts != sweep {
				t.Errorf("sweep %d: %d attempts recorded", sweep, row.Attempts)
			}
			if !strings.Contains(row.LastError, "unavailable") {
				t.Errorf("sweep %d: last_error %q does not say why", sweep, row.LastError)
			}
		case 3:
			if delivered != 1 || failed != 0 {
				t.Errorf("sweep 3: delivered %d failed %d, want 1 and 0", delivered, failed)
			}
			if row.DeliveredAt == nil {
				t.Fatal("sweep 3: the send succeeded and the row is not marked delivered, so it " +
					"will be sent again forever")
			}
			if row.Attempts != 3 {
				t.Errorf("sweep 3: %d attempts recorded, want 3 — the record of the tries is "+
					"the record of the outage", row.Attempts)
			}
		}
	}
	if len(sender.sent) != 1 {
		t.Errorf("the message was sent %d times", len(sender.sent))
	}
	// Failures are logged as a count per sweep, not a line per message.
	if len(log.errs) != 2 {
		t.Errorf("%d error lines for two failed sweeps: %v", len(log.errs), log.errs)
	}
}

// Marks run under the row's tenant.
//
// The sweep reads across tenants through a definer-rights function; the marks
// must not, because they go through the ordinary policies and a mark with no
// tenant on its context would be refused — or, worse, would need the policies
// loosened.
func TestMarksRunUnderTheRowsTenant(t *testing.T) {
	store := newStore(queued("N1", "TN_A"), queued("N2", "TN_B"))
	d := New(store, &flakySender{}, &lines{}, 0)
	d.Once(context.Background())
	for id, want := range map[string]string{"N1": "TN_A", "N2": "TN_B"} {
		if got := store.tenantSeen[id]; got != want {
			t.Errorf("%s was marked under tenant %q, want %q", id, got, want)
		}
	}
}

// With nothing to deliver to, the queue is reported every sweep, not once.
//
// The startup line saying NOTIFICATION_URL is unset is read once, by whoever
// deployed it, on the day. The queue keeps growing after that. So every sweep
// that finds messages owed says so, and nothing is marked — the rows stay for
// the day somebody configures a destination.
func TestWithNoSenderTheQueueIsReportedEverySweepAndNothingIsMarked(t *testing.T) {
	store := newStore(queued("N1", "TN1"), queued("N2", "TN1"))
	log := &lines{}
	d := New(store, nil, log, 0)

	for i := 0; i < 3; i++ {
		if delivered, failed := d.Once(context.Background()); delivered != 0 || failed != 0 {
			t.Errorf("sweep %d: delivered %d failed %d with no sender", i+1, delivered, failed)
		}
	}
	for id, r := range store.rows {
		if r.Attempts != 0 || r.DeliveredAt != nil {
			t.Errorf("%s was marked (%d attempts, delivered=%v) with nothing to deliver to",
				id, r.Attempts, r.DeliveredAt != nil)
		}
	}
	if len(log.errs) != 3 {
		t.Fatalf("%d error lines for three sweeps with two messages owed, want one per sweep: %v",
			len(log.errs), log.errs)
	}
	for _, l := range log.errs {
		if !strings.Contains(l, "2 messages") || !strings.Contains(l, "NOTIFICATION_URL") {
			t.Errorf("the line does not say how many are owed and what to set: %q", l)
		}
	}
}

// A store that cannot be read is a logged error and an empty sweep, not a crash.
type brokenStore struct{ fakeStore }

func (*brokenStore) NotificationsToDeliver(context.Context, int) ([]*domain.QueuedNotification, error) {
	return nil, errors.New("connection refused")
}

func TestAnUnreadableOutboxIsLoggedNotFatal(t *testing.T) {
	log := &lines{}
	d := New(&brokenStore{}, &flakySender{}, log, 0)
	if delivered, failed := d.Once(context.Background()); delivered != 0 || failed != 0 {
		t.Errorf("delivered %d failed %d from an unreadable outbox", delivered, failed)
	}
	if len(log.errs) != 1 || !strings.Contains(log.errs[0], "connection refused") {
		t.Errorf("the failure to read the outbox was not reported: %v", log.errs)
	}
}

// Kick never blocks, however many times it is called.
func TestKickNeverBlocks(t *testing.T) {
	d := New(newStore(), &flakySender{}, &lines{}, 0)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			d.Kick()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-contextDone(t):
		t.Fatal("Kick blocked")
	}
}

func contextDone(t *testing.T) <-chan struct{} {
	ctx, cancel := context.WithTimeout(context.Background(), 2e9)
	t.Cleanup(cancel)
	return ctx.Done()
}
