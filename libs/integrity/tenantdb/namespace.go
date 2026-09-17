package tenantdb

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// A schema of its own for each service.
//
// # WHAT WAS ACTUALLY THE CASE
//
// Every service in both deployments points at the same database. Every
// DATABASE_URL in docker-compose.yaml names `dairy`; the modulith gets one
// DATABASE_URL for all twenty-eight of its modules; and deploy/postgres-init
// applies every services/*/internal/db/schema.sql into it in a loop.
//
// billing-service and order-service both define a table called `invoices`, and
// they are not the same table. The schemas are written CREATE TABLE IF NOT
// EXISTS because they have to be re-runnable, so the second definition was not
// an error — it was skipped, silently, and billing sorts first. Every write
// order-service made to an invoice failed on a column that was never created.
// It had never worked outside a test, because the end-to-end suite gives each
// service a database of its own and is therefore the one arrangement where the
// collision cannot happen.
//
// # WHY A NAMESPACE RATHER THAN A RENAME
//
// Renaming the two tables would have fixed today and left the mechanism intact:
// the twenty-ninth service defines a table somebody else already has, and the
// same silence follows. A schema per service makes the collision impossible
// rather than resolved, and it costs one startup parameter.
//
// # WHAT STAYS IN public
//
// The things every service genuinely shares, and only those. `audit_logs` —
// audit-service's schema is that one table, and twenty-five services write to
// it through libs/integrity/audit. The ULID polyfill. gavya_current_tenant()
// and the isolation machinery. A service's search path is its own schema then
// public, so those resolve and nothing else does.
//
// # WHAT THE SPLIT DOES NOT MEAN
//
// It does not mean the services stop referring to each other. Twenty-two
// enforced references cross services — fourteen at cattle-service's `cattle`,
// which a breeding cycle, a vaccination, a milk session, a vet visit and a
// treatment all name, and four at product-catalog's `skus`.
//
// This paragraph said the opposite when it was written. I had checked the
// schema files' REFERENCES clauses, found none across services, and said so.
// The keys are not there: they are in gavya_reference_decisions, each with a
// written reason, and gavya_enforce_references creates them during deployment.
// Reading the declarations rather than what the platform builds is the same
// mistake as trusting a test that runs in the one arrangement where the bug
// cannot happen — which is how the invoices collision survived this long.
//
// So enforcement finds both ends of a reference wherever they are. What the
// split buys is that a table *name* cannot collide; the couplings between
// services are declared, and stay.

// namespaceShape is what a schema name may contain. Narrow on purpose: this
// ends up in a startup parameter, and a name that needed quoting would be a
// name somebody quotes wrongly somewhere else.
var namespaceShape = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// NamespaceFor is the schema a service's tables live in.
//
// Derived from the service's own directory name — order-service becomes
// order_service — rather than kept in a table somewhere. A mapping is a second
// list to maintain and the interesting failure is a service missing from it,
// which reads as a service in public, which is the collision this exists to
// prevent.
func NamespaceFor(service string) string {
	return strings.ReplaceAll(strings.TrimSpace(service), "-", "_")
}

// NewPoolFor opens a pool whose queries resolve in this service's schema.
//
// The service's own name, not SERVICE_NAME from the environment. The modulith
// runs twenty-eight modules in one process and SERVICE_NAME is `gavya` for all
// of them, so a pool that read it would put every module in one namespace and
// rebuild the collision inside the shape that was meant to be the way out.
func NewPoolFor(ctx context.Context, dsn, service string) (*pgxpool.Pool, error) {
	namespace := NamespaceFor(service)
	if !namespaceShape.MatchString(namespace) {
		return nil, fmt.Errorf("tenantdb: %q is not a service name this can make a schema "+
			"from; it has to be lower case letters, digits and hyphens", service)
	}
	return newPool(ctx, dsn, namespace)
}

// searchPath is what a pool's connections resolve names against.
//
// The service's schema first and public second, so an unqualified table is the
// service's own and an unqualified audit_logs or gen_ulid() is the shared one.
// Set as a startup parameter rather than a statement per acquire: it travels in
// the connection's opening packet, costs nothing per query, and cannot be left
// unset by a path somebody forgot.
func searchPath(namespace string) string {
	if namespace == "" {
		return "public"
	}
	return namespace + ", public"
}

// NamespacedDSN adds a service's search path to a DSN.
//
// For code that connects with plain pgx against a database the platform built —
// the repository suites tagged dbintegration, which read TEST_DATABASE_URL and
// are handed a database holding every service's schema. Their queries are their
// service's queries, written unqualified, and in production they run on a pool
// from NewPoolFor. A test connection without the same search path is testing
// against an arrangement the platform does not have.
//
// Returns the DSN unchanged when it already names a search_path, on the same
// terms as everything else here: something deliberate wins.
func NamespacedDSN(dsn, service string) (string, error) {
	namespace := NamespaceFor(service)
	if namespace != "" && !namespaceShape.MatchString(namespace) {
		return "", fmt.Errorf("tenantdb: %q is not a service name this can make a schema from", service)
	}
	if settingOf(dsn, "search_path") != "" {
		return dsn, nil
	}

	trimmed := strings.TrimSpace(dsn)
	if strings.HasPrefix(trimmed, "postgres://") || strings.HasPrefix(trimmed, "postgresql://") {
		u, err := url.Parse(trimmed)
		if err != nil {
			return "", fmt.Errorf("tenantdb: %w", err)
		}
		q := u.Query()
		q.Set("search_path", searchPath(namespace))
		u.RawQuery = q.Encode()
		return u.String(), nil
	}
	return trimmed + " search_path='" + searchPath(namespace) + "'", nil
}
