package domain

import "time"

// Telling somebody.
//
// notification-service has existed for as long as this platform has, with eight
// routes and an inbox per recipient, and nothing anywhere called it. A payable
// was held, a fortnight approved, money marked as paid — and the only way to
// learn any of it was to go and look. This is the record of what settlement owes
// somebody a message about, kept here until it has actually been delivered.
//
// # WHO IS TOLD
//
// Not the producer. A payable names a producer_ref, which is the society's own
// member code or an imported one, and nothing in this platform links either to
// a person who can sign in. Inventing that link here would be inventing it. The
// people who use this platform today are the society's staff, so the recipients
// are roles: a notification addressed to "accountant" is for everyone in the
// tenant who holds that role, and an inbox is read by asking for the roles one
// holds.
//
// Which roles, per event, is a starter routing in RecipientsFor, in the same
// spirit as the roles themselves: the shape worth keeping is that a hold reaches
// the people who can resolve it and money leaving reaches the people whose job
// is to see it leave. Everything else is a detail somebody closer to the
// business should correct.

// NotificationEvent is what happened.
type NotificationEvent string

const (
	EventPayableHeld      NotificationEvent = "payable_held"
	EventPayableApproved  NotificationEvent = "payable_approved"
	EventPayablePaid      NotificationEvent = "payable_paid"
	EventCycleApproved    NotificationEvent = "cycle_approved"
	EventAdjustmentRaised NotificationEvent = "adjustment_raised"
)

// RecipientRole is the recipient_type under which a role, rather than a person,
// is addressed.
const RecipientRole = "role"

// RecipientsFor is the starter routing: which roles are told about what.
//
//   - A hold is a decision about one member's money that somebody has to act
//     on. The supervisor decides disputed things and the accountant owns the
//     money, so both are told.
//   - Everything else is money moving or about to move, and the auditor is the
//     role whose whole job is to see that. Not the accountant: the accountant is
//     the one who did it, and telling somebody what they just did is noise.
func RecipientsFor(e NotificationEvent) []string {
	switch e {
	case EventPayableHeld:
		return []string{"supervisor", "accountant"}
	case EventPayableApproved, EventPayablePaid, EventCycleApproved, EventAdjustmentRaised:
		return []string{"auditor"}
	}
	return nil
}

// QueuedNotification is one message owed to one recipient, and whether it has
// been delivered.
//
// It is written in the same transaction as the change it describes, so a hold
// that commits has a message queued and a hold that rolls back has none. It is
// delivered afterwards, separately, and stays here until that succeeds — which
// is what makes a notification-service outage a delay rather than a message
// nobody ever gets.
type QueuedNotification struct {
	ID       string
	TenantID string
	Event    NotificationEvent

	RecipientType string
	RecipientID   string

	Title string
	Body  string

	ReferenceType string
	ReferenceID   string

	CreatedAt time.Time
	CreatedBy string

	// Attempts and LastError are the delivery record. DeliveredAt is set once,
	// when notification-service accepted it.
	Attempts    int
	LastError   string
	DeliveredAt *time.Time
}

// Delivered reports whether this has reached notification-service.
func (n QueuedNotification) Delivered() bool { return n.DeliveredAt != nil }
