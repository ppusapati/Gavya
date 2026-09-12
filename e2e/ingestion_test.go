//go:build e2e

// The ingestion spine, through the real binary.
//
// "Replayable idempotent ingestion — redelivering a record any number of times
// admits it exactly once" is one of the six properties this platform claims. Of
// the six it was the only one with nothing behind it end to end: the harness
// built ingestion-service, waited for it to report ready, and never called it.
// Every one of its thirteen routes was untested through HTTP.
//
// The admission rules themselves have thorough unit tests, and those are the
// right place for the rule table — Admit is a pure function over a loaded
// context, and exercising it needs no database. What unit tests cannot show is
// that the loading is right: that the repository finds the record already
// occupying a slot, that the session's high water mark is the one the rules
// read, that a generation roll is visible to the next delivery. Those are
// properties of the wiring, and the wiring is what these tests are for.
//
// The device story throughout is one physical unit: a milk analyser in a village
// collection centre that gets reflashed halfway through the fortnight.
package e2e

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

const ingestionSvc = "ingestion.v1.IngestionService"

type registerDeviceReq struct {
	TenantID string `json:"tenant_id"`
	Serial   string `json:"serial"`
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	Actor    string `json:"actor"`
}

type deviceResp struct {
	Device *struct {
		ID                string `json:"id"`
		Serial            string `json:"serial"`
		CurrentGeneration int64  `json:"current_generation"`
	} `json:"device"`
}

type openSessionReq struct {
	TenantID          string `json:"tenant_id"`
	DeviceID          string `json:"device_id"`
	ExternalSessionID string `json:"external_session_id"`
	OperatorRef       string `json:"operator_ref"`
	Actor             string `json:"actor"`
}

type closeSessionReq struct {
	TenantID  string `json:"tenant_id"`
	SessionID string `json:"session_id"`
	Actor     string `json:"actor"`
}

type sessionResp struct {
	Session *struct {
		ID           string `json:"id"`
		Generation   int64  `json:"generation"`
		Status       string `json:"status"`
		LastSequence int64  `json:"last_sequence"`
		RecordCount  int64  `json:"record_count"`
	} `json:"session"`
}

type deliverReq struct {
	TenantID          string          `json:"tenant_id"`
	DeviceID          string          `json:"device_id"`
	Generation        int64           `json:"generation"`
	ExternalSessionID string          `json:"external_session_id"`
	Sequence          int64           `json:"sequence"`
	Payload           json.RawMessage `json:"payload"`
	CapturedAt        string          `json:"captured_at"`
	Actor             string          `json:"actor"`
}

type deliverResp struct {
	Outcome             string `json:"outcome"`
	RecordID            string `json:"record_id"`
	QuarantineID        string `json:"quarantine_id"`
	Reason              string `json:"reason"`
	Detail              string `json:"detail"`
	ConflictingRecordID string `json:"conflicting_record_id"`
}

type deliverBatchReq struct {
	Records []deliverReq `json:"records"`
}

type deliverBatchResp struct {
	Results     []deliverResp `json:"results"`
	Accepted    int           `json:"accepted"`
	Replayed    int           `json:"replayed"`
	Quarantined int           `json:"quarantined"`
}

type rollGenerationReq struct {
	TenantID string `json:"tenant_id"`
	DeviceID string `json:"device_id"`
	Reason   string `json:"reason"`
	Actor    string `json:"actor"`
}

type generationResp struct {
	Generation *struct {
		Generation int64  `json:"generation"`
		Reason     string `json:"reason"`
	} `json:"generation"`
}

type listQuarantinedReq struct {
	TenantID string `json:"tenant_id"`
	Reason   string `json:"reason"`
	Limit    int32  `json:"limit"`
}

type listQuarantinedResp struct {
	Records []*struct {
		ID                  string `json:"id"`
		Reason              string `json:"reason"`
		Sequence            int64  `json:"sequence"`
		ConflictingRecordID string `json:"conflicting_record_id"`
		Resolved            bool   `json:"resolved"`
	} `json:"records"`
}

type resolveQuarantineReq struct {
	ID         string `json:"id"`
	TenantID   string `json:"tenant_id"`
	Resolution string `json:"resolution"`
	Actor      string `json:"actor"`
}

// collector is one registered device with one open session, which is the state
// every delivery below starts from.
type collector struct {
	p       *platform
	device  string
	session string // the external id the device uses
	gen     int64
}

func aCollector(t *testing.T, p *platform) *collector {
	t.Helper()
	dev, err := svcclient.Call[registerDeviceReq, deviceResp](
		context.Background(), p.ingestion(), ingestionSvc+"/RegisterDevice",
		registerDeviceReq{TenantID: p.tenant, Serial: newID("ser"),
			Kind: "MILK_ANALYSER", Label: "Kothapalli centre", Actor: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("register device: %v", err)
	}
	if dev.Device.CurrentGeneration != 1 {
		t.Fatalf("a newly registered device is on generation %d, want 1 — a device's "+
			"first epoch is provisioning, and records arriving under generation 1 have "+
			"nowhere to go if it does not exist", dev.Device.CurrentGeneration)
	}

	c := &collector{p: p, device: dev.Device.ID, session: newID("cap"), gen: 1}
	c.open(t)
	return c
}

func (c *collector) open(t *testing.T) *sessionResp {
	t.Helper()
	s, err := svcclient.Call[openSessionReq, sessionResp](
		context.Background(), c.p.ingestion(), ingestionSvc+"/OpenSession",
		openSessionReq{TenantID: c.p.tenant, DeviceID: c.device,
			ExternalSessionID: c.session, OperatorRef: "operator", Actor: "e2e"}, c.p.opts())
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	return s
}

// deliver sends one record. The payload is what decides replay from conflict, so
// it is the parameter that matters most here.
func (c *collector) deliver(t *testing.T, seq int64, payload string) *deliverResp {
	t.Helper()
	out, err := svcclient.Call[deliverReq, deliverResp](
		context.Background(), c.p.ingestion(), ingestionSvc+"/DeliverRecord",
		c.record(seq, payload), c.p.opts())
	if err != nil {
		t.Fatalf("deliver sequence %d: %v", seq, err)
	}
	return out
}

func (c *collector) record(seq int64, payload string) deliverReq {
	return deliverReq{
		TenantID: c.p.tenant, DeviceID: c.device, Generation: c.gen,
		ExternalSessionID: c.session, Sequence: seq,
		Payload:    json.RawMessage(payload),
		CapturedAt: time.Now().UTC().Format(time.RFC3339),
		Actor:      "e2e",
	}
}

// Redelivering a record admits it exactly once.
//
// This is the property, stated as the platform states it. A collection device in
// a village runs on a phone with no signal for most of the day; its outbox
// retries whenever it finds a bar. The record it retries has to land once, and
// the device has to be told clearly enough that it can stop.
func TestRedeliveringARecordAdmitsItExactlyOnce(t *testing.T) {
	p := startPlatform(t)
	c := aCollector(t, p)

	const payload = `{"producer":"P-118","litres":"12.400","fat":"4.1"}`
	first := c.deliver(t, 1, payload)
	if first.Outcome != "ACCEPTED" {
		t.Fatalf("the first delivery was %s (%s: %s), want ACCEPTED",
			first.Outcome, first.Reason, first.Detail)
	}
	if first.RecordID == "" {
		t.Fatal("an accepted record came back with no record id, so the device has " +
			"nothing to reconcile its outbox against")
	}

	// Redelivered nine more times, as an outbox would.
	for i := 0; i < 9; i++ {
		again := c.deliver(t, 1, payload)
		if again.Outcome != "DUPLICATE_REPLAY" {
			t.Fatalf("redelivery %d was %s (%s), want DUPLICATE_REPLAY", i+1, again.Outcome, again.Detail)
		}
		if again.RecordID != first.RecordID {
			t.Fatalf("redelivery %d was told record %s, but the original is %s — a device "+
				"handed a different id each time cannot tell a replay from a second admission",
				i+1, again.RecordID, first.RecordID)
		}
	}

	// And the session counted one record, not ten. The outcome strings could all
	// be right while the count drifted, and the count is what a supervisor
	// reconciles a day's collection against.
	s := c.open(t)
	if s.Session.RecordCount != 1 {
		t.Errorf("after one record and nine redeliveries the session holds %d records, want 1",
			s.Session.RecordCount)
	}
	if s.Session.LastSequence != 1 {
		t.Errorf("session high water mark is %d, want 1", s.Session.LastSequence)
	}
}

// Ten devices retrying the same record at once still admit it once.
//
// The sequential case above can pass on a service that checks for an existing
// record and then inserts, which is two statements and a window between them. A
// phone that comes back into signal and fires its whole outbox at once is
// exactly what walks into that window.
func TestConcurrentRedeliveryAdmitsOneRecord(t *testing.T) {
	p := startPlatform(t)
	c := aCollector(t, p)

	const payload = `{"producer":"P-204","litres":"9.750"}`
	const attempts = 10

	var wg sync.WaitGroup
	outcomes := make([]*deliverResp, attempts)
	errs := make([]error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out, err := svcclient.Call[deliverReq, deliverResp](
				context.Background(), p.ingestion(), ingestionSvc+"/DeliverRecord",
				c.record(1, payload), p.opts())
			outcomes[i], errs[i] = out, err
		}(i)
	}
	wg.Wait()

	accepted, replayed := 0, 0
	ids := map[string]bool{}
	for i, out := range outcomes {
		if errs[i] != nil {
			t.Fatalf("attempt %d: %v", i, errs[i])
		}
		switch out.Outcome {
		case "ACCEPTED":
			accepted++
			ids[out.RecordID] = true
		case "DUPLICATE_REPLAY":
			replayed++
			ids[out.RecordID] = true
		default:
			t.Fatalf("attempt %d was %s (%s: %s); the same record delivered twice is a "+
				"replay, not a conflict", i, out.Outcome, out.Reason, out.Detail)
		}
	}
	if accepted != 1 {
		t.Errorf("%d of %d concurrent deliveries were ACCEPTED, want exactly 1 — the "+
			"others are the same record and the producer would be paid twice for it",
			accepted, attempts)
	}
	if len(ids) != 1 {
		t.Errorf("the %d deliveries name %d different records, want 1", attempts, len(ids))
	}

	s := c.open(t)
	if s.Session.RecordCount != 1 {
		t.Errorf("the session holds %d records after %d deliveries of one, want 1",
			s.Session.RecordCount, attempts)
	}
}

// Two payloads under one sequence number are held, not chosen between.
//
// A replay is decided by the payload hash. When the hash differs, the device has
// issued one identity for two readings, and there is no safe way to pick: taking
// the first discards a real collection, taking the second overwrites one. Both
// are held and the session's identity is impugned, because a sequence space that
// has done this once cannot be trusted for the rest of the run.
func TestTwoPayloadsUnderOneSequenceAreHeldForAPerson(t *testing.T) {
	p := startPlatform(t)
	c := aCollector(t, p)

	first := c.deliver(t, 1, `{"producer":"P-118","litres":"12.400"}`)
	if first.Outcome != "ACCEPTED" {
		t.Fatalf("first delivery: %s (%s)", first.Outcome, first.Detail)
	}

	clash := c.deliver(t, 1, `{"producer":"P-118","litres":"21.400"}`)
	if clash.Outcome != "QUARANTINED" {
		t.Fatalf("a second payload under sequence 1 was %s, want QUARANTINED — one of "+
			"12.400 and 21.400 litres is a collection nobody has a record of",
			clash.Outcome)
	}
	if clash.Reason != "TRANSPORT_IDENTITY_CONFLICT" {
		t.Errorf("reason = %s, want TRANSPORT_IDENTITY_CONFLICT", clash.Reason)
	}
	if clash.ConflictingRecordID != first.RecordID {
		t.Errorf("the quarantine names %q as what it collided with, want the admitted "+
			"record %q — a reviewer has to be able to read both", clash.ConflictingRecordID, first.RecordID)
	}
	if clash.RecordID != "" {
		t.Errorf("a quarantined record came back with record_id %q; the device would read "+
			"that as stored and drop it from its outbox", clash.RecordID)
	}

	// Held, and findable. A quarantine nobody can list is a record that is lost
	// with extra steps.
	held, err := svcclient.Call[listQuarantinedReq, listQuarantinedResp](
		context.Background(), p.ingestion(), ingestionSvc+"/ListQuarantined",
		listQuarantinedReq{TenantID: p.tenant, Limit: 50}, p.opts())
	if err != nil {
		t.Fatalf("list quarantined: %v", err)
	}
	var found bool
	for _, r := range held.Records {
		if r.ID == clash.QuarantineID {
			found = true
			if r.Resolved {
				t.Error("a quarantine arrived already resolved")
			}
		}
	}
	if !found {
		t.Fatalf("quarantine %s is not in the list of held records", clash.QuarantineID)
	}

	// And the session stops accepting, because its sequence space issued one
	// number for two readings.
	s := c.open(t)
	if s.Session.Status == "OPEN" {
		t.Errorf("the session is still %s after issuing one sequence for two payloads; "+
			"every later record in it inherits the same doubt", s.Session.Status)
	}
}

// A device that is reflashed starts counting again, and both runs survive.
//
// This is what generations are for. The analyser is reflashed mid-fortnight and
// its sequence counter restarts at 1. Without a generation, sequence 1 of the
// new run is indistinguishable from a replay of sequence 1 of the old, and the
// platform either drops a real collection or admits a duplicate.
func TestAReflashedDeviceStartsCountingAgainWithoutLosingTheOldRun(t *testing.T) {
	p := startPlatform(t)
	c := aCollector(t, p)

	before := c.deliver(t, 1, `{"producer":"P-118","litres":"12.400"}`)
	if before.Outcome != "ACCEPTED" {
		t.Fatalf("before the reflash: %s (%s)", before.Outcome, before.Detail)
	}

	rolled, err := svcclient.Call[rollGenerationReq, generationResp](
		context.Background(), p.ingestion(), ingestionSvc+"/RollGeneration",
		rollGenerationReq{TenantID: p.tenant, DeviceID: c.device,
			Reason: "FIRMWARE_REFLASH", Actor: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("roll generation: %v", err)
	}
	if rolled.Generation.Generation != 2 {
		t.Fatalf("after one roll the device is on generation %d, want 2", rolled.Generation.Generation)
	}

	// The reflashed device opens a new session and starts at sequence 1 again.
	// Same number, different epoch, different payload — and it must be admitted.
	c.gen = 2
	c.session = newID("cap")
	c.open(t)
	after := c.deliver(t, 1, `{"producer":"P-204","litres":"9.750"}`)
	if after.Outcome != "ACCEPTED" {
		t.Fatalf("sequence 1 after a reflash was %s (%s: %s), want ACCEPTED — this is the "+
			"case generations exist to make admissible", after.Outcome, after.Reason, after.Detail)
	}
	if after.RecordID == before.RecordID {
		t.Fatal("the two runs' sequence 1 came back as the same record")
	}

	// A late arrival from the old epoch is held rather than dropped. It may be a
	// genuine collection made before the reflash, and only an operator knows
	// whether it was recovered another way.
	late, err := svcclient.Call[deliverReq, deliverResp](
		context.Background(), p.ingestion(), ingestionSvc+"/DeliverRecord",
		deliverReq{TenantID: p.tenant, DeviceID: c.device, Generation: 1,
			ExternalSessionID: newID("cap"), Sequence: 7,
			Payload:    json.RawMessage(`{"producer":"P-311","litres":"6.200"}`),
			CapturedAt: time.Now().UTC().Format(time.RFC3339), Actor: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("late arrival: %v", err)
	}
	if late.Outcome != "QUARANTINED" {
		t.Errorf("a record from the previous generation was %s, want QUARANTINED — it is "+
			"not safe to admit and not ours to discard", late.Outcome)
	}
	if late.QuarantineID == "" {
		t.Error("the late record was refused without being held anywhere")
	}
}

// A closed session takes no more records, and says so.
func TestAClosedSessionRefusesFurtherRecordsWithoutLosingThem(t *testing.T) {
	p := startPlatform(t)
	c := aCollector(t, p)

	opened := c.open(t)
	if out := c.deliver(t, 1, `{"producer":"P-118","litres":"12.400"}`); out.Outcome != "ACCEPTED" {
		t.Fatalf("before closing: %s", out.Outcome)
	}
	if _, err := svcclient.Call[closeSessionReq, sessionResp](
		context.Background(), p.ingestion(), ingestionSvc+"/CloseSession",
		closeSessionReq{TenantID: p.tenant, SessionID: opened.Session.ID, Actor: "e2e"},
		p.opts()); err != nil {
		t.Fatalf("close session: %v", err)
	}

	after := c.deliver(t, 2, `{"producer":"P-204","litres":"9.750"}`)
	if after.Outcome != "QUARANTINED" {
		t.Fatalf("a record delivered after the session closed was %s, want QUARANTINED", after.Outcome)
	}
	if after.Reason != "SESSION_NOT_ACCEPTING" {
		t.Errorf("reason = %s, want SESSION_NOT_ACCEPTING", after.Reason)
	}
	if after.QuarantineID == "" {
		t.Error("the record was refused and not held; a collection made just before a " +
			"supervisor closed the session is exactly the one worth keeping")
	}

	// A record already admitted stays admitted after the close. A device
	// retrying its outbox must not be told its accepted records have gone.
	replay := c.deliver(t, 1, `{"producer":"P-118","litres":"12.400"}`)
	if replay.Outcome != "DUPLICATE_REPLAY" {
		t.Errorf("replaying an admitted record into a closed session was %s, want "+
			"DUPLICATE_REPLAY — the record was admitted before the close and is still there",
			replay.Outcome)
	}
}

// A batch reports what happened to each record and what happened overall.
//
// A device uploads a fortnight in one call. The counts are what its operator
// reads; the per-record results are what its outbox acts on. Both have to be
// right, and they have to agree with each other.
func TestABatchReportsEachRecordAndTheWholeUpload(t *testing.T) {
	p := startPlatform(t)
	c := aCollector(t, p)

	const dup = `{"producer":"P-118","litres":"12.400"}`
	if out := c.deliver(t, 1, dup); out.Outcome != "ACCEPTED" {
		t.Fatalf("seeding: %s", out.Outcome)
	}

	batch, err := svcclient.Call[deliverBatchReq, deliverBatchResp](
		context.Background(), p.ingestion(), ingestionSvc+"/DeliverBatch",
		deliverBatchReq{Records: []deliverReq{
			c.record(1, dup), // already admitted
			c.record(2, `{"producer":"P-204","litres":"9.750"}`),  // new
			c.record(3, `{"producer":"P-311","litres":"6.200"}`),  // new
			c.record(1, `{"producer":"P-118","litres":"99.900"}`), // conflicts with the seed
		}}, p.opts())
	if err != nil {
		t.Fatalf("deliver batch: %v", err)
	}

	if len(batch.Results) != 4 {
		t.Fatalf("a batch of 4 came back with %d results; a device cannot tell which of "+
			"its records to drop", len(batch.Results))
	}
	want := []string{"DUPLICATE_REPLAY", "ACCEPTED", "ACCEPTED", "QUARANTINED"}
	for i, w := range want {
		if batch.Results[i].Outcome != w {
			t.Errorf("record %d was %s, want %s (%s)", i, batch.Results[i].Outcome, w,
				batch.Results[i].Detail)
		}
	}
	if batch.Accepted != 2 || batch.Replayed != 1 || batch.Quarantined != 1 {
		t.Errorf("counts are accepted=%d replayed=%d quarantined=%d, want 2/1/1 — the "+
			"summary and the per-record results have to be the same story",
			batch.Accepted, batch.Replayed, batch.Quarantined)
	}
}

// A held record can be resolved, and stops being outstanding.
func TestAHeldRecordCanBeResolved(t *testing.T) {
	p := startPlatform(t)
	c := aCollector(t, p)

	if out := c.deliver(t, 1, `{"producer":"P-118","litres":"12.400"}`); out.Outcome != "ACCEPTED" {
		t.Fatalf("seeding: %s", out.Outcome)
	}
	clash := c.deliver(t, 1, `{"producer":"P-118","litres":"21.400"}`)
	if clash.Outcome != "QUARANTINED" {
		t.Fatalf("expected a conflict, got %s", clash.Outcome)
	}

	if _, err := svcclient.Call[resolveQuarantineReq, listQuarantinedResp](
		context.Background(), p.ingestion(), ingestionSvc+"/ResolveQuarantine",
		resolveQuarantineReq{ID: clash.QuarantineID, TenantID: p.tenant,
			Resolution: "the analyser was double-keyed; 12.400 is the weighbridge figure",
			Actor:      "supervisor"}, p.opts()); err != nil {
		t.Fatalf("resolve quarantine: %v", err)
	}

	held, err := svcclient.Call[listQuarantinedReq, listQuarantinedResp](
		context.Background(), p.ingestion(), ingestionSvc+"/ListQuarantined",
		listQuarantinedReq{TenantID: p.tenant, Limit: 50}, p.opts())
	if err != nil {
		t.Fatalf("list quarantined: %v", err)
	}
	for _, r := range held.Records {
		if r.ID == clash.QuarantineID && !r.Resolved {
			t.Errorf("quarantine %s is still outstanding after being resolved; a queue "+
				"that never empties is one nobody reads", r.ID)
		}
	}
}

// One tenant cannot see another's devices, sessions or held records.
//
// The same check the seven older services get in erp_test.go, for the service
// that holds the rawest data in the platform.
func TestIngestionKeepsTenantsApart(t *testing.T) {
	p := startPlatform(t)
	c := aCollector(t, p)
	if out := c.deliver(t, 1, `{"producer":"P-118","litres":"12.400"}`); out.Outcome != "ACCEPTED" {
		t.Fatalf("seeding: %s", out.Outcome)
	}
	if clash := c.deliver(t, 1, `{"producer":"P-118","litres":"21.400"}`); clash.Outcome != "QUARANTINED" {
		t.Fatalf("seeding a quarantine: %s", clash.Outcome)
	}

	// The owning tenant sees them. Without this the check below is satisfied by
	// a list that returns nothing to anybody, which is not isolation — it is a
	// broken query passing for a policy.
	mine, err := svcclient.Call[listQuarantinedReq, listQuarantinedResp](
		context.Background(), p.ingestion(), ingestionSvc+"/ListQuarantined",
		listQuarantinedReq{TenantID: p.tenant, Limit: 50}, p.opts())
	if err != nil {
		t.Fatalf("list quarantined as the owning tenant: %v", err)
	}
	if len(mine.Records) == 0 {
		t.Fatal("the tenant that produced a conflict sees no held records, so the check " +
			"below would pass against a list that returns nothing to anybody")
	}

	other := newID("tnt")
	held, err := svcclient.Call[listQuarantinedReq, listQuarantinedResp](
		context.Background(), p.ingestion(), ingestionSvc+"/ListQuarantined",
		listQuarantinedReq{TenantID: other, Limit: 50},
		actingAs(other, "e2e"))
	if err == nil && len(held.Records) > 0 {
		t.Errorf("a second tenant sees %d held records through ingestion-service, and it "+
			"wrote none; quarantined payloads are the rawest data this platform holds",
			len(held.Records))
	}

	mineDevices, err := svcclient.Call[listQuarantinedReq, deviceListResp](
		context.Background(), p.ingestion(), ingestionSvc+"/ListDevices",
		listQuarantinedReq{TenantID: p.tenant, Limit: 50}, p.opts())
	if err != nil {
		t.Fatalf("list devices as the owning tenant: %v", err)
	}
	if len(mineDevices.Devices) == 0 {
		t.Fatal("the tenant that registered a device sees none")
	}

	devices, err := svcclient.Call[listQuarantinedReq, deviceListResp](
		context.Background(), p.ingestion(), ingestionSvc+"/ListDevices",
		listQuarantinedReq{TenantID: other, Limit: 50},
		actingAs(other, "e2e"))
	if err == nil && len(devices.Devices) > 0 {
		t.Errorf("a second tenant sees %d devices, and it registered none", len(devices.Devices))
	}
}

type deviceListResp struct {
	Devices []*struct {
		ID string `json:"id"`
	} `json:"devices"`
}

// The reads a person uses when something has gone wrong.
//
// A device's record, the epochs it has been through, and the full payload of a
// record that was held. These are what somebody opens when a collection is
// missing or two of them collide, and they were the three routes this service
// had left uncalled.

type getDeviceReq struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}

type listGenerationsReq struct {
	TenantID string `json:"tenant_id"`
	DeviceID string `json:"device_id"`
}

type listGenerationsResp struct {
	Generations []*struct {
		Generation int64  `json:"generation"`
		Reason     string `json:"reason"`
		DeviceID   string `json:"device_id"`
	} `json:"generations"`
}

type getQuarantinedResp struct {
	Record *struct {
		ID                  string `json:"id"`
		Reason              string `json:"reason"`
		Sequence            int64  `json:"sequence"`
		ConflictingRecordID string `json:"conflicting_record_id"`
	} `json:"record"`
	Payload json.RawMessage `json:"payload"`
}

// A device's epochs are listed in order, and they are that device's.
//
// A reflash opens a new one, and the list is how somebody works out which run a
// missing collection belonged to. One that shows another device's epochs answers
// that question wrongly and plausibly.
func TestADevicesGenerationsAreItsOwn(t *testing.T) {
	p := startPlatform(t)
	mine := aCollector(t, p)
	other := aCollector(t, p)

	for _, reason := range []string{"FIRMWARE_REFLASH", "APP_REINSTALL"} {
		if _, err := svcclient.Call[rollGenerationReq, generationResp](
			context.Background(), p.ingestion(), ingestionSvc+"/RollGeneration",
			rollGenerationReq{TenantID: p.tenant, DeviceID: mine.device,
				Reason: reason, Actor: "e2e"}, p.opts()); err != nil {
			t.Fatalf("roll %s: %v", reason, err)
		}
	}

	list, err := svcclient.Call[listGenerationsReq, listGenerationsResp](
		context.Background(), p.ingestion(), ingestionSvc+"/ListGenerations",
		listGenerationsReq{TenantID: p.tenant, DeviceID: mine.device}, p.opts())
	if err != nil {
		t.Fatalf("list generations: %v", err)
	}
	// Provisioning, then two rolls.
	if len(list.Generations) != 3 {
		t.Fatalf("the device has been through %d epochs, want 3 — provisioning and two "+
			"rolls", len(list.Generations))
	}
	for _, g := range list.Generations {
		if g.DeviceID != mine.device {
			t.Errorf("an epoch of device %s came back in %s's list", g.DeviceID, mine.device)
		}
	}

	// The other device has only been provisioned.
	theirs, err := svcclient.Call[listGenerationsReq, listGenerationsResp](
		context.Background(), p.ingestion(), ingestionSvc+"/ListGenerations",
		listGenerationsReq{TenantID: p.tenant, DeviceID: other.device}, p.opts())
	if err != nil {
		t.Fatalf("list the other device's generations: %v", err)
	}
	if len(theirs.Generations) != 1 {
		t.Errorf("a device that has never been rolled has %d epochs, want 1",
			len(theirs.Generations))
	}

	// And the device itself reads back, with the generation it is now on.
	dev, err := svcclient.Call[getDeviceReq, deviceResp](
		context.Background(), p.ingestion(), ingestionSvc+"/GetDevice",
		getDeviceReq{ID: mine.device, TenantID: p.tenant}, p.opts())
	if err != nil {
		t.Fatalf("get device: %v", err)
	}
	if dev.Device.CurrentGeneration != 3 {
		t.Errorf("after two rolls the device is on generation %d, want 3",
			dev.Device.CurrentGeneration)
	}
}

// A held record comes back with the payload that was refused.
//
// Holding a record and then not being able to show it is the same as dropping
// it: the whole argument for quarantine is that a person can look at both
// readings and say which is the collection.
func TestAHeldRecordComesBackWithThePayloadThatWasRefused(t *testing.T) {
	p := startPlatform(t)
	c := aCollector(t, p)

	const first = `{"producer":"P-118","litres":"12.400"}`
	const second = `{"producer":"P-118","litres":"21.400"}`
	admitted := c.deliver(t, 1, first)
	if admitted.Outcome != "ACCEPTED" {
		t.Fatalf("seeding: %s", admitted.Outcome)
	}
	clash := c.deliver(t, 1, second)
	if clash.Outcome != "QUARANTINED" {
		t.Fatalf("expected a conflict, got %s", clash.Outcome)
	}

	held, err := svcclient.Call[getDeviceReq, getQuarantinedResp](
		context.Background(), p.ingestion(), ingestionSvc+"/GetQuarantined",
		getDeviceReq{ID: clash.QuarantineID, TenantID: p.tenant}, p.opts())
	if err != nil {
		t.Fatalf("get quarantined: %v", err)
	}
	if string(held.Payload) == "" || string(held.Payload) == "null" {
		t.Fatal("the held record came back with no payload; holding a reading nobody " +
			"can look at is the same as dropping it")
	}
	if !strings.Contains(string(held.Payload), "21.400") {
		t.Errorf("the payload is %s, want the reading that was refused", held.Payload)
	}
	if held.Record.ConflictingRecordID != admitted.RecordID {
		t.Errorf("the held record names %q as what it collided with, want %q — a "+
			"reviewer has to be able to read both", held.Record.ConflictingRecordID,
			admitted.RecordID)
	}
	if held.Record.Sequence != 1 {
		t.Errorf("the held record is at sequence %d, want 1", held.Record.Sequence)
	}
}
