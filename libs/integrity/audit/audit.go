// Package audit records what was changed, in the same transaction that changed
// it.
//
// # Why not a call to the audit service
//
// The arrangement this replaces is that a service makes its change and then
// tells the audit service about it. Every failure between those two — a restart,
// a dropped connection, the audit service being down for a minute — leaves the
// change made and unrecorded, and nothing reports it, because from the caller's
// side the change succeeded.
//
// A trail with holes is worse than no trail. It is a complete-looking record of
// an incomplete set of events, and the missing events are not a random sample:
// the conditions that lose a record are the conditions under which things go
// wrong, so the gaps land exactly where somebody will later be looking.
//
// So Write takes the transaction the change is being made in. Either both land
// or neither does, and that is not a discipline anybody has to keep — it is what
// a transaction means.
//
// # Why the actor is not an argument
//
// It comes from the context, where the transport put it after the gateway
// verified the session. An argument is something a caller chooses, and a caller
// that can choose whose name goes on a change can put somebody else's there.
// Several of these procedures carry a created_by in the request body; that field
// is the client's claim about itself and is deliberately not what gets recorded.
//
// # What is refused
//
// A record that cannot say who did what to which thing is not written at all.
// The alternative — writing it with the missing parts blank — produces a trail
// that has rows for everything and answers for nothing, and that is harder to
// discover than a service that failed loudly the first time it was run.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ppusapati/gavya/libs/integrity/tracing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ppusapati/gavya/libs/integrity/tenantctx"
)

// Execer is the part of a transaction this package needs. pgx.Tx satisfies it,
// and so does a pool — though a pool is the wrong thing to pass, see Write.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// IDs generates the identifier for a record. Injected because every service in
// this platform already has one and they should not disagree.
type IDs interface{ New() string }

// Entry is one thing that happened.
type Entry struct {
	// Action is what was done, in the platform's own vocabulary rather than the
	// database's: "record_milk", not "insert". A trail written in table names
	// answers questions about the schema and not about the business.
	Action string

	// ResourceType and ResourceID are what it was done to.
	ResourceType string
	ResourceID   string

	// Before and After are the state either side of the change. Nil for the one
	// that does not apply: a creation has no before, a deletion has no after.
	// Marshalled to JSON, so a struct or a map both work.
	Before any
	After  any

	// ServiceName is which service made the change. Not derived from anything,
	// because a service that lies about this is already inside the trust
	// boundary and a service that forgets to set it should be visible.
	ServiceName string

	// TraceID ties this to the request that caused it.
	//
	// Leave it empty and the trace on the context is used, which is what almost
	// every caller wants and none of them had: this field existed from the
	// beginning and nothing ever set it, so every row in the trail carried an
	// empty trace_id. Set it only to say the entry belongs to a different
	// request from the one running — a sweep writing off yesterday's queue
	// belongs to the trace that queued the work, not to its own.
	TraceID string
}

// ErrIncomplete is returned for a record that cannot be attributed.
var ErrIncomplete = errors.New("audit: this record cannot say who did what")

// Write records an entry inside the caller's transaction.
//
// tx must be the transaction the change is being made in. Passing a pool
// compiles and produces a record written in its own transaction, which commits
// whether or not the change did — which is the failure this package exists to
// remove. There is no way for this function to tell the difference, so it is
// stated here rather than checked.
func Write(ctx context.Context, tx Execer, ids IDs, e Entry) error {
	tenant, err := tenantctx.From(ctx)
	if err != nil {
		return fmt.Errorf("%w: no tenant (%v)", ErrIncomplete, err)
	}
	actor, err := tenantctx.ActorFrom(ctx)
	if err != nil {
		return fmt.Errorf("%w: no actor (%v)", ErrIncomplete, err)
	}
	switch {
	case e.Action == "":
		return fmt.Errorf("%w: no action", ErrIncomplete)
	case e.ResourceType == "":
		return fmt.Errorf("%w: no resource type", ErrIncomplete)
	case e.ResourceID == "":
		return fmt.Errorf("%w: no resource id", ErrIncomplete)
	}

	before, err := encode(e.Before)
	if err != nil {
		return fmt.Errorf("audit: the state before the change could not be recorded: %w", err)
	}
	after, err := encode(e.After)
	if err != nil {
		return fmt.Errorf("audit: the state after the change could not be recorded: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO audit_logs
			(id, tenant_id, actor_id, actor_type, action, resource_type, resource_id,
			 old_value, new_value, service_name, trace_id, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,nullif($10,''),nullif($11,''),$3)`,
		ids.New(), tenant, actor.Identifier(), actor.Kind(),
		e.Action, e.ResourceType, e.ResourceID,
		before, after, e.ServiceName, traceFor(ctx, e))
	if err != nil {
		return fmt.Errorf("audit: %w", err)
	}
	return nil
}

// traceFor is the trace this entry belongs to.
//
// The field has been on Entry for as long as the trail has existed and nothing
// ever set it, so every audit row in this platform carried an empty trace_id —
// a column in the schema, a field in the domain model and on the wire format,
// describing nothing. It is taken from the context now, which is where the
// server middleware put it.
//
// An explicit TraceID on the entry still wins, for a caller that knows better
// than the context: a sweep processing yesterday's queue belongs to the trace of
// the work that queued it, not to its own.
func traceFor(ctx context.Context, e Entry) string {
	if e.TraceID != "" {
		return e.TraceID
	}
	return tracing.TraceIDFrom(ctx)
}

// encode renders a state for storage, or nothing when there is none.
//
// A nil state and a state that encodes to "null" are different things — a
// creation has no before, while a field explicitly set to null does — so nil
// becomes a SQL null and everything else becomes its JSON.
func encode(v any) (*string, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	s := string(b)
	return &s, nil
}
