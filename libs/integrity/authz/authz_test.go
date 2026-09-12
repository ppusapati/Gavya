package authz

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Every procedure the platform serves names a permission, and every permission
// named here belongs to a procedure that exists.
//
// Both directions matter and they fail differently. A route with no entry is
// refused at runtime by Decide — safe, but it fails when a co-operative tries to
// record its morning collection rather than when somebody adds the route. An
// entry for a route that no longer exists is the opposite: harmless in
// operation, and it quietly rots until nobody can tell which of these lines are
// load-bearing.
//
// The route list is scraped from the services rather than written here, for the
// same reason the gateway's compose check scrapes the gateway's own config: a
// list maintained by hand is one that goes stale in exactly the way the check
// exists to prevent.
//
// RUN WITH -count=1. This reads Go files in other modules and the test cache does
// not track them, so a cached pass is a check that did not run.
func TestEveryProcedureNamesAPermissionAndEveryPermissionNamesAProcedure(t *testing.T) {
	registered := scrapeProcedures(t)
	if len(registered) < 200 {
		t.Fatalf("found only %d procedures across the services; the pattern this test "+
			"scrapes with has probably stopped matching, and a check that finds "+
			"nothing passes", len(registered))
	}

	table := Table()

	var missing []string
	for _, p := range registered {
		if _, ok := table[p]; !ok {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("%d procedures have no permission declared:\n  %s\n"+
			"Decide refuses these, so they are closed rather than open — but they "+
			"are closed to everybody including the people whose job they are.",
			len(missing), strings.Join(missing, "\n  "))
	}

	known := make(map[string]bool, len(registered))
	for _, p := range registered {
		known[p] = true
	}
	var stale []string
	for p := range table {
		if !known[p] {
			stale = append(stale, p)
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf("%d permissions are declared for procedures that do not exist:\n  %s",
			len(stale), strings.Join(stale, "\n  "))
	}

	t.Logf("%d procedures, %d with a declared permission", len(registered), len(table))
}

// Nothing in the table names a permission no role can hold.
//
// A procedure requiring a permission that appears in no role is unreachable by
// every person and every service — a route nobody can call, which looks from the
// outside exactly like a route that is broken.
func TestEveryPermissionAProcedureNeedsIsHeldBySomeRole(t *testing.T) {
	held := Set{}
	for _, r := range Roles() {
		for p := range r.Permissions {
			held[p] = struct{}{}
		}
	}

	orphan := map[Permission][]string{}
	for procedure, needs := range Table() {
		if needs == Public {
			continue
		}
		if !held.Has(needs) {
			orphan[needs] = append(orphan[needs], procedure)
		}
	}
	for needs, procedures := range orphan {
		sort.Strings(procedures)
		t.Errorf("%s is required by %d procedures and held by no role, so none of "+
			"them can be called by anybody:\n  %s",
			needs, len(procedures), strings.Join(procedures, "\n  "))
	}
}

// And no role holds a permission nothing requires.
//
// That is the other half of the same honesty. A role carrying settlement.approve
// when no procedure asks for it reads as a control — somebody looking at the
// role list would believe approval is restricted — and controls nothing.
func TestNoRoleHoldsAPermissionNoProcedureRequires(t *testing.T) {
	required := Set{}
	for _, needs := range Table() {
		required[needs] = struct{}{}
	}

	for name, role := range Roles() {
		var idle []string
		for p := range role.Permissions {
			if !required.Has(p) {
				idle = append(idle, string(p))
			}
		}
		if len(idle) > 0 {
			sort.Strings(idle)
			t.Errorf("role %q holds %s, which no procedure requires\n"+
				"A permission nothing asks for reads as a control and is not one.",
				name, strings.Join(idle, ", "))
		}
	}
}

// A procedure nobody declared is refused, not allowed.
//
// This is the property the whole package rests on: adding a route without
// deciding who may call it must break that route, not open it. The test above
// stops it reaching a release; this one decides what happens if it ever does.
func TestAnUndeclaredProcedureIsRefused(t *testing.T) {
	everything := Set{}
	for _, p := range Table() {
		everything[p] = struct{}{}
	}

	err := Decide("some.v1.Service/AddedYesterday", everything)
	if err == nil {
		t.Fatal("a procedure with no declared permission was allowed, and the caller " +
			"holding every permission in the platform is exactly who would not notice")
	}
	var unknown *ErrUnknownProcedure
	if !errors.As(err, &unknown) {
		t.Errorf("refused with %v, want an ErrUnknownProcedure — the difference "+
			"matters: this is a fault in the platform, not in the request", err)
	}
}

// The refusal names what was needed.
//
// An authorisation failure that says only "denied" turns into a support ticket.
// One that names the permission turns into a role change.
func TestARefusalNamesThePermissionItWanted(t *testing.T) {
	err := Decide("settlement.v1.SettlementService/ApproveCycle", NewSet(SettlementRead))
	if err == nil {
		t.Fatal("a caller holding only settlement.read approved a payment cycle")
	}
	var denied *ErrNoPermission
	if !errors.As(err, &denied) {
		t.Fatalf("refused with %T, want ErrNoPermission", err)
	}
	if denied.Needs != SettlementApprove {
		t.Errorf("the refusal names %q, want %q", denied.Needs, SettlementApprove)
	}
	if !strings.Contains(err.Error(), "settlement.approve") {
		t.Errorf("the message is %q and does not name the permission", err.Error())
	}
}

// Public procedures are callable holding nothing.
//
// Sign-in cannot require a permission, because permissions arrive with a
// session and a session is what sign-in produces.
func TestSignInNeedsNoPermission(t *testing.T) {
	for _, p := range []string{
		"gavya.identity.v1.IdentityService/SignIn",
		"gavya.identity.v1.IdentityService/SignInService",
		"/gavya.identity.v1.IdentityService/SignIn",
	} {
		if err := Decide(p, Set{}); err != nil {
			t.Errorf("%s was refused to a caller holding nothing: %v", p, err)
		}
	}
}

// The separations the roles exist for.
//
// Each of these is a control somebody would state in a sentence, and each is
// checked as one. Written out rather than derived from the role table, so a role
// edited to be more permissive fails here rather than silently redefining what
// the control was.
func TestTheSeparationsTheRolesExistFor(t *testing.T) {
	roles := Roles()
	for _, c := range []struct {
		role, procedure, why string
		allowed              bool
	}{
		{"collector", "milk.v1.MilkService/RecordMilk", "a collector records milk", true},
		{"collector", "procurement.v1.ProcurementService/CorrectCollection",
			"correcting a collection re-prices it; the person who recorded a figure " +
				"must not be the one who amends it", false},
		{"collector", "settlement.v1.SettlementService/ApproveCycle",
			"a collector approving payments is the whole reason this package exists", false},
		{"collector", "procurement.v1.ProcurementService/DeclareRateCard",
			"a collector must not set the price their own milk is bought at", false},

		{"supervisor", "procurement.v1.ProcurementService/CorrectCollection",
			"correcting a collection is what a supervisor is for", true},
		{"supervisor", "settlement.v1.SettlementService/ApproveCycle",
			"a supervisor decides disputes, not payments", false},

		{"accountant", "settlement.v1.SettlementService/ApproveCycle",
			"approving the cycle is the accountant's", true},
		{"accountant", "milk.v1.MilkService/RecordMilk",
			"the figure and the payment for it are two people's work", false},

		{"auditor", "settlement.v1.SettlementService/GetCycle", "an auditor reads", true},
		{"auditor", "settlement.v1.SettlementService/ApproveCycle", "and changes nothing", false},
		{"auditor", "milk.v1.MilkService/RecordMilk", "and records nothing", false},
		{"auditor", "cattle.v1.CattleService/DeleteCattle", "and deletes nothing", false},

		{"service", "milk.v1.MilkService/RecordMilk", "a device records collections", true},
		{"service", "settlement.v1.SettlementService/ApproveCycle",
			"an approval nobody can be asked about is not an approval", false},

		{"admin", "settlement.v1.SettlementService/ApproveCycle",
			"the tenant's administrator can do anything within it", true},
	} {
		role, ok := roles[c.role]
		if !ok {
			t.Fatalf("no role named %q", c.role)
		}
		err := Decide(c.procedure, role.Permissions)
		if c.allowed && err != nil {
			t.Errorf("%s cannot call %s, and %s: %v", c.role, c.procedure, c.why, err)
		}
		if !c.allowed && err == nil {
			t.Errorf("%s can call %s, and %s", c.role, c.procedure, c.why)
		}
	}
}

// The auditor reads everything and writes nothing.
//
// Checked against the whole table rather than a sample, because "read-only" is
// the one claim in the role list somebody will rely on without re-reading it.
func TestTheAuditorCanCallEveryReadAndNoWrite(t *testing.T) {
	auditor := Roles()["auditor"]
	reads, writes := 0, 0
	for procedure, needs := range Table() {
		if needs == Public {
			continue
		}
		isRead := strings.HasSuffix(string(needs), ".read")
		err := Decide(procedure, auditor.Permissions)
		switch {
		case isRead && err != nil:
			// Not every read is the auditor's: tenant administration is its own
			// domain. Counted rather than demanded.
			t.Logf("auditor cannot read %s (%s)", procedure, needs)
		case isRead:
			reads++
		case err == nil:
			writes++
			t.Errorf("the auditor can call %s, which needs %s — the role is "+
				"described as reading everything and changing nothing",
				procedure, needs)
		}
	}
	if reads == 0 {
		t.Fatal("the auditor can read nothing at all, so this check proved nothing")
	}
	t.Logf("auditor: %d reads allowed, %d writes allowed", reads, writes)
}

// A set survives the round trip through a header.
func TestASetSurvivesBeingWrittenToAHeaderAndRead(t *testing.T) {
	want := Roles()["accountant"].Permissions
	got := ParseSet(want.String())
	if len(got) != len(want) {
		t.Fatalf("wrote %d permissions and read back %d", len(want), len(got))
	}
	for p := range want {
		if !got.Has(p) {
			t.Errorf("%s did not survive the round trip", p)
		}
	}
	// Sorted, so the same set always renders identically.
	if want.String() != ParseSet(want.String()).String() {
		t.Error("a set does not render the same way twice")
	}
}

// scrapeProcedures reads every route every service registers.
func scrapeProcedures(t *testing.T) []string {
	t.Helper()
	root := repoRoot(t)
	dirs, err := filepath.Glob(filepath.Join(root, "services", "*-service"))
	if err != nil {
		t.Fatal(err)
	}

	nameRe := regexp.MustCompile(`const ServiceName = "([^"]+)"`)
	routeRe := regexp.MustCompile(`route\("([A-Za-z]+)"`)

	var out []string
	for _, dir := range dirs {
		path := filepath.Join(dir, "internal", "handler", "connect_handlers.go")
		src, err := os.ReadFile(path)
		if err != nil {
			continue // a service shaped differently, or none
		}
		name := nameRe.FindSubmatch(src)
		if name == nil {
			continue
		}
		for _, m := range routeRe.FindAllSubmatch(src, -1) {
			out = append(out, string(name[1])+"/"+string(m[1]))
		}
	}
	sort.Strings(out)
	return out
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// .../libs/integrity/authz -> repository root
	return filepath.Dir(filepath.Dir(filepath.Dir(wd)))
}
