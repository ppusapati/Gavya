//go:build e2e

// What the platform knew, and when it knew it.
//
// This is the claim the whole integrity layer rests on, and until now it was a
// claim about columns rather than a demonstration. A settlement is recomputed
// from the readings; a reading is corrected; the settlement changes. Somebody
// then asks the only question that matters in that conversation — "on what basis
// did you pay me that, and has it changed since?" — and answering it requires
// the platform to distinguish two different things that a single timestamp
// cannot:
//
//   - when a fact was true of the world (valid time), and
//   - when the platform came to believe it (transaction time).
//
// A correction to a fat reading does not change when the milk was collected. It
// changes what we believe the fat was, from now on, about a moment that has
// already passed. A system with one timestamp either loses the original reading
// or misreports when the milk arrived, and both make the settlement
// indefensible.
//
// So these tests record a reading, correct it, and then ask what was believed
// before and after the correction. The original must still be readable, the
// correction must not move the collection, and the two answers must differ.
package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

type subjectProto struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type originProto struct {
	Kind           string `json:"kind"`
	SourceSystemID string `json:"source_system_id,omitempty"`
	SourceRecordID string `json:"source_record_id,omitempty"`
}

type recordObservationReq struct {
	TenantID  string       `json:"tenant_id"`
	Subject   subjectProto `json:"subject"`
	Quantity  string       `json:"quantity_kind"`
	Value     float64      `json:"value"`
	Origin    originProto  `json:"origin"`
	ValidFrom string       `json:"valid_from"`
	Corrects  string       `json:"corrects,omitempty"`
	CreatedBy string       `json:"created_by"`
}

type observationProto struct {
	ID           string  `json:"id"`
	Value        float64 `json:"value"`
	ValidFrom    string  `json:"valid_from"`
	ValidTo      string  `json:"valid_to,omitempty"`
	RecordedAt   string  `json:"recorded_at,omitempty"`
	SupersededAt string  `json:"superseded_at,omitempty"`
	SupersededBy string  `json:"superseded_by,omitempty"`
	Supersedes   string  `json:"supersedes,omitempty"`
}

type recordObservationResp struct {
	Observation *observationProto `json:"observation"`
}

type listForSubjectReq struct {
	TenantID string       `json:"tenant_id"`
	Subject  subjectProto `json:"subject"`
	Quantity string       `json:"quantity_kind,omitempty"`
	ValidAt  string       `json:"valid_at,omitempty"`
	AsOf     string       `json:"as_of,omitempty"`
	Limit    int32        `json:"limit,omitempty"`
}

type listForSubjectResp struct {
	Observations []*observationProto `json:"observations"`
}

const observationSvc = "observation.v1.ObservationService"

func record(t *testing.T, p *platform, in recordObservationReq) *observationProto {
	t.Helper()
	resp, err := svcclient.Call[recordObservationReq, recordObservationResp](
		context.Background(), p.observation(), observationSvc+"/RecordObservation", in, p.opts())
	if err != nil {
		t.Fatalf("record observation: %v", err)
	}
	if resp.Observation == nil {
		t.Fatal("no observation came back")
	}
	return resp.Observation
}

func listAsOf(t *testing.T, p *platform, subject subjectProto, asOf string) []*observationProto {
	t.Helper()
	resp, err := svcclient.Call[listForSubjectReq, listForSubjectResp](
		context.Background(), p.observation(), observationSvc+"/ListObservationsForSubject",
		listForSubjectReq{
			TenantID: p.tenant, Subject: subject, Quantity: "FAT_PERCENT",
			AsOf: asOf, Limit: 50,
		}, p.opts())
	if err != nil {
		t.Fatalf("list as of %q: %v", asOf, err)
	}
	return resp.Observations
}

// A correction changes what is believed, not when the milk arrived, and both
// readings stay readable afterwards.
func TestACorrectionChangesWhatIsBelievedAndNotWhenItHappened(t *testing.T) {
	p := startPlatform(t)
	subject := subjectProto{Kind: "CATTLE", ID: newID("cat")}
	collected := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Second)

	original := record(t, p, recordObservationReq{
		TenantID: p.tenant, Subject: subject, Quantity: "FAT_PERCENT", Value: 4.15,
		Origin:    originProto{Kind: "NATIVE"},
		ValidFrom: collected.Format(time.RFC3339),
		CreatedBy: "operator-1",
	})

	// A moment between the two, so "before the correction" is a real instant
	// rather than an assumption about clock resolution.
	betweenAt := time.Now().UTC().Add(time.Second)
	time.Sleep(1100 * time.Millisecond)

	corrected := record(t, p, recordObservationReq{
		TenantID: p.tenant, Subject: subject, Quantity: "FAT_PERCENT", Value: 3.95,
		Origin:    originProto{Kind: "NATIVE"},
		ValidFrom: collected.Format(time.RFC3339),
		Corrects:  original.ID,
		CreatedBy: "operator-2",
	})

	if corrected.Supersedes != original.ID {
		t.Errorf("the correction says it supersedes %q, want %q", corrected.Supersedes, original.ID)
	}

	// The correction is about the same moment in the world. Moving valid_from
	// would say the milk arrived when the correction was typed.
	if corrected.ValidFrom != original.ValidFrom {
		t.Errorf("the correction moved the collection from %s to %s; a correction changes what "+
			"is believed, not when it happened", original.ValidFrom, corrected.ValidFrom)
	}

	// What we knew before the correction.
	before := listAsOf(t, p, subject, betweenAt.Format(time.RFC3339))
	if len(before) != 1 {
		t.Fatalf("as of %s there were %d readings, want the original alone",
			betweenAt.Format(time.RFC3339), len(before))
	}
	if before[0].Value != 4.15 {
		t.Errorf("as of before the correction the reading was %v, want the original 4.15", before[0].Value)
	}

	// And what we know now.
	now := listAsOf(t, p, subject, "")
	if len(now) != 1 {
		t.Fatalf("%d current readings, want the correction alone", len(now))
	}
	if now[0].Value != 3.95 {
		t.Errorf("the current reading is %v, want the corrected 3.95", now[0].Value)
	}

	if before[0].Value == now[0].Value {
		t.Error("the two answers are the same, so nothing here demonstrates anything")
	}
}

// The superseded reading is still there. A platform that deleted it could not
// answer what a past settlement was computed from, which is the question this
// exists to answer.
func TestTheSupersededReadingIsStillReadable(t *testing.T) {
	p := startPlatform(t)
	subject := subjectProto{Kind: "CATTLE", ID: newID("cat")}
	collected := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Second)

	original := record(t, p, recordObservationReq{
		TenantID: p.tenant, Subject: subject, Quantity: "FAT_PERCENT", Value: 4.40,
		Origin: originProto{Kind: "NATIVE"}, ValidFrom: collected.Format(time.RFC3339),
		CreatedBy: "operator-1",
	})
	time.Sleep(1100 * time.Millisecond)
	_ = record(t, p, recordObservationReq{
		TenantID: p.tenant, Subject: subject, Quantity: "FAT_PERCENT", Value: 4.10,
		Origin: originProto{Kind: "NATIVE"}, ValidFrom: collected.Format(time.RFC3339),
		Corrects: original.ID, CreatedBy: "operator-2",
	})

	got, err := svcclient.Call[getObservationReq, getObservationResp](
		context.Background(), p.observation(), observationSvc+"/GetObservation",
		getObservationReq{TenantID: p.tenant, ID: original.ID}, p.opts())
	if err != nil {
		t.Fatalf("the superseded reading could not be fetched: %v", err)
	}
	if got.Observation == nil {
		t.Fatal("the superseded reading is gone")
	}
	if got.Observation.Value != 4.40 {
		t.Errorf("the superseded reading now reads %v; it was edited rather than superseded",
			got.Observation.Value)
	}
	if got.Observation.SupersededAt == "" {
		t.Error("the superseded reading does not say when it stopped being believed")
	}
	if got.Observation.SupersededBy == "" {
		t.Error("the superseded reading does not say what replaced it")
	}
}

type getObservationReq struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
}

type getObservationResp struct {
	Observation *observationProto `json:"observation"`
}

// Asking as of a moment before anything was recorded must return nothing rather
// than the current reading. This is the one that fails if as_of is accepted and
// then ignored — which looks identical to a working implementation on every
// query where the answer has not changed.
func TestAsOfBeforeAnythingWasRecordedReturnsNothing(t *testing.T) {
	p := startPlatform(t)
	subject := subjectProto{Kind: "CATTLE", ID: newID("cat")}
	collected := time.Now().UTC().Add(-72 * time.Hour).Truncate(time.Second)

	before := time.Now().UTC().Add(-time.Hour)
	_ = record(t, p, recordObservationReq{
		TenantID: p.tenant, Subject: subject, Quantity: "FAT_PERCENT", Value: 4.15,
		Origin: originProto{Kind: "NATIVE"}, ValidFrom: collected.Format(time.RFC3339),
		CreatedBy: "operator-1",
	})

	got := listAsOf(t, p, subject, before.Format(time.RFC3339))
	if len(got) != 0 {
		t.Errorf("as of an hour before it was recorded, the platform reports %d readings — "+
			"as_of is being accepted and ignored", len(got))
	}
}

// Two corrections in a row leave one current reading and a readable history, not
// a fork. A chain that branches cannot answer what was believed at a moment,
// because there would be two answers.
func TestCorrectingACorrectionLeavesOneCurrentReading(t *testing.T) {
	p := startPlatform(t)
	subject := subjectProto{Kind: "CATTLE", ID: newID("cat")}
	collected := time.Now().UTC().Add(-12 * time.Hour).Truncate(time.Second)

	first := record(t, p, recordObservationReq{
		TenantID: p.tenant, Subject: subject, Quantity: "FAT_PERCENT", Value: 4.15,
		Origin: originProto{Kind: "NATIVE"}, ValidFrom: collected.Format(time.RFC3339),
		CreatedBy: "operator-1",
	})
	time.Sleep(1100 * time.Millisecond)
	second := record(t, p, recordObservationReq{
		TenantID: p.tenant, Subject: subject, Quantity: "FAT_PERCENT", Value: 3.95,
		Origin: originProto{Kind: "NATIVE"}, ValidFrom: collected.Format(time.RFC3339),
		Corrects: first.ID, CreatedBy: "operator-2",
	})
	time.Sleep(1100 * time.Millisecond)
	third := record(t, p, recordObservationReq{
		TenantID: p.tenant, Subject: subject, Quantity: "FAT_PERCENT", Value: 4.02,
		Origin: originProto{Kind: "NATIVE"}, ValidFrom: collected.Format(time.RFC3339),
		Corrects: second.ID, CreatedBy: "operator-3",
	})

	now := listAsOf(t, p, subject, "")
	if len(now) != 1 {
		var vals []float64
		for _, o := range now {
			vals = append(vals, o.Value)
		}
		t.Fatalf("%d current readings (%v), want only the latest — the history has forked", len(now), vals)
	}
	if now[0].ID != third.ID {
		t.Errorf("the current reading is %s, want the last correction %s", now[0].ID, third.ID)
	}
}
