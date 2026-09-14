// Package authz decides whether a caller may invoke a procedure.
//
// Until now the platform authenticated and never authorised. A session proved
// which tenant somebody acted for, and every one of the platform's procedures was then
// open to them: a collector at a village booth could approve a settlement cycle,
// rewrite a rate card, or read every producer's payment history. The tenant
// boundary held; inside it there was none.
//
// The shape here is deliberately small, because an authorisation model nobody
// can hold in their head is one that gets bypassed:
//
//   - A Permission is a domain and an action, "settlement.approve". There are
//     tens of these, not hundreds, so a role is readable.
//   - A Role is a named set of permissions. Seven of them, drawn from the jobs
//     that actually exist in a dairy co-operative.
//   - Every procedure names exactly one permission it requires, in routes.go,
//     written out rather than derived. A rule that computes a permission from a
//     method name is a rule that silently mis-files the one procedure whose name
//     does not fit its pattern, and the cost of that is somebody approving
//     payments who should not.
//
// Decide fails closed. A procedure absent from the table is refused, so adding a
// route without a permission breaks it loudly instead of leaving it open — and
// the test in this package refuses the whole build rather than waiting for that.
package authz

import (
	"fmt"
	"sort"
	"strings"
)

// Permission is a domain and an action, joined by a dot.
//
// The domains are the parts of the business somebody can be given a job in, not
// the services the code happens to be split into: pooling, settlement and
// shadow-settlement are one domain here because "may approve what a producer is
// paid" is one job, and it would be a strange role that could do it in two of
// the three.
type Permission string

// The actions. Four, ordered by how much damage they do.
//
// The split that matters is write against approve. Recording a collection and
// approving the payment cycle it lands in are different acts by different people
// — that separation is most of what an internal control is — and a model with a
// single "write" cannot express it.
const (
	actionRead    = "read"    // see it
	actionWrite   = "write"   // record something new, or amend it
	actionApprove = "approve" // decide, sign off, release money or lift a hold
	actionAdmin   = "admin"   // remove, suspend, or change who may do what
)

// The domains.
const (
	domainMilk       = "milk"       // collection at the booth, and what it weighed
	domainHerd       = "herd"       // animals: cattle, breeding, health, feed, farms
	domainMarket     = "market"     // buying and selling livestock
	domainLab        = "lab"        // samples, results, instruments, certificates
	domainPlant      = "plant"      // processing: batches, vessels, movements, stock
	domainCatalogue  = "catalogue"  // products and their prices
	domainSales      = "sales"      // orders, invoices, returns, payments in
	domainSettlement = "settlement" // what a producer is owed, and paying it
	domainRateCard   = "ratecard"   // the chart a collection is priced against
	domainIdentity   = "identity"   // which external code means which producer
	domainAudit      = "audit"      // the record of what was done
	domainTenant     = "tenant"     // the co-operative itself, and its people
	domainPlatform   = "platform"   // files, notifications, reports, ingestion
)

func perm(domain, action string) Permission { return Permission(domain + "." + action) }

// The permissions, named so a role reads as a sentence.
var (
	MilkRead  = perm(domainMilk, actionRead)
	MilkWrite = perm(domainMilk, actionWrite)
	// MilkApprove is held by one route: correcting a collection, which re-prices
	// it against the card in force and so changes what a producer is paid. The
	// collector who recorded a figure must not be the one who amends it.
	MilkApprove = perm(domainMilk, actionApprove)

	HerdRead  = perm(domainHerd, actionRead)
	HerdWrite = perm(domainHerd, actionWrite)
	HerdAdmin = perm(domainHerd, actionAdmin)

	MarketRead    = perm(domainMarket, actionRead)
	MarketWrite   = perm(domainMarket, actionWrite)
	MarketApprove = perm(domainMarket, actionApprove)

	LabRead    = perm(domainLab, actionRead)
	LabWrite   = perm(domainLab, actionWrite)
	LabApprove = perm(domainLab, actionApprove)

	PlantRead    = perm(domainPlant, actionRead)
	PlantWrite   = perm(domainPlant, actionWrite)
	PlantApprove = perm(domainPlant, actionApprove)

	CatalogueRead  = perm(domainCatalogue, actionRead)
	CatalogueWrite = perm(domainCatalogue, actionWrite)

	SalesRead    = perm(domainSales, actionRead)
	SalesWrite   = perm(domainSales, actionWrite)
	SalesApprove = perm(domainSales, actionApprove)

	SettlementRead    = perm(domainSettlement, actionRead)
	SettlementWrite   = perm(domainSettlement, actionWrite)
	SettlementApprove = perm(domainSettlement, actionApprove)

	RateCardRead  = perm(domainRateCard, actionRead)
	RateCardWrite = perm(domainRateCard, actionWrite)

	IdentityRead    = perm(domainIdentity, actionRead)
	IdentityWrite   = perm(domainIdentity, actionWrite)
	IdentityApprove = perm(domainIdentity, actionApprove)
	IdentityAdmin   = perm(domainIdentity, actionAdmin)

	AuditRead  = perm(domainAudit, actionRead)
	AuditWrite = perm(domainAudit, actionWrite)

	TenantRead  = perm(domainTenant, actionRead)
	TenantWrite = perm(domainTenant, actionWrite)
	TenantAdmin = perm(domainTenant, actionAdmin)

	PlatformRead    = perm(domainPlatform, actionRead)
	PlatformWrite   = perm(domainPlatform, actionWrite)
	PlatformApprove = perm(domainPlatform, actionApprove)
	PlatformAdmin   = perm(domainPlatform, actionAdmin)
)

// Public marks a procedure that needs no permission at all.
//
// It is a permission value rather than an absent entry so that "anyone may call
// this" is something somebody wrote down, and shows up in a diff when it
// changes. An absent entry means the opposite — see Decide.
const Public Permission = "public"

// Set is the permissions a caller holds.
type Set map[Permission]struct{}

// NewSet builds a set from permissions in any order.
func NewSet(perms ...Permission) Set {
	s := make(Set, len(perms))
	for _, p := range perms {
		s[p] = struct{}{}
	}
	return s
}

// ParseSet reads the comma-separated form a header carries.
func ParseSet(s string) Set {
	out := Set{}
	for _, part := range strings.Split(s, ",") {
		if p := Permission(strings.TrimSpace(part)); p != "" {
			out[p] = struct{}{}
		}
	}
	return out
}

// String renders a set for a header, sorted so the same permissions always
// produce the same string — a header that varies run to run is one nothing can
// be cached or compared on.
func (s Set) String() string {
	parts := make([]string, 0, len(s))
	for p := range s {
		parts = append(parts, string(p))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// Has reports whether the set carries a permission.
func (s Set) Has(p Permission) bool {
	_, ok := s[p]
	return ok
}

// Role is a named job, and the permissions it carries.
type Role struct {
	Name        string
	Description string
	Permissions Set
}

// The starter roles.
//
// These are a proposal drawn from the jobs a dairy co-operative actually has,
// not a general-purpose hierarchy, and they are meant to be argued with. The
// shape worth keeping is the separation of recording from approving: a
// collector records milk and an accountant approves what it is paid, and no
// single role does both. Everything else here is a detail somebody closer to
// the business should correct.
func Roles() map[string]Role {
	roles := []Role{
		{
			Name: "collector",
			Description: "Records collections at a booth or chilling centre. Sees " +
				"the herd and the day's own figures, and approves nothing.",
			Permissions: NewSet(
				MilkRead, MilkWrite,
				HerdRead,
				LabRead, LabWrite,
				RateCardRead,
				IdentityRead,
				PlatformRead,
			),
		},
		{
			Name: "clerk",
			Description: "The society office. Keeps the herd records, the catalogue " +
				"and the order book, and cannot touch what anyone is paid.",
			Permissions: NewSet(
				MilkRead, MilkWrite,
				HerdRead, HerdWrite,
				MarketRead, MarketWrite,
				LabRead,
				PlantRead, PlantWrite,
				CatalogueRead, CatalogueWrite,
				SalesRead, SalesWrite,
				SettlementRead,
				RateCardRead,
				IdentityRead, IdentityWrite,
				PlatformRead, PlatformWrite,
			),
		},
		{
			Name: "supervisor",
			Description: "Decides the things the platform declines to decide: slot " +
				"conflicts, quarantine, disputed readings. Not money.",
			Permissions: NewSet(
				MilkRead, MilkWrite, MilkApprove,
				HerdRead, HerdWrite,
				MarketRead, MarketWrite, MarketApprove,
				LabRead, LabWrite, LabApprove,
				PlantRead, PlantWrite, PlantApprove,
				CatalogueRead,
				SalesRead,
				SettlementRead,
				RateCardRead,
				IdentityRead, IdentityWrite, IdentityApprove,
				AuditRead,
				PlatformRead, PlatformWrite, PlatformApprove,
			),
		},
		{
			Name: "accountant",
			Description: "Values pools, approves cycles and pays producers, and " +
				"declares the rate card collections are priced against. Records no " +
				"milk: the figure and the payment for it are two people's work.",
			Permissions: NewSet(
				MilkRead,
				HerdRead,
				MarketRead,
				LabRead,
				PlantRead,
				CatalogueRead, CatalogueWrite,
				SalesRead, SalesWrite, SalesApprove,
				SettlementRead, SettlementWrite, SettlementApprove,
				RateCardRead, RateCardWrite,
				IdentityRead,
				AuditRead,
				PlatformRead, PlatformWrite,
			),
		},
		{
			Name: "auditor",
			Description: "Reads everything and changes nothing. The only role whose " +
				"permissions are entirely reads, which is what makes it worth having.",
			Permissions: NewSet(
				MilkRead, HerdRead, MarketRead, LabRead, PlantRead,
				CatalogueRead, SalesRead, SettlementRead, RateCardRead,
				IdentityRead, AuditRead, TenantRead, PlatformRead,
			),
		},
		{
			Name: "admin",
			Description: "The co-operative's own administrator: its people, its " +
				"settings, and anything a role below can do.",
			Permissions: all(),
		},
		{
			Name: "service",
			Description: "A machine: a device feeding collections in, or a " +
				"recomputation reconciling them. Records and reads; approves nothing, " +
				"because an approval nobody can be asked about is not an approval.",
			Permissions: NewSet(
				MilkRead, MilkWrite,
				HerdRead,
				LabRead, LabWrite,
				PlantRead, PlantWrite,
				SettlementRead, SettlementWrite,
				RateCardRead,
				IdentityRead, IdentityWrite,
				AuditRead, AuditWrite,
				PlatformRead, PlatformWrite,
			),
		},
	}

	out := make(map[string]Role, len(roles))
	for _, r := range roles {
		out[r.Name] = r
	}
	return out
}

// all is every permission this package defines, which is what admin holds.
//
// Derived rather than listed, so a permission added later is one the tenant's
// administrator can already exercise. That is the right default for this one
// role and the wrong default for every other, which is why only this one is
// built this way.
func all() Set {
	out := Set{}
	for _, p := range Table() {
		if p != Public {
			out[p] = struct{}{}
		}
	}
	return out
}

// ErrNoPermission is returned when a caller may not invoke a procedure.
type ErrNoPermission struct {
	Procedure string
	Needs     Permission
}

func (e *ErrNoPermission) Error() string {
	return fmt.Sprintf("this session may not call %s; it requires %s", e.Procedure, e.Needs)
}

// ErrUnknownProcedure is returned for a procedure with no entry in the table.
//
// Its own type because it means something different from a refusal: not "you may
// not", but "nobody decided who may", which is a fault in the platform rather
// than in the request.
type ErrUnknownProcedure struct{ Procedure string }

func (e *ErrUnknownProcedure) Error() string {
	return fmt.Sprintf("no permission is declared for %s, so nothing may call it", e.Procedure)
}

// Decide reports whether a caller holding these permissions may invoke a
// procedure.
//
// Fails closed in both directions. A procedure absent from the table is refused
// rather than allowed, so a route added without a permission stops working
// instead of standing open — the opposite convention would mean every new
// endpoint ships unprotected until somebody remembers.
func Decide(procedure string, held Set) error {
	needs, ok := Table()[normalise(procedure)]
	if !ok {
		return &ErrUnknownProcedure{Procedure: procedure}
	}
	if needs == Public {
		return nil
	}
	if held.Has(needs) {
		return nil
	}
	return &ErrNoPermission{Procedure: procedure, Needs: needs}
}

// normalise strips the leading slash, so a table keyed on
// "milk.v1.MilkService/RecordCollection" matches a request path of
// "/milk.v1.MilkService/RecordCollection".
func normalise(procedure string) string {
	return strings.TrimPrefix(procedure, "/")
}
