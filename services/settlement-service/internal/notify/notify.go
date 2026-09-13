// Package notify delivers what the outbox owes.
//
// The outbox is written in the transaction that makes a change; this is the
// other half, which reads what is due and hands it to notification-service. The
// two halves are separate on purpose: a change must never fail because the
// inbox service is down, and a message must never be lost because it was.
package notify

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
	"github.com/ppusapati/gavya/libs/integrity/tenantdb"

	"github.com/ppusapati/gavya/services/settlement-service/internal/domain"
)

// Sender hands one message to whatever holds inboxes.
type Sender interface {
	Send(ctx context.Context, n *domain.QueuedNotification) error
}

// Store is the outbox.
type Store interface {
	NotificationsToDeliver(ctx context.Context, limit int) ([]*domain.QueuedNotification, error)
	NotificationDelivered(ctx context.Context, id string) error
	NotificationFailed(ctx context.Context, id, reason string) error
}

// Logger is the two lines this package writes.
type Logger interface {
	Infof(string, ...any)
	Errorf(string, ...any)
}

// Batch is how many rows one sweep takes.
const Batch = 200

// Dispatcher sweeps the outbox and delivers what is due.
type Dispatcher struct {
	store    Store
	sender   Sender
	log      Logger
	interval time.Duration
	kick     chan struct{}
}

// New builds a dispatcher.
//
// sender may be nil, for a service started without a notification-service to
// deliver to. It still runs: each sweep then counts what is owed and says so,
// because an outbox that fills in silence is a queue nobody knows about.
func New(store Store, sender Sender, log Logger, interval time.Duration) *Dispatcher {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &Dispatcher{
		store: store, sender: sender, log: log, interval: interval,
		kick: make(chan struct{}, 1),
	}
}

// Kick asks for a sweep now rather than at the next tick.
//
// Called after a commit, so a hold is in the inbox in milliseconds rather than
// within the interval. Non-blocking, and collapses: a hundred commits in a
// second are one sweep, which delivers all of them.
func (d *Dispatcher) Kick() {
	select {
	case d.kick <- struct{}{}:
	default:
	}
}

// Run sweeps until the context ends.
func (d *Dispatcher) Run(ctx context.Context) {
	tick := time.NewTicker(d.interval)
	defer tick.Stop()
	for {
		d.Once(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-d.kick:
		}
	}
}

// Once performs one sweep and reports how many were delivered and how many
// failed.
func (d *Dispatcher) Once(ctx context.Context) (delivered, failed int) {
	due, err := d.store.NotificationsToDeliver(ctx, Batch)
	if err != nil {
		d.log.Errorf("notifications: reading the outbox: %v", err)
		return 0, 0
	}
	if len(due) == 0 {
		return 0, 0
	}
	if d.sender == nil {
		// Said every sweep rather than once at startup, because the startup
		// line is read once and the queue keeps growing.
		d.log.Errorf("notifications: %d messages are owed and this service has nothing to "+
			"deliver them to; set NOTIFICATION_URL", len(due))
		return 0, 0
	}

	for _, n := range due {
		// Under the row's tenant, for the policies. The sweep read across
		// tenants through the definer-rights function; from here on this is an
		// ordinary tenant-scoped write.
		tctx := tenantdb.WithTenant(ctx, n.TenantID)
		if err := d.sender.Send(tctx, n); err != nil {
			failed++
			if ferr := d.store.NotificationFailed(tctx, n.ID, err.Error()); ferr != nil {
				d.log.Errorf("notifications: %s failed (%v) and recording that failed too: %v",
					n.ID, err, ferr)
			}
			// One line per failure is a log that becomes unreadable the moment
			// notification-service is down; the count at the end says it once.
			continue
		}
		if err := d.store.NotificationDelivered(tctx, n.ID); err != nil {
			// Delivered and not recorded means it will be delivered again. That
			// is the right side to err on — a duplicate message is a nuisance,
			// a lost one is the failure this exists to prevent — and it is
			// worth a line.
			d.log.Errorf("notifications: %s was delivered and could not be marked so; it "+
				"will be sent again: %v", n.ID, err)
			continue
		}
		delivered++
	}
	if failed > 0 {
		d.log.Errorf("notifications: %d of %d due messages could not be delivered and will "+
			"be retried", failed, len(due))
	}
	return delivered, failed
}

// ---------------------------------------------------------------------------
// The sender over notification-service
// ---------------------------------------------------------------------------

const (
	serviceName = "notification.v1.NotificationService"
	send        = "/" + serviceName + "/SendNotification"
)

// Options builds the per-call options for one tenant, the way every outbound
// call from this service does.
type Options func(ctx context.Context, tenantID string) func() svcclient.CallOptions

// Client delivers to notification-service.
type Client struct {
	svc  *svcclient.Client
	opts Options
}

// NewClient wraps an already-configured client.
func NewClient(c *svcclient.Client, opts Options) *Client {
	return &Client{svc: c, opts: opts}
}

type sendRequest struct {
	TenantID      string `json:"tenant_id"`
	RecipientID   string `json:"recipient_id"`
	RecipientType string `json:"recipient_type"`
	Channel       string `json:"channel"`
	Title         string `json:"title"`
	Body          string `json:"body"`
	Priority      string `json:"priority"`
	ReferenceID   string `json:"reference_id"`
	ReferenceType string `json:"reference_type"`
	CreatedBy     string `json:"created_by"`
}

type sendResponse struct {
	Notification *struct {
		ID string `json:"id"`
	} `json:"notification"`
}

// ErrNotAccepted says notification-service answered without a notification.
var ErrNotAccepted = errors.New("notification-service answered without a notification")

// Send delivers one message.
func (c *Client) Send(ctx context.Context, n *domain.QueuedNotification) error {
	resp, err := svcclient.Call[sendRequest, sendResponse](ctx, c.svc, send, sendRequest{
		TenantID:      n.TenantID,
		RecipientID:   n.RecipientID,
		RecipientType: n.RecipientType,
		Channel:       "in_app",
		Title:         n.Title,
		Body:          n.Body,
		Priority:      priorityOf(n.Event),
		ReferenceID:   n.ReferenceID,
		ReferenceType: n.ReferenceType,
		CreatedBy:     n.CreatedBy,
	}, c.opts(ctx, n.TenantID)())
	if err != nil {
		return fmt.Errorf("send to notification-service: %w", err)
	}
	if resp.Notification == nil || resp.Notification.ID == "" {
		return ErrNotAccepted
	}
	return nil
}

// priorityOf marks a hold as the one that needs somebody today.
func priorityOf(e domain.NotificationEvent) string {
	if e == domain.EventPayableHeld {
		return "high"
	}
	return "normal"
}
