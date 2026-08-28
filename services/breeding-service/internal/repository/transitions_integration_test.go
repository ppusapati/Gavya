package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/services/breeding-service/internal/domain"
)

// testIDs supplies the identifiers audit entries carry. The chain does not care
// what the id is, only that it is unique.
type testIDs struct{}

func (testIDs) New() string { return newTestID("AU") }

// A breeding record and the state it moves an animal into are one fact. That
// they commit together is a property of the transaction, and only a real
// database can show it.
//
//	TEST_DATABASE_DSN="postgres://user@host:port/%s?sslmode=disable" go test ./...

const testDatabase = "breeding_repo_test"

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	tpl := os.Getenv("TEST_DATABASE_DSN")
	if tpl == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}
	ctx := context.Background()

	admin, err := pgxpool.New(ctx, fmt.Sprintf(tpl, "postgres"))
	if err != nil {
		t.Fatalf("connect to the maintenance database: %v", err)
	}
	defer admin.Close()

	var exists bool
	if err := admin.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname=$1)`, testDatabase).Scan(&exists); err != nil {
		t.Fatalf("look for %s: %v", testDatabase, err)
	}
	if !exists {
		if _, err := admin.Exec(ctx, `CREATE DATABASE "`+testDatabase+`"`); err != nil {
			t.Fatalf("create %s: %v", testDatabase, err)
		}
	}

	pool, err := pgxpool.New(ctx, fmt.Sprintf(tpl, testDatabase))
	if err != nil {
		t.Fatalf("connect to %s: %v", testDatabase, err)
	}
	t.Cleanup(pool.Close)

	schema, err := os.ReadFile(filepath.Join("..", "db", "schema.sql"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	if _, err := pool.Exec(ctx, string(schema)); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	return pool
}

var idSeq atomic.Int64

func newTestID(prefix string) string {
	body := fmt.Sprintf("%s%d%d", prefix, time.Now().UnixNano(), idSeq.Add(1))
	if len(body) > 26 {
		return body[:26]
	}
	return body + "0000000000000000000000000"[:26-len(body)]
}

func ptr(s string) *string { return &s }

type herd struct {
	repo   Repository
	tenant string
}

func newHerd(t *testing.T) *herd {
	t.Helper()
	return &herd{repo: New(testPool(t), testIDs{}), tenant: newTestID("tnt")}
}

func (h *herd) cycle(t *testing.T) *domain.BreedingCycle {
	t.Helper()
	actor := newTestID("usr")
	c, err := h.repo.CreateBreedingCycle(context.Background(), &domain.BreedingCycle{
		ID:        newTestID("cyc"),
		TenantID:  h.tenant,
		CattleID:  newTestID("cow"),
		HeatDate:  time.Now(),
		Status:    domain.CycleOpen,
		CreatedBy: actor,
		UpdatedBy: actor,
	})
	if err != nil {
		t.Fatalf("CreateBreedingCycle: %v", err)
	}
	return c
}

func (h *herd) inseminate(t *testing.T, c *domain.BreedingCycle) (*domain.Insemination, *domain.BreedingCycle, error) {
	t.Helper()
	actor := newTestID("usr")
	return h.repo.RecordInseminationInCycle(context.Background(), &domain.Insemination{
		ID:            newTestID("ins"),
		TenantID:      h.tenant,
		CycleID:       c.ID,
		CattleID:      c.CattleID,
		BullID:        ptr(newTestID("bul")),
		SemenBatchID:  ptr(newTestID("btc")),
		InseminatedAt: time.Now(),
		Method:        "AI",
		CreatedBy:     actor,
		UpdatedBy:     actor,
	})
}

func (h *herd) confirm(t *testing.T, ins *domain.Insemination) (*domain.Pregnancy, *domain.BreedingCycle, error) {
	t.Helper()
	actor := newTestID("usr")
	return h.repo.ConfirmPregnancyForCycle(context.Background(), &domain.Pregnancy{
		ID:                  newTestID("prg"),
		TenantID:            h.tenant,
		CattleID:            ins.CattleID,
		InseminationID:      ins.ID,
		ConfirmedAt:         time.Now(),
		ExpectedCalvingDate: time.Now().AddDate(0, 9, 10),
		Status:              domain.PregnancyActive,
		CreatedBy:           actor,
		UpdatedBy:           actor,
	})
}

func (h *herd) calve(t *testing.T, p *domain.Pregnancy) (*domain.CalvingRecord, *domain.Pregnancy, error) {
	t.Helper()
	actor := newTestID("usr")
	return h.repo.RecordCalvingAndClosePregnancy(context.Background(), &domain.CalvingRecord{
		ID:          newTestID("clv"),
		TenantID:    h.tenant,
		PregnancyID: p.ID,
		CattleID:    p.CattleID,
		CalfID:      ptr(newTestID("clf")),
		CalvingDate: time.Now(),
		CalfGender:  domain.CalfFemale,
		CalfWeight:  32.5,
		Status:      domain.CalvingNormal,
		CreatedBy:   actor,
		UpdatedBy:   actor,
	})
}

// The whole sequence, which is the thing a herd manager actually does.
func TestACycleAdvancesThroughItsRecords(t *testing.T) {
	h := newHerd(t)
	cycle := h.cycle(t)

	ins, afterIns, err := h.inseminate(t, cycle)
	if err != nil {
		t.Fatalf("insemination: %v", err)
	}
	if afterIns.Status != domain.CycleInseminated {
		t.Errorf("cycle = %s after insemination, want inseminated", afterIns.Status)
	}

	preg, afterPreg, err := h.confirm(t, ins)
	if err != nil {
		t.Fatalf("pregnancy: %v", err)
	}
	if afterPreg.Status != domain.CyclePregnant {
		t.Errorf("cycle = %s after confirmation, want pregnant", afterPreg.Status)
	}

	_, afterCalving, err := h.calve(t, preg)
	if err != nil {
		t.Fatalf("calving: %v", err)
	}
	if afterCalving.Status != domain.PregnancyDelivered {
		t.Errorf("pregnancy = %s after calving, want delivered", afterCalving.Status)
	}
}

// The defect: the cycle update was logged and discarded, so an insemination
// could stand against a cycle still reading open.
func TestAnInseminationNeverStandsAgainstAnUnadvancedCycle(t *testing.T) {
	h := newHerd(t)
	cycle := h.cycle(t)

	if _, _, err := h.inseminate(t, cycle); err != nil {
		t.Fatal(err)
	}

	after, err := h.repo.GetBreedingCycle(context.Background(), cycle.ID, h.tenant)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status == domain.CycleOpen {
		t.Error("the cycle still reads open, so the animal invites a second service")
	}
}

// An animal already carrying must not be bred again through the same cycle.
func TestAPregnantCycleRefusesAnotherInsemination(t *testing.T) {
	h := newHerd(t)
	cycle := h.cycle(t)
	ins, _, err := h.inseminate(t, cycle)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.confirm(t, ins); err != nil {
		t.Fatal(err)
	}

	if _, _, err := h.inseminate(t, cycle); !errors.Is(err, ErrCycleNotOpen) {
		t.Fatalf("err = %v, want ErrCycleNotOpen", err)
	}
}

// The old path skipped the cycle update silently when the insemination lookup
// failed, because the result was discarded with `if err == nil`.
func TestAPregnancyAgainstAnUnknownInseminationIsRefused(t *testing.T) {
	h := newHerd(t)
	cycle := h.cycle(t)

	_, _, err := h.confirm(t, &domain.Insemination{ID: newTestID("gone"), CattleID: cycle.CattleID})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// More calves than animals that bore them is not a herd.
func TestAPregnancyCannotDeliverTwice(t *testing.T) {
	h := newHerd(t)
	cycle := h.cycle(t)
	ins, _, err := h.inseminate(t, cycle)
	if err != nil {
		t.Fatal(err)
	}
	preg, _, err := h.confirm(t, ins)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.calve(t, preg); err != nil {
		t.Fatal(err)
	}

	if _, _, err := h.calve(t, preg); !errors.Is(err, ErrPregnancyClosed) {
		t.Fatalf("err = %v, want ErrPregnancyClosed", err)
	}
}

// A record that cannot be written must take the state change with it, or the
// animal's history and its state disagree.
func TestAnUnwritableCalvingLeavesThePregnancyOpen(t *testing.T) {
	h := newHerd(t)
	cycle := h.cycle(t)
	ins, _, err := h.inseminate(t, cycle)
	if err != nil {
		t.Fatal(err)
	}
	preg, _, err := h.confirm(t, ins)
	if err != nil {
		t.Fatal(err)
	}

	actor := newTestID("usr")
	_, _, err = h.repo.RecordCalvingAndClosePregnancy(context.Background(), &domain.CalvingRecord{
		ID:          "this-identifier-is-far-too-long-for-the-column-it-goes-in",
		TenantID:    h.tenant,
		PregnancyID: preg.ID,
		CattleID:    preg.CattleID,
		CalvingDate: time.Now(),
		CalfGender:  domain.CalfFemale,
		Status:      domain.CalvingNormal,
		CreatedBy:   actor,
		UpdatedBy:   actor,
	})
	if err == nil {
		t.Fatal("an unwritable calving record was accepted")
	}

	after, err := h.repo.GetPregnancy(context.Background(), preg.ID, h.tenant)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != domain.PregnancyActive {
		t.Errorf("pregnancy = %s, want it left active by a failed calving", after.Status)
	}
}

// Another tenant's cycle is not reachable by guessing an identifier.
func TestOneTenantCannotInseminateAnothersCycle(t *testing.T) {
	owner := newHerd(t)
	cycle := owner.cycle(t)

	intruder := &herd{repo: owner.repo, tenant: newTestID("tnt")}
	if _, _, err := intruder.inseminate(t, cycle); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
