// Package historyprojection projects rows from workflow.workflow_history
// (the unified engine's history table) into a generic HistoryEvent shape
// that consumer modules translate into their own legacy audit-log
// proto types.
//
// Why this package exists
// =======================
// Phase 6.A of the engine-unification plan (docs/BASE_DOMAIN_AUDITS.md):
// the legacy approval_actions and fb_audit_logs tables remain populated
// for pre-bridge approval rows but every new approval / form instance
// writes its history through the unified engine into workflow_history.
// Consumer modules (workflow.approval, workflow.formbuilder) need to
// serve their existing GetApprovalHistory RPCs without forcing every
// downstream caller to migrate to a new proto. This package gives them
// an indexed read path off workflow_history that they can translate to
// their legacy proto shape.
//
// Scope
// =====
// THIS package owns: reading the engine's workflow_history rows for a
// given execution_id, ordered chronologically, with details jsonb
// unmarshalled into a Go map. It also exposes a small helper for
// reading workflow_comments rows so consumers can decorate their
// projection with comment text without hand-rolling a second query.
//
// THIS package does NOT own: translating HistoryEvent into any
// consumer-specific proto. That belongs to the consumer because each
// legacy proto has different field names, enum mappings, and metadata
// columns. Centralising the translation here would couple this
// package to every consumer's proto package, which is the kind of
// coupling ports + adapters (ADR-0003) exists to prevent.
//
// Phase 6 design Q1: IP/UA are read from details["ip_address"] and
// details["user_agent"] (see executor.go:CompleteTask write path).
package historyprojection

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// HistoryEvent is the projected shape of a single workflow_history row.
// Consumers translate this to their legacy proto type via the helper
// methods below or by reading Details directly.
type HistoryEvent struct {
	// ID is the workflow_history row's primary key (ULID).
	ID string

	// ExecutionID is the workflow_executions row this event belongs to.
	ExecutionID string

	// EventType is the engine's semantic event name. Phase 6.A defines
	// these values:
	//   "execution_started"  — workflow began
	//   "task_created"       — a user task was materialized
	//   "task_completed"     — a user task was finished by an actor
	//   "element_completed"  — token left an element (BPMN-graph event)
	//   "execution_completed"— workflow reached a terminal end event
	// Consumers map these to their own action_type enum (e.g. APPROVE
	// when EventType=task_completed AND Details["variables"]["decision"]="approved").
	EventType string

	// ElementID is the BPMN element id this event is about (e.g. "review",
	// "approve_2"). Empty string when the event is not element-specific.
	ElementID string

	// ElementType is the engine type of the element ("user_task",
	// "exclusive_gateway", "end_event", ...). Useful for filtering
	// projections to only the events a consumer cares about.
	ElementType string

	// ElementName is the human-readable name set on the element in the
	// workflow definition.
	ElementName string

	// ActorID is the user who triggered the event. Empty when the event
	// was system-driven (token advancement, gateway evaluation).
	ActorID string

	// ActorType is "user" for human-triggered events, "system" for
	// engine-driven events.
	ActorType string

	// Details carries the event-specific payload. For task_completed
	// rows the engine writes:
	//   "taskId":      string
	//   "completedBy": string
	//   "variables":   map[string]any (the verb's completion variables)
	//   "ip_address":  string  (when present in request context)
	//   "user_agent":  string  (when present in request context)
	// Consumers extract whichever sub-fields their legacy proto needs.
	Details map[string]any

	// Timestamp is when the event was recorded.
	Timestamp time.Time
}

// Reader queries the engine's workflow_history table and returns
// projected events. It is safe for concurrent use; the underlying pool
// handles concurrency.
//
// Construction takes a *pgxpool.Pool directly (NOT a sqlc.RLSPool
// wrapper) because the projection is a read-only path that should
// participate in the rlsConn middleware's request-scoped transaction
// when present, and bypass it otherwise. See the rlsConnMiddleware
// documentation for the dispatch.
type Reader struct {
	pool *pgxpool.Pool
}

// NewReader builds a Reader against the given pool.
func NewReader(pool *pgxpool.Pool) *Reader {
	return &Reader{pool: pool}
}

// Events returns every workflow_history row for the given execution_id,
// ordered by timestamp ASC (chronological) and then by primary key
// (stable ordering for events recorded with the same timestamp). When
// executionID is empty an empty slice is returned without hitting the
// DB; callers may pass empty when they have not yet stashed an engine
// execution id on the legacy row's metadata.
//
// Errors propagate verbatim from pgx; callers decide whether to fall
// back to the legacy table or surface the error.
func (r *Reader) Events(ctx context.Context, executionID string) ([]*HistoryEvent, error) {
	if executionID == "" {
		return nil, nil
	}
	const query = `
SELECT id, execution_id, event_type, element_id, element_type, element_name,
       actor_id, actor_type, details, timestamp
FROM workflow.workflow_history
WHERE execution_id = $1 AND is_deleted = false
ORDER BY timestamp ASC, id ASC`
	rows, err := r.pool.Query(ctx, query, executionID)
	if err != nil {
		return nil, fmt.Errorf("historyprojection: query workflow_history: %w", err)
	}
	defer rows.Close()

	var out []*HistoryEvent
	for rows.Next() {
		ev, err := scanEvent(rows)
		if err != nil {
			return nil, fmt.Errorf("historyprojection: scan row: %w", err)
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("historyprojection: iterate rows: %w", err)
	}
	return out, nil
}

// EventsFilteredByType is a convenience that returns only events whose
// EventType matches one of the supplied values. Useful when a consumer
// only cares about task_completed rows (legacy approval_actions parity)
// and wants the database to do the filtering rather than allocating
// every row in Go.
func (r *Reader) EventsFilteredByType(ctx context.Context, executionID string, eventTypes ...string) ([]*HistoryEvent, error) {
	if executionID == "" {
		return nil, nil
	}
	if len(eventTypes) == 0 {
		// Empty filter degenerates to "all events" — same shape as
		// Events(). Easier than failing here and forcing the caller to
		// branch.
		return r.Events(ctx, executionID)
	}
	const query = `
SELECT id, execution_id, event_type, element_id, element_type, element_name,
       actor_id, actor_type, details, timestamp
FROM workflow.workflow_history
WHERE execution_id = $1 AND is_deleted = false AND event_type = ANY($2)
ORDER BY timestamp ASC, id ASC`
	rows, err := r.pool.Query(ctx, query, executionID, eventTypes)
	if err != nil {
		return nil, fmt.Errorf("historyprojection: query workflow_history (filtered): %w", err)
	}
	defer rows.Close()

	var out []*HistoryEvent
	for rows.Next() {
		ev, err := scanEvent(rows)
		if err != nil {
			return nil, fmt.Errorf("historyprojection: scan row: %w", err)
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("historyprojection: iterate filtered rows: %w", err)
	}
	return out, nil
}

// CommentRow is the projected shape of a single workflow_comments row.
// Used by consumers that want to decorate their history projection with
// the comment text written by the orchestrator's appendComment path.
type CommentRow struct {
	ID          string
	TaskID      string // empty when the comment is execution-level
	AuthorID    string
	Content     string
	Timestamp   time.Time
}

// CommentsForEvents collects task IDs from the given history events,
// deduplicates, and issues a single batch query against
// workflow_comments. Returns a map suitable for handlers that need to
// join comment text onto their legacy proto's remarks field.
//
// This helper exists at the package level (vs in each consuming
// handler) per the no-duplication golden rule: both
// workflow.approval.GetApprovalHistory and
// formbuilder.ApprovalService.GetApprovalHistory need the same dedupe
// + batch-fetch sequence. Returns nil with no error when:
//   - events is empty
//   - no event carries a taskID (e.g. system-driven events)
// Returns nil + the underlying error when the DB query fails — the
// caller decides whether to treat that as fatal or degrade
// gracefully.
func (r *Reader) CommentsForEvents(ctx context.Context, events []*HistoryEvent) (map[string][]*CommentRow, error) {
	if len(events) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(events))
	taskIDs := make([]string, 0, len(events))
	for _, ev := range events {
		taskID := TaskIDFromEvent(ev)
		if taskID == "" {
			continue
		}
		if _, ok := seen[taskID]; ok {
			continue
		}
		seen[taskID] = struct{}{}
		taskIDs = append(taskIDs, taskID)
	}
	if len(taskIDs) == 0 {
		return nil, nil
	}
	return r.CommentsByTask(ctx, taskIDs)
}

// CommentsByTask returns every workflow_comments row for the given
// task IDs, indexed by task_id. Used by consumers that want to attach
// the comment text to the legacy proto's "remarks" field.
//
// Returns an empty map when taskIDs is empty.
func (r *Reader) CommentsByTask(ctx context.Context, taskIDs []string) (map[string][]*CommentRow, error) {
	if len(taskIDs) == 0 {
		return map[string][]*CommentRow{}, nil
	}
	const query = `
SELECT id, COALESCE(task_id, '') AS task_id, author_id, content, created_at
FROM workflow.workflow_comments
WHERE task_id = ANY($1) AND is_deleted = false
ORDER BY created_at ASC, id ASC`
	rows, err := r.pool.Query(ctx, query, taskIDs)
	if err != nil {
		return nil, fmt.Errorf("historyprojection: query workflow_comments: %w", err)
	}
	defer rows.Close()

	out := make(map[string][]*CommentRow)
	for rows.Next() {
		var (
			c          CommentRow
			ts         interface{}
		)
		if err := rows.Scan(&c.ID, &c.TaskID, &c.AuthorID, &c.Content, &ts); err != nil {
			return nil, fmt.Errorf("historyprojection: scan comment: %w", err)
		}
		c.Timestamp = coerceTimestamp(ts)
		out[c.TaskID] = append(out[c.TaskID], &c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("historyprojection: iterate comments: %w", err)
	}
	return out, nil
}

// ============================================================
// Helpers exported for consumers that want to translate
// HistoryEvent → their legacy proto without hand-parsing details.
// ============================================================

// DecisionFromEvent extracts the orchestrator's decision string
// ("approved", "rejected", or "") from a task_completed event's details.
// The orchestrator writes the decision under
// details.variables.<decisionVar>; the variable name depends on the
// element id (see ApprovalTaskOrchestrator.decisionVariableFor). This
// helper inspects the variables map and returns the first non-empty
// decision-shaped string it finds. Returns "" when the event is not a
// task_completed, when there are no variables, or when no recognisable
// decision key is present.
//
// Recognised decision keys (matches the orchestrator's element-id
// inference):
//   - "decision"                 — review / approve_request / monetary
//   - "primary_decision"         — escalating_approval primary
//   - "escalation_decision"      — escalating_approval escalated
//   - "decision_<n>" (n=1..N)    — sequential / parallel approval
func DecisionFromEvent(ev *HistoryEvent) string {
	if ev == nil || ev.EventType != "task_completed" {
		return ""
	}
	rawVars, ok := ev.Details["variables"]
	if !ok {
		return ""
	}
	vars, ok := rawVars.(map[string]any)
	if !ok {
		return ""
	}
	// Prefer specific names first so parallel_approval's decision_1 vs
	// decision (from a single-decision template) is unambiguous.
	for _, key := range []string{"decision", "primary_decision", "escalation_decision"} {
		if v, ok := vars[key].(string); ok && v != "" {
			return v
		}
	}
	// decision_<n> — return the first non-empty decision; consumers
	// that want the per-stage breakdown should iterate variables
	// themselves.
	for k, v := range vars {
		if len(k) > len("decision_") && k[:len("decision_")] == "decision_" {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// IPAddressFromEvent returns the captured client IP from a
// task_completed event's details, or "" when not present. Phase 6.A
// design Q1: IP is stored in details["ip_address"].
func IPAddressFromEvent(ev *HistoryEvent) string {
	if ev == nil {
		return ""
	}
	if s, ok := ev.Details["ip_address"].(string); ok {
		return s
	}
	return ""
}

// UserAgentFromEvent returns the captured user agent from a
// task_completed event's details, or "" when not present.
func UserAgentFromEvent(ev *HistoryEvent) string {
	if ev == nil {
		return ""
	}
	if s, ok := ev.Details["user_agent"].(string); ok {
		return s
	}
	return ""
}

// TaskIDFromEvent returns the engine task id captured at completion
// time, or "" when not a task_completed event. Used to correlate
// history events with the workflow_comments rows for the same task.
func TaskIDFromEvent(ev *HistoryEvent) string {
	if ev == nil {
		return ""
	}
	if s, ok := ev.Details["taskId"].(string); ok {
		return s
	}
	return ""
}

// CommentForEvent returns the legacy "remarks" string the projected
// HistoryEvent should surface, joining workflow_comments rows by
// taskID. The orchestrator writes one comment per verb (Approve /
// Reject / ...) so the typical case is exactly one row; this helper
// concatenates with newlines when there are multiple, preserving
// every comment chronologically (Reader returns rows ordered by
// created_at ASC). Returns "" when:
//   - commentsByTask is nil (caller passed nil to suppress the join)
//   - the event has no taskID (system-driven event)
//   - no comment row exists for the taskID
func CommentForEvent(ev *HistoryEvent, commentsByTask map[string][]*CommentRow) string {
	if commentsByTask == nil {
		return ""
	}
	taskID := TaskIDFromEvent(ev)
	if taskID == "" {
		return ""
	}
	rows := commentsByTask[taskID]
	if len(rows) == 0 {
		return ""
	}
	if len(rows) == 1 {
		return rows[0].Content
	}
	out := rows[0].Content
	for i := 1; i < len(rows); i++ {
		out += "\n" + rows[i].Content
	}
	return out
}

// ============================================================
// internals
// ============================================================

func scanEvent(rows pgx.Rows) (*HistoryEvent, error) {
	var (
		id, execID, eventType, actorType                     string
		elementID, elementType, elementName, actorID         *string
		detailsJSON                                          []byte
		timestampRaw                                         interface{}
	)
	if err := rows.Scan(&id, &execID, &eventType, &elementID, &elementType, &elementName, &actorID, &actorType, &detailsJSON, &timestampRaw); err != nil {
		return nil, err
	}
	ev := &HistoryEvent{
		ID:          id,
		ExecutionID: execID,
		EventType:   eventType,
		ElementID:   derefString(elementID),
		ElementType: derefString(elementType),
		ElementName: derefString(elementName),
		ActorID:     derefString(actorID),
		ActorType:   actorType,
		Timestamp:   coerceTimestamp(timestampRaw),
	}
	if len(detailsJSON) > 0 {
		var d map[string]any
		if err := json.Unmarshal(detailsJSON, &d); err == nil {
			ev.Details = d
		}
	}
	if ev.Details == nil {
		ev.Details = map[string]any{}
	}
	return ev, nil
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// coerceTimestamp accepts the various timestamp shapes pgx hands back
// (time.Time, pgtype.Timestamptz embedded values, etc) and returns a
// canonical time.Time. Falls back to zero-value when the input shape
// is unexpected; the caller can detect that with .IsZero() if needed.
func coerceTimestamp(v interface{}) time.Time {
	switch t := v.(type) {
	case time.Time:
		return t
	case *time.Time:
		if t != nil {
			return *t
		}
	}
	return time.Time{}
}
