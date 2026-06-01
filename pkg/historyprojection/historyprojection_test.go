package historyprojection

import (
	"testing"
	"time"
)

// These tests pin the projection helpers (DecisionFromEvent +
// IPAddressFromEvent + UserAgentFromEvent + TaskIDFromEvent) since
// those are the load-bearing part consumers depend on for legacy
// proto translation. The DB-touching parts (Reader.Events,
// Reader.EventsFilteredByType, Reader.CommentsByTask) are exercised
// by the live regression harness in app/cmd/cutover-probe + the live
// GetApprovalHistory probes in Phase 6.A.8.

func TestDecisionFromEvent_ApprovedSimpleDecision(t *testing.T) {
	ev := &HistoryEvent{
		EventType: "task_completed",
		Details: map[string]any{
			"variables": map[string]any{
				"decision": "approved",
			},
		},
	}
	if got := DecisionFromEvent(ev); got != "approved" {
		t.Fatalf("DecisionFromEvent = %q, want \"approved\"", got)
	}
}

func TestDecisionFromEvent_RejectedSimpleDecision(t *testing.T) {
	ev := &HistoryEvent{
		EventType: "task_completed",
		Details: map[string]any{
			"variables": map[string]any{
				"decision": "rejected",
			},
		},
	}
	if got := DecisionFromEvent(ev); got != "rejected" {
		t.Fatalf("DecisionFromEvent = %q, want \"rejected\"", got)
	}
}

func TestDecisionFromEvent_PreferenceOrder(t *testing.T) {
	// When multiple decision keys are present (theoretical edge case
	// — a buggy template that writes both), the canonical names take
	// precedence over decision_<n>.
	ev := &HistoryEvent{
		EventType: "task_completed",
		Details: map[string]any{
			"variables": map[string]any{
				"decision":   "approved",
				"decision_1": "rejected",
			},
		},
	}
	if got := DecisionFromEvent(ev); got != "approved" {
		t.Fatalf("DecisionFromEvent = %q, want \"approved\" (canonical name wins)", got)
	}
}

func TestDecisionFromEvent_PrimaryDecision(t *testing.T) {
	// escalating_approval template uses primary_decision on the
	// approve_primary task.
	ev := &HistoryEvent{
		EventType: "task_completed",
		Details: map[string]any{
			"variables": map[string]any{
				"primary_decision": "approved",
			},
		},
	}
	if got := DecisionFromEvent(ev); got != "approved" {
		t.Fatalf("DecisionFromEvent = %q, want \"approved\" (primary_decision)", got)
	}
}

func TestDecisionFromEvent_EscalationDecision(t *testing.T) {
	ev := &HistoryEvent{
		EventType: "task_completed",
		Details: map[string]any{
			"variables": map[string]any{
				"escalation_decision": "rejected",
			},
		},
	}
	if got := DecisionFromEvent(ev); got != "rejected" {
		t.Fatalf("DecisionFromEvent = %q, want \"rejected\" (escalation_decision)", got)
	}
}

func TestDecisionFromEvent_StagedDecision(t *testing.T) {
	// sequential_approval template uses decision_<n> per stage.
	ev := &HistoryEvent{
		EventType: "task_completed",
		Details: map[string]any{
			"variables": map[string]any{
				"decision_2": "approved",
			},
		},
	}
	if got := DecisionFromEvent(ev); got != "approved" {
		t.Fatalf("DecisionFromEvent = %q, want \"approved\" (decision_2)", got)
	}
}

func TestDecisionFromEvent_NotTaskCompletedEvent(t *testing.T) {
	// element_completed and other non-task-completed events have no
	// orchestrator decision; helper must return "" not panic.
	ev := &HistoryEvent{
		EventType: "element_completed",
		Details: map[string]any{
			"variables": map[string]any{"decision": "approved"},
		},
	}
	if got := DecisionFromEvent(ev); got != "" {
		t.Fatalf("DecisionFromEvent = %q, want \"\" (only task_completed yields a decision)", got)
	}
}

func TestDecisionFromEvent_NilEvent(t *testing.T) {
	if got := DecisionFromEvent(nil); got != "" {
		t.Fatalf("DecisionFromEvent(nil) = %q, want \"\"", got)
	}
}

func TestDecisionFromEvent_NoVariables(t *testing.T) {
	ev := &HistoryEvent{EventType: "task_completed", Details: map[string]any{}}
	if got := DecisionFromEvent(ev); got != "" {
		t.Fatalf("DecisionFromEvent (no variables) = %q, want \"\"", got)
	}
}

func TestDecisionFromEvent_VariablesWrongShape(t *testing.T) {
	// Defensive: the engine writes variables as map[string]any; if a
	// future schema-change writes it as []any or string, the helper
	// must return "" not panic.
	ev := &HistoryEvent{
		EventType: "task_completed",
		Details:   map[string]any{"variables": "not a map"},
	}
	if got := DecisionFromEvent(ev); got != "" {
		t.Fatalf("DecisionFromEvent (wrong-shape variables) = %q, want \"\"", got)
	}
}

func TestDecisionFromEvent_DecisionWithEmptyValue(t *testing.T) {
	// An explicit empty string is treated as "no decision" — the
	// helper must NOT return "" here as if the key were absent; check
	// that decision keys with empty values fall through to the next
	// candidate (decision_<n>).
	ev := &HistoryEvent{
		EventType: "task_completed",
		Details: map[string]any{
			"variables": map[string]any{
				"decision":   "",
				"decision_1": "approved",
			},
		},
	}
	if got := DecisionFromEvent(ev); got != "approved" {
		t.Fatalf("DecisionFromEvent = %q, want \"approved\" (empty canonical falls through to staged)", got)
	}
}

func TestIPAddressFromEvent_Present(t *testing.T) {
	ev := &HistoryEvent{Details: map[string]any{"ip_address": "10.0.0.5"}}
	if got := IPAddressFromEvent(ev); got != "10.0.0.5" {
		t.Fatalf("IPAddressFromEvent = %q, want \"10.0.0.5\"", got)
	}
}

func TestIPAddressFromEvent_Missing(t *testing.T) {
	ev := &HistoryEvent{Details: map[string]any{}}
	if got := IPAddressFromEvent(ev); got != "" {
		t.Fatalf("IPAddressFromEvent (missing) = %q, want \"\"", got)
	}
}

func TestIPAddressFromEvent_NilEvent(t *testing.T) {
	if got := IPAddressFromEvent(nil); got != "" {
		t.Fatalf("IPAddressFromEvent(nil) = %q, want \"\"", got)
	}
}

func TestUserAgentFromEvent_Present(t *testing.T) {
	ev := &HistoryEvent{Details: map[string]any{"user_agent": "TestAgent/1.0"}}
	if got := UserAgentFromEvent(ev); got != "TestAgent/1.0" {
		t.Fatalf("UserAgentFromEvent = %q, want \"TestAgent/1.0\"", got)
	}
}

func TestTaskIDFromEvent_Present(t *testing.T) {
	ev := &HistoryEvent{Details: map[string]any{"taskId": "01ABC..."}}
	if got := TaskIDFromEvent(ev); got != "01ABC..." {
		t.Fatalf("TaskIDFromEvent = %q, want \"01ABC...\"", got)
	}
}

func TestTaskIDFromEvent_Missing(t *testing.T) {
	ev := &HistoryEvent{Details: map[string]any{}}
	if got := TaskIDFromEvent(ev); got != "" {
		t.Fatalf("TaskIDFromEvent (missing) = %q, want \"\"", got)
	}
}

// CommentForEvent — Bucket 2.1 of Phase 6 — joins workflow_comments
// rows onto a HistoryEvent by taskID. Tests pin every branch.

func TestCommentForEvent_NilMapReturnsEmpty(t *testing.T) {
	ev := &HistoryEvent{Details: map[string]any{"taskId": "t1"}}
	if got := CommentForEvent(ev, nil); got != "" {
		t.Fatalf("CommentForEvent(_, nil) = %q, want \"\"", got)
	}
}

func TestCommentForEvent_NoTaskIDReturnsEmpty(t *testing.T) {
	ev := &HistoryEvent{Details: map[string]any{}}
	commentsByTask := map[string][]*CommentRow{
		"t1": {{Content: "looks good"}},
	}
	if got := CommentForEvent(ev, commentsByTask); got != "" {
		t.Fatalf("CommentForEvent (no taskID) = %q, want \"\"", got)
	}
}

func TestCommentForEvent_TaskIDAbsentInMap(t *testing.T) {
	ev := &HistoryEvent{Details: map[string]any{"taskId": "t1"}}
	commentsByTask := map[string][]*CommentRow{
		"other-task": {{Content: "irrelevant"}},
	}
	if got := CommentForEvent(ev, commentsByTask); got != "" {
		t.Fatalf("CommentForEvent (taskID not in map) = %q, want \"\"", got)
	}
}

func TestCommentForEvent_SingleComment(t *testing.T) {
	ev := &HistoryEvent{Details: map[string]any{"taskId": "t1"}}
	commentsByTask := map[string][]*CommentRow{
		"t1": {{Content: "looks good"}},
	}
	if got := CommentForEvent(ev, commentsByTask); got != "looks good" {
		t.Fatalf("CommentForEvent (single) = %q, want \"looks good\"", got)
	}
}

func TestCommentForEvent_MultipleCommentsConcatenated(t *testing.T) {
	ev := &HistoryEvent{Details: map[string]any{"taskId": "t1"}}
	commentsByTask := map[string][]*CommentRow{
		"t1": {
			{Content: "first"},
			{Content: "second"},
			{Content: "third"},
		},
	}
	want := "first\nsecond\nthird"
	if got := CommentForEvent(ev, commentsByTask); got != want {
		t.Fatalf("CommentForEvent (multi) = %q, want %q", got, want)
	}
}

func TestCommentForEvent_EmptyContentPreserved(t *testing.T) {
	// Defensive: a row with empty Content must NOT collapse to "no
	// comment" — it might be a deliberately blank action recorded.
	// The helper returns the content as-is (single-row case = "")
	// which the consumer can choose to ignore upstream.
	ev := &HistoryEvent{Details: map[string]any{"taskId": "t1"}}
	commentsByTask := map[string][]*CommentRow{
		"t1": {{Content: ""}},
	}
	if got := CommentForEvent(ev, commentsByTask); got != "" {
		t.Fatalf("CommentForEvent (empty single) = %q, want \"\"", got)
	}
}

// HistoryEvent is a value-only struct; no pointer receivers; this test
// confirms the timestamp-coerce helper handles the shapes pgx returns.
func TestCoerceTimestamp(t *testing.T) {
	now := time.Now().UTC()

	// Direct time.Time
	if got := coerceTimestamp(now); !got.Equal(now) {
		t.Errorf("coerceTimestamp(time.Time) = %v, want %v", got, now)
	}
	// Pointer to time.Time (some pgx scan paths)
	if got := coerceTimestamp(&now); !got.Equal(now) {
		t.Errorf("coerceTimestamp(*time.Time) = %v, want %v", got, now)
	}
	// Nil pointer
	if got := coerceTimestamp((*time.Time)(nil)); !got.IsZero() {
		t.Errorf("coerceTimestamp(nil *time.Time) = %v, want zero", got)
	}
	// Unexpected type
	if got := coerceTimestamp("not a time"); !got.IsZero() {
		t.Errorf("coerceTimestamp(string) = %v, want zero", got)
	}
}
