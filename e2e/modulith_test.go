//go:build e2e

// The whole platform as one process, actually running.
//
// The modulith is the shape that ships: twenty-eight modules, one binary, one
// port, with the gateway's own middleware in front of them in the same process.
// It had five tests and every one of them was structural — every service
// mounted, no two modules claiming a route, every service-level defence wrapped
// here too. Nothing had ever started it.
//
// So the deployment shape with the least coverage was the one meant to be
// deployed, and the arrangement it exists to create — one door instead of
// twenty-nine — had never been shown to hold. This is also the shape the
// invoices collision would have been found in years earlier, if anything had run
// it: twenty-eight modules, one database, which is exactly where two services
// defining a table of the same name stops being theoretical.
//
// # IT SIGNS IN
//
// Unlike the harness in harness_test.go, which calls services directly and sets
// the tenant and the permissions in headers because there is no gateway between
// two services. Through the modulith there is one, in front, in-process, and it
// strips exactly those headers and replaces them from a verified session.
//
// That makes this the only suite in the repository that goes through the front
// door the way the console and the collection bench do — and the only one that
// can show the front door is there at all.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ppusapati/gavya/libs/integrity/authz"
	"github.com/ppusapati/gavya/libs/integrity/credential"
	"github.com/ppusapati/gavya/libs/integrity/tenantdb"
)

const (
	modulithDatabase = "e2e_modulith"
	modulithPassword = "a modulith password"
	// The tenant and the person this suite acts as. Seeded here rather than
	// borrowed from the isolation seed so that a change there cannot quietly
	// change what this is allowed to do.
	modulithTenant = "T_MODULITH_0000000000000M"
	modulithUser   = "US_MODULITH_00000000000M"
	modulithRole   = "RL_MODULITH_00000000000M"
	modulithEmail  = "operator@modulith.test"
)

var (
	modulithOnce sync.Once
	modulithBase string
	modulithErr  error
)

// theModulith starts it once and returns its one address.
func theModulith(t *testing.T) string {
	t.Helper()
	_ = dsn(t, "postgres")
	modulithOnce.Do(func() { modulithBase, modulithErr = startTheModulith(t) })
	if modulithErr != nil {
		t.Fatalf("start the modulith: %v", modulithErr)
	}
	return modulithBase
}

func startTheModulith(t *testing.T) (string, error) {
	return startAModulith(t, modulithDatabase, "", true)
}

// startAModulith builds one and runs it. shared registers it for the route
// coverage sweep; a second instance started to measure something does not want
// to be counted as the platform.
func startAModulith(t *testing.T, database, poolSize string, shared bool, extraEnv ...string) (string, error) {
	t.Helper()

	// A database of its own, built the way a deployment builds one: every
	// schema in its own namespace, the isolation sweeps, the keys.
	owner, _ := builtFrom(t, database)
	seedModulithIdentity(t, owner)

	root := workspaceRoot(t)
	// Not t.TempDir, and the process below is not killed by t.Cleanup either.
	// This starts inside a sync.Once, so the t it happens to hold is whichever
	// test asked first — and its cleanups run when that test ends, taking the
	// server away from every test after it. TestMain owns both.
	binDir, err := os.MkdirTemp("", "gavya-e2e-modulith")
	if err != nil {
		return "", err
	}
	sharedBinDirs = append(sharedBinDirs, binDir)
	bin := filepath.Join(binDir, "gavya-modulith")
	build := exec.Command("go", "build", "-o", bin, "./cmd/server")
	build.Dir = filepath.Join(root, "services", "modulith")
	if out, err := build.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build the modulith: %v\n%s", err, out)
	}

	addr := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	base := "http://" + addr

	env := append(os.Environ(),
		"SERVICE_NAME=gavya",
		"SERVER_ADDR="+addr,
		"DATABASE_URL="+modulithDSN(t, database, poolSize),
		// observation-service will not start without it, and the modulith
		// mounts observation-service.
		"MEASUREMENT_REGIME=IN_LEGAL_METROLOGY",
	)
	// Every upstream is this process, through the loopback address — which is
	// what docker-compose.modulith.yaml sets and why: the gateway's verifier
	// and settlement's reader still speak HTTP, to themselves, rather than
	// having a second code path only this deployment uses.
	for _, name := range modulithUpstreamVars(t, root) {
		env = append(env, name+"="+base)
	}
	env = append(env, extraEnv...)

	cmd := exec.Command(bin)
	cmd.Env = env
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start: %w", err)
	}
	sharedProcs = append(sharedProcs, cmd)
	if shared {
		// And what it served counts towards the route coverage in
		// coverage_test.go. Recorded by TestMain's sweep, which reads this map
		// before killing anything.
		sharedBaseURLs["modulith"] = base
	}

	deadline := time.Now().Add(60 * time.Second)
	lastSaid := "nothing answered"
	for time.Now().Before(deadline) {
		if c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
			_ = c.Close()
			resp, err := http.Get(base + "/readyz")
			if err == nil {
				said, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return base, nil
				}
				// Kept so the failure names the module that could not reach its
				// data. /readyz answers with the dependency by name, which is
				// the whole reason it does.
				lastSaid = fmt.Sprintf("%d %s", resp.StatusCode, strings.TrimSpace(string(said)))
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return "", fmt.Errorf("the modulith did not become ready on %s: %s", addr, lastSaid)
}

// modulithDSN is the database URL this instance connects with.
//
// poolSize, when a caller gives one, becomes pool_max_conns on the DSN — which
// tenantdb obeys, because a DSN that says something deliberate wins over the
// platform's default. Only this harness passes one; a deployment sets nothing and
// gets tenantdb.MaxConns.
func modulithDSN(t *testing.T, database, poolSize string) string {
	t.Helper()
	d := asRole(dsn(t, database), "gavya_app")
	if poolSize == "" {
		return d
	}
	sep := "?"
	if strings.Contains(d, "?") {
		sep = "&"
	}
	return d + sep + "pool_max_conns=" + poolSize
}

// modulithUpstreamVars is every environment variable the modulith's compose file
// points at the process itself.
//
// Read out of that file rather than listed here. The list is thirty-one long and
// a copy of it would drift; worse, the drift would be silent — a module whose
// URL is unset calls an empty address, which reads as that module being broken
// rather than as this harness being incomplete.
func modulithUpstreamVars(t *testing.T, root string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "docker-compose.modulith.yaml"))
	if err != nil {
		t.Fatalf("read the modulith compose file: %v", err)
	}
	var names []string
	for _, line := range strings.Split(string(b), "\n") {
		name, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok || !strings.Contains(value, "127.0.0.1:8080") {
			continue
		}
		names = append(names, name)
	}
	if len(names) < 25 {
		t.Fatalf("found %d upstream variables in the modulith compose file; there are "+
			"about thirty, so this is not reading it", len(names))
	}
	sort.Strings(names)
	return names
}

// seedModulithIdentity puts one tenant, one person and one role in the database,
// so this suite can sign in the way a person does.
func seedModulithIdentity(t *testing.T, owner *pgx.Conn) {
	t.Helper()
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := owner.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}

	exec(`INSERT INTO tenants (id,name,slug,status,contact_email,currency,currency_scale,created_by,updated_by)
	      VALUES ($1,'Modulith Society','modulith','active','modulith@test.invalid','INR',2,'seed','seed')
	      ON CONFLICT (id) DO NOTHING`, modulithTenant)

	// A role the platform defines, by name.
	//
	// The permissions a session carries come from authz.Roles() keyed on the
	// role's name — not from the permissions column, which is what the
	// administration procedures write and what a reader would reasonably assume.
	// A role named anything else verifies fine, reports its name, and holds
	// nothing; identity-service says so in its log and answers with an empty set,
	// which is the designed behaviour and which this test got wrong first time
	// by inventing a name.
	//
	// clerk rather than a wider one because it is the role an office actually
	// has, and because what is being tested is that permissions reach the module
	// at all — not how many of them there are.
	const roleName = "clerk"
	if _, defined := authz.Roles()[roleName]; !defined {
		t.Fatalf("the platform does not define a role called %q, so a session with it "+
			"would carry no permissions", roleName)
	}
	perms := make([]string, 0)
	for p := range authz.Roles()[roleName].Permissions {
		perms = append(perms, string(p))
	}
	sort.Strings(perms)
	exec(`INSERT INTO roles (id,tenant_id,name,permissions,created_by,updated_by)
	      VALUES ($1,$2,$3,$4,'seed','seed')
	      ON CONFLICT (id) DO NOTHING`,
		modulithRole, modulithTenant, roleName, "{"+strings.Join(perms, ",")+"}")

	// A real hash, at a cost that keeps the suite quick without changing which
	// code paths run — the same parameters identity-service's own tests use.
	cheap := credential.Params{Memory: 8 * 1024, Time: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	hash, err := credential.HashWith(modulithPassword, cheap)
	if err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO users (id,email,email_normalised,full_name,password_hash,created_by,updated_by)
	      VALUES ($1,$2::text,lower($2::text),'Modulith Operator',$3,'seed','seed')
	      ON CONFLICT (id) DO NOTHING`, modulithUser, modulithEmail, hash)
	exec(`INSERT INTO tenant_memberships (id,tenant_id,user_id,role_id,status,created_by,updated_by)
	      VALUES ($1,$2,$3,$4,'active','seed','seed')
	      ON CONFLICT (id) DO NOTHING`,
		"MB_MODULITH_00000000000M", modulithTenant, modulithUser, modulithRole)
}

// aCaller posts to the one port the way a client outside the platform does.
//
// Its own small client rather than svcclient, because svcclient is what services
// use to call each other and has no session to present — there is no gateway
// between two services. What reaches the modulith from outside carries a bearer
// token and nothing else, which is what the console and the bench send.
type aCaller struct {
	base    string
	session string
	extra   map[string]string
}

func (c aCaller) call(t *testing.T, procedure string, in any) (int, map[string]any) {
	t.Helper()
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("POST", c.base+"/"+procedure, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.session != "" {
		req.Header.Set("Authorization", "Bearer "+c.session)
	}
	for k, v := range c.extra {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s: %v", procedure, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

// signedIn returns a caller holding a session the modulith issued.
func signedIn(t *testing.T) aCaller {
	t.Helper()
	anonymous := aCaller{base: theModulith(t)}
	code, body := anonymous.call(t, "gavya.identity.v1.IdentityService/SignIn", map[string]string{
		"email": modulithEmail, "password": modulithPassword, "tenant_id": modulithTenant,
	})
	if code != http.StatusOK {
		t.Fatalf("SignIn through the modulith: %d %v", code, body)
	}
	token, _ := body["session_id"].(string)
	if token == "" {
		t.Fatalf("SignIn returned no session id: %v", body)
	}
	// What the gateway will get back when it verifies this on every request.
	// Asserted here because a session that verifies with no permissions refuses
	// every procedure with permission_denied, which reads as a role that needs
	// widening rather than as a session that carried nothing.
	code, verified := anonymous.call(t, "gavya.identity.v1.IdentityService/VerifySession",
		map[string]string{"session_id": token})
	if code != http.StatusOK {
		t.Fatalf("the session this sign-in issued does not verify: %d %v", code, verified)
	}
	held, _ := verified["permissions"].([]any)
	if len(held) == 0 {
		t.Fatalf("the session verifies and carries no permissions, so every procedure "+
			"will be refused: %v", verified)
	}

	return aCaller{base: anonymous.base, session: token}
}

// There is one door, and it is shut to somebody who has not signed in.
//
// The whole argument for this shape is in this assertion. Under compose every
// service published its own host port, and services take the tenant and the
// permissions from headers the gateway sets — which is sound exactly as long as
// the gateway is the only way in, and it was one door of thirty. Anyone who
// could reach the host could send an X-Gavya-Tenant of their choosing to any
// service and be believed.
//
// Nothing had ever checked that the modulith closes that. It was argued for in a
// comment and tested structurally — the middleware is wired in the right order —
// and a wiring test cannot tell a middleware that refuses from one that is
// present and waves everything through.
func TestTheModulithRefusesACallerWhoHasNotSignedIn(t *testing.T) {
	anonymous := aCaller{base: theModulith(t)}

	code, body := anonymous.call(t, "cattle.v1.CattleService/ListCattle", map[string]any{
		"tenant_id": modulithTenant, "limit": 10,
	})
	if code != http.StatusUnauthorized {
		t.Fatalf("an unauthenticated call to a module answered %d %v, want 401 — the "+
			"modulith's one advantage over publishing twenty-nine ports is that this "+
			"is refused", code, body)
	}

	// And the way in is open, or nobody could ever sign in.
	code, _ = anonymous.call(t, "gavya.identity.v1.IdentityService/SignIn", map[string]string{
		"email": modulithEmail, "password": "not the password", "tenant_id": modulithTenant,
	})
	if code == http.StatusUnauthorized {
		// A wrong password is also 401; what must not happen is the gateway
		// refusing before identity-service ever sees it, which would be the same
		// code for a different reason. Distinguished by the successful sign-in
		// every other test here performs.
		t.Log("a wrong password is refused, as it should be")
	}
}

// The tenant comes from the session, and a caller cannot choose it.
//
// This is the forgery the modulith exists to make impossible, and it had never
// been tried. The caller sends an X-Gavya-Tenant naming somebody else's society
// along with their own valid session; the gateway strips what the client sent
// before it does anything else, and the tenant that reaches the module is the
// one the session says.
func TestTheModulithIgnoresATenantTheCallerAsserts(t *testing.T) {
	caller := signedIn(t)
	caller.extra = map[string]string{
		"X-Gavya-Tenant":      "T_SOMEBODY_ELSES_00000000",
		"X-Gavya-Permissions": "cattle.write,settlement.approve",
		"X-Gavya-Actor":       "somebody else",
	}

	// A read that is scoped by tenant. If the forged header won, this would be
	// answered against a tenant the caller has no membership in.
	code, body := caller.call(t, "cattle.v1.CattleService/ListCattle", map[string]any{
		"tenant_id": modulithTenant, "limit": 10,
	})
	if code != http.StatusOK {
		t.Fatalf("a signed-in read answered %d %v", code, body)
	}

	// A change, so there is a row to look at. Written by a module that took its
	// tenant from whatever reached it.
	made := caller.call2xx(t, "cattle.v1.CattleService/CreateCattle", map[string]any{
		"tenant_id": modulithTenant, "tag_number": newID("tag"),
		"name": "Lakshmi", "gender": "F", "weight": 410.5,
		"created_by": modulithUser,
	})
	animal, _ := made["cattle"].(map[string]any)
	if animal == nil {
		t.Fatalf("CreateCattle returned no animal: %v", made)
	}

	// The row is where the answer is. It was written under whichever tenant
	// reached the module, and the forged header named a different one.
	owner, err := pgx.Connect(context.Background(),
		lookingEverywhere(t, dsn(t, modulithDatabase)))
	if err != nil {
		t.Fatalf("connect to the modulith's database: %v", err)
	}
	defer owner.Close(context.Background())

	var tenant string
	if err := owner.QueryRow(context.Background(),
		`SELECT tenant_id FROM cattle_service.cattle WHERE id = $1`,
		animal["id"]).Scan(&tenant); err != nil {
		t.Fatalf("read the animal back: %v", err)
	}
	if tenant != modulithTenant {
		t.Errorf("the animal was recorded against tenant %q and the caller's session "+
			"names %q — a header the caller sent decided whose society this was",
			tenant, modulithTenant)
	}
	if tenant == "T_SOMEBODY_ELSES_00000000" {
		t.Error("the tenant the caller asserted in a header is the one the row carries")
	}
}

// call2xx is call, with the failure reported rather than returned.
func (c aCaller) call2xx(t *testing.T, procedure string, in any) map[string]any {
	t.Helper()
	code, body := c.call(t, procedure, in)
	if code < 200 || code > 299 {
		t.Fatalf("%s answered %d: %v", procedure, code, body)
	}
	return body
}

// Two modules, one port, one process.
//
// order-service's invoicing is here on purpose. It is the procedure that had
// never worked in either deployed shape — billing-service and order-service both
// define a table called invoices and one database held only billing's — and this
// is the shape it was broken in. Running it here is the proof the schemas being
// split actually fixed the thing it was meant to fix, in the deployment rather
// than in a test that gives each service a database of its own.
func TestTheModulithServesEveryModuleFromOneDoor(t *testing.T) {
	caller := signedIn(t)

	// A product and an order, through the front door.
	product := caller.call2xx(t, "productcatalog.v1.ProductCatalogService/CreateProduct", map[string]any{
		"tenant_id": modulithTenant, "name": "Toned Milk 500ml", "slug": newID("slug"),
		"product_type": "simple", "status": "active", "created_by": modulithUser,
	})
	if product["product"] == nil {
		t.Fatalf("CreateProduct returned nothing: %v", product)
	}

	order := caller.call2xx(t, "order.v1.OrderService/CreateOrder", map[string]any{
		"tenant_id": modulithTenant, "customer_id": "CU_MODULITH_0000000000M0",
		"currency": "INR", "created_by": modulithUser,
	})
	placed, _ := order["order"].(map[string]any)
	if placed == nil {
		t.Fatalf("CreateOrder returned nothing: %v", order)
	}

	// An order can only be invoiced once it is confirmed, and confirmed once it
	// has something on it.
	sku := caller.call2xx(t, "productcatalog.v1.ProductCatalogService/CreateSKU", map[string]any{
		"tenant_id": modulithTenant, "product_id": product["product"].(map[string]any)["id"],
		"code": newID("sku"), "price": "45.00", "currency": "INR",
		"status": "active", "created_by": modulithUser,
	})
	made, _ := sku["sku"].(map[string]any)
	if made == nil {
		t.Fatalf("CreateSKU returned nothing: %v", sku)
	}
	caller.call2xx(t, "order.v1.OrderService/AddOrderItem", map[string]any{
		"tenant_id": modulithTenant, "order_id": placed["id"],
		"sku_id": made["id"], "product_id": product["product"].(map[string]any)["id"],
		"quantity": "2", "unit_price": "45.00", "tax_rate": "0",
		"created_by": modulithUser,
	})
	caller.call2xx(t, "order.v1.OrderService/ConfirmOrder", map[string]any{
		"id": placed["id"], "tenant_id": modulithTenant, "updated_by": modulithUser,
	})

	// The invoice. This is the one that used to fail on a column that was never
	// created, in exactly this arrangement.
	invoice := caller.call2xx(t, "order.v1.OrderService/GenerateInvoice", map[string]any{
		"tenant_id": modulithTenant, "order_id": placed["id"], "created_by": modulithUser,
	})
	billed, _ := invoice["invoice"].(map[string]any)
	if billed == nil {
		t.Fatalf("GenerateInvoice returned nothing: %v", invoice)
	}
	if billed["order_id"] != placed["id"] {
		t.Errorf("the invoice names order %v and it was raised for %v — order-service's "+
			"invoices table is the one it defines, or it is billing-service's",
			billed["order_id"], placed["id"])
	}
}

// What a self-call costs, measured rather than reasoned about.
//
// Every internal call in the modulith goes out to its own address and back —
// deliberately, so that session verification has one implementation rather than
// a second path only this shape uses. The plan for this shape said the hop was
// worth measuring before deciding anything about it, and this is the
// measurement.
//
// It is not latency that turns out to matter. The per-caller rate limit is
// applied by serve.Unguarded, outside the gateway's middleware — so it runs
// before the gateway sets X-Gavya-Client, and ratelimit.ByClient falls back to
// the peer address. For a self-call that peer is the process itself.
//
// So the verifier's call is charged to a bucket of its own, keyed on the
// loopback address, and one such call happens for every authenticated request in
// the platform. The number below is what that costs a caller.
func TestWhatASelfCallCostsTheRateLimit(t *testing.T) {
	// Small on purpose, and its own instance: the harness raises the limit out
	// of the way for every other suite, which is right — dozens of tests from
	// one address is not a client any deployment sees — and would hide exactly
	// what this is measuring.
	const burst = 20
	//
	// One connection per module rather than tenantdb.MaxConns: this instance is
	// counting rate-limit tokens, not measuring capacity, and a second modulith
	// at the platform's real pool size is another two hundred and twenty-four
	// connections. That is what took the whole suite over the test server's
	// limit the first time this ran — "remaining connection slots are reserved"
	// from every test after it, which is the arithmetic in
	// TestThePoolsFitTheDatabase arriving for real.
	base, err := startAModulith(t, "e2e_modulith_limited", "1", false,
		"GAVYA_RATE_PER_SECOND=1", fmt.Sprintf("GAVYA_RATE_BURST=%d", burst))
	if err != nil {
		t.Fatalf("start a rate-limited modulith: %v", err)
	}

	anonymous := aCaller{base: base}
	code, body := anonymous.call(t, "gavya.identity.v1.IdentityService/SignIn", map[string]string{
		"email": modulithEmail, "password": modulithPassword, "tenant_id": modulithTenant,
	})
	if code != http.StatusOK {
		t.Fatalf("SignIn: %d %v", code, body)
	}
	token, _ := body["session_id"].(string)
	caller := aCaller{base: base, session: token}

	// Read after read, from one caller, until something is refused. A refusal
	// arrives as 429 either to this caller or to the verifier — and the verifier
	// reporting one comes back as unauthenticated or unavailable, which is the
	// more misleading of the two.
	served := 0
	var refusal string
	for i := 0; i < burst*2; i++ {
		code, body := caller.call(t, "cattle.v1.CattleService/ListCattle", map[string]any{
			"tenant_id": modulithTenant, "limit": 1,
		})
		if code == http.StatusOK {
			served++
			continue
		}
		refusal = fmt.Sprintf("%d %v", code, body)
		break
	}

	t.Logf("burst %d, one caller: %d reads served before %q", burst, served, refusal)

	if served == 0 {
		t.Fatalf("nothing was served at all, so this is measuring something else: %s", refusal)
	}

	// One token a request, and the sign-in above spent one of them — so a burst
	// of twenty buys nineteen reads, which is what this measures.
	//
	// It measured nine before ratelimit.ExemptProbes stopped counting the
	// gateway's own call to VerifySession, and then answered 503 about an
	// identity service that was running. Two tokens a request rather than one.
	//
	// Exact rather than generous: at one token a second the bucket does not
	// refill meaningfully inside a loop this short, so an off-by-one here is a
	// real change in what a request costs and worth failing on.
	if served < burst-1 {
		t.Errorf("one caller got %d reads out of a burst of %d, so each authenticated "+
			"request spends about %.1f tokens.\n"+
			"The extra one is the gateway's own call to VerifySession. It leaves this "+
			"process and comes back, and the limiter sits outside the middleware that "+
			"sets X-Gavya-Client — so it keys on the loopback address, and every "+
			"authenticated request in the platform shares that one bucket.\n"+
			"What that means deployed: the general rate limit, which exists to bound "+
			"one client, becomes the platform's total ceiling on authenticated "+
			"traffic. And it fails as %q — about a process that is running, and in "+
			"this shape is the one answering.",
			served, burst, float64(burst)/float64(served), refusal)
	}
}

// What the scrape would actually get.
//
// deploy/monitoring/prometheus.modulith.yml points at this process and
// gateway-service's tests check that it points at the right port with the right
// job name. That is the configuration being right; whether the endpoint carries
// anything is a different claim, and the one that matters.
//
// It matters here more than anywhere. libs/integrity/tenantdb/watch.go sums the
// pool statistics across every pool in the process, and the reason written there
// is the modulith — observe.Publish is keyed by name, so a gauge per pool would
// leave the twenty-eighth module silently the only one reported. That summing
// was written for a shape nothing had ever started.
func TestTheModulithPublishesWhatTheScrapeReads(t *testing.T) {
	caller := signedIn(t)

	// Some traffic, so the per-procedure counters have something in them.
	caller.call2xx(t, "cattle.v1.CattleService/ListCattle", map[string]any{
		"tenant_id": modulithTenant, "limit": 5,
	})

	resp, err := http.Get(theModulith(t) + "/metrics")
	if err != nil {
		t.Fatalf("scrape the modulith: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the metrics endpoint answered %d — Prometheus would read the whole "+
			"platform as down", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)

	// Every family the alert rules name, from one endpoint.
	for _, name := range []string{
		"gavya_requests_total",
		"gavya_request_duration_seconds_bucket",
		"gavya_requests_in_flight",
		"gavya_uptime_seconds",
		"gavya_db_pool_max_conns",
		"gavya_db_pool_acquired_conns",
		"gavya_db_pool_empty_acquires_total",
		"gavya_db_queries_total",
	} {
		if !strings.Contains(body, "\n"+name+" ") && !strings.Contains(body, "\n"+name+"{") {
			t.Errorf("%s is not on the modulith's metrics endpoint, so the alert that "+
				"names it would evaluate to no data forever", name)
		}
	}

	// The procedure that was just called, by name. A counter with no procedure
	// labels is a counter that would satisfy the check above and tell nobody
	// which module is failing.
	if !strings.Contains(body, `procedure="cattle.v1.CattleService/ListCattle"`) {
		t.Error("the request counters carry no label for a procedure that was just " +
			"called, so ProceduresFailing could never name one")
	}

	// And the summing, which is the thing this shape exists to exercise.
	//
	// Twenty-eight modules, each with a pool of tenantdb.MaxConns, in one
	// process. A gauge reporting one pool's eight would look perfectly healthy
	// and would be under-reporting the process by a factor of twenty-eight —
	// against a server limit this platform now does arithmetic about.
	max := metricValue(t, body, "gavya_db_pool_max_conns")
	modules := float64(len(modulithModules(t)))
	if want := modules * float64(tenantdb.MaxConns); max != want {
		t.Errorf("the process reports it may hold %v connections; %v modules at %d each "+
			"is %v. A gauge that reports one pool rather than the sum reads as healthy "+
			"while the process is holding twenty-eight times what it says.",
			max, modules, tenantdb.MaxConns, want)
	}
}

// metricValue reads one unlabelled metric off a scrape.
func metricValue(t *testing.T, body, name string) float64 {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		got, rest, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok || got != name {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(rest), 64)
		if err != nil {
			t.Fatalf("%s is %q, which is not a number", name, rest)
		}
		return v
	}
	t.Fatalf("%s is not on the endpoint", name)
	return 0
}

// modulithModules is the list of modules the binary mounts, read out of its own
// source rather than counted here — a number written in two places is a number
// that stops agreeing.
func modulithModules(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(workspaceRoot(t), "services", "modulith", "cmd", "server", "main.go"))
	if err != nil {
		t.Fatalf("read the modulith's module list: %v", err)
	}
	found := regexp.MustCompile(`\{"([a-z-]+-service)",\s+\w+\.Build\}`).FindAllStringSubmatch(string(b), -1)
	if len(found) < 20 {
		t.Fatalf("read %d modules out of the modulith's source; there are twenty-eight, "+
			"so the pattern has stopped matching", len(found))
	}
	var names []string
	for _, m := range found {
		names = append(names, m[1])
	}
	return names
}
