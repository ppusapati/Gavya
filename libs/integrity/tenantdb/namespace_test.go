package tenantdb

import (
	"strings"
	"testing"
)

// The name a service's tables live under.
func TestAServiceNameBecomesASchemaName(t *testing.T) {
	cases := map[string]string{
		"order-service":             "order_service",
		"shadow-settlement-service": "shadow_settlement_service",
		"audit-service":             "audit_service",
		"  milk-service  ":          "milk_service",
	}
	for service, want := range cases {
		if got := NamespaceFor(service); got != want {
			t.Errorf("NamespaceFor(%q) = %q, want %q", service, got, want)
		}
	}
}

// Its own schema first, public second.
//
// The order is the whole arrangement. First means an unqualified table is the
// service's own; second means an unqualified audit_logs or gen_ulid() is the
// shared one. Reversed, a service that happened to name a table the same as
// something in public would silently read the wrong one — which is the failure
// the schemas were split to end.
func TestTheSearchPathIsItsOwnSchemaThenPublic(t *testing.T) {
	if got := searchPath("order_service"); got != "order_service, public" {
		t.Errorf("searchPath = %q, want \"order_service, public\"", got)
	}
	// No schema is public alone, not an empty path: a connection with an empty
	// search_path resolves nothing at all and every query fails on a relation
	// that is plainly there.
	if got := searchPath(""); got != "public" {
		t.Errorf("searchPath with no namespace = %q, want \"public\"", got)
	}
}

// A service name that could not be a schema name is refused rather than quoted
// around. This ends up in a startup parameter, and a name needing quotes is one
// somebody quotes wrongly somewhere else.
func TestAServiceNameThatIsNotASchemaNameIsRefused(t *testing.T) {
	for _, bad := range []string{"Order-Service", "order service", "1st-service", "", "order;drop"} {
		if _, err := NewPoolFor(t.Context(), "postgres://app@db/dairy?sslmode=require", bad); err == nil {
			t.Errorf("%q was accepted as a service name", bad)
		}
	}
}

// For the suites that connect with plain pgx against a database the platform
// built. Their queries are their service's, written unqualified.
func TestADSNCanBeGivenAServicesSearchPath(t *testing.T) {
	got, err := NamespacedDSN("postgres://app@db:5432/dairy?sslmode=require", "order-service")
	if err != nil {
		t.Fatal(err)
	}
	if want := settingOf(got, "search_path"); want != "order_service, public" {
		t.Errorf("search_path = %q, want \"order_service, public\"", want)
	}
	if settingOf(got, "sslmode") != "require" {
		t.Errorf("adding the search path lost sslmode: %q", got)
	}

	// A DSN that says so itself wins, on the same terms as the pool size and the
	// timeouts: something deliberate is not overwritten.
	const deliberate = "postgres://app@db/dairy?sslmode=require&search_path=masters,public"
	if got, _ := NamespacedDSN(deliberate, "order-service"); got != deliberate {
		t.Errorf("a DSN naming its own search_path was rewritten to %q", got)
	}

	// And the keyword form, which a deployment may well hand it.
	got, err = NamespacedDSN("host=db user=app sslmode=require", "milk-service")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "search_path='milk_service, public'") {
		t.Errorf("keyword-form DSN came back as %q", got)
	}
}
