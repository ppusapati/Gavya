// Command server runs the whole platform as one process.
//
// Twenty-nine services, one binary, one port. Every module is assembled by the
// same internal/app that its own main uses, so this is a deployment shape rather
// than a second implementation — a module wired differently here from the way it
// is wired alone is a module tested in only one of them.
//
// # WHY THIS EXISTS
//
// Under docker-compose every service published its own host port: 8080, 8081,
// 8082, and so on. Services take the tenant and the permissions they act on from
// headers the gateway sets, which is sound exactly as long as the gateway is the
// only way in — and it was one door of thirty. Anyone who could reach the host
// could send X-Gavya-Tenant and X-Gavya-Permissions of their choosing to any
// service and be believed.
//
// There is one door here. Nothing listens but this, the gateway's own middleware
// runs first in the same process, and the permissions reach authz.Guard on the
// request context rather than in a header, so inside the process there is no
// forgeable value at all. The header is still set, for code that reads headers,
// and it no longer carries the weight.
//
// # WHAT IT DOES NOT DO
//
// It does not merge the databases. Each module opens its own pool from its own
// DATABASE_URL, exactly as it does when it runs alone, because two services
// owning one schema is a different and much larger change than two services
// sharing one process — and because several of them define tables of the same
// name. Splitting this back into separate processes later is a deployment
// decision, not a rewrite.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/authz"
	"github.com/ppusapati/gavya/libs/integrity/observe"
	"github.com/ppusapati/gavya/libs/integrity/serve"

	gatewayconfig "github.com/ppusapati/gavya/services/gateway-service/config"
	gatewayhandler "github.com/ppusapati/gavya/services/gateway-service/handler"

	auditapp "github.com/ppusapati/gavya/services/audit-service/app"
	balanceapp "github.com/ppusapati/gavya/services/balance-service/app"
	billingapp "github.com/ppusapati/gavya/services/billing-service/app"
	breedingapp "github.com/ppusapati/gavya/services/breeding-service/app"
	canonicalapp "github.com/ppusapati/gavya/services/canonical-service/app"
	cattlemarketapp "github.com/ppusapati/gavya/services/cattle-market-service/app"
	cattleapp "github.com/ppusapati/gavya/services/cattle-service/app"
	farmapp "github.com/ppusapati/gavya/services/farm-service/app"
	feedapp "github.com/ppusapati/gavya/services/feed-service/app"
	fileapp "github.com/ppusapati/gavya/services/file-service/app"
	healthapp "github.com/ppusapati/gavya/services/health-service/app"
	identityapp "github.com/ppusapati/gavya/services/identity-service/app"
	ingestionapp "github.com/ppusapati/gavya/services/ingestion-service/app"
	inventoryapp "github.com/ppusapati/gavya/services/inventory-service/app"
	laboratoryapp "github.com/ppusapati/gavya/services/laboratory-service/app"
	materialapp "github.com/ppusapati/gavya/services/material-service/app"
	milkapp "github.com/ppusapati/gavya/services/milk-service/app"
	notificationapp "github.com/ppusapati/gavya/services/notification-service/app"
	observationapp "github.com/ppusapati/gavya/services/observation-service/app"
	orderapp "github.com/ppusapati/gavya/services/order-service/app"
	poolingapp "github.com/ppusapati/gavya/services/pooling-service/app"
	procurementapp "github.com/ppusapati/gavya/services/procurement-service/app"
	productcatalogapp "github.com/ppusapati/gavya/services/product-catalog-service/app"
	productionapp "github.com/ppusapati/gavya/services/production-service/app"
	reportingapp "github.com/ppusapati/gavya/services/reporting-service/app"
	settlementapp "github.com/ppusapati/gavya/services/settlement-service/app"
	shadowsettlementapp "github.com/ppusapati/gavya/services/shadow-settlement-service/app"
	tenantapp "github.com/ppusapati/gavya/services/tenant-service/app"

	"p9e.in/samavaya/packages/p9log"
)

// module is one service and the function that builds it.
type module struct {
	name  string
	build func(context.Context, *p9log.Helper) (serve.Registrar, *pgxpool.Pool, error)
}

func main() {
	log := p9log.NewHelper(p9log.DefaultLogger)
	ctx := context.Background()

	mux := http.NewServeMux()
	var pools []*pgxpool.Pool
	var checks []observe.Check
	closeAll := func() {
		// Reverse order, so the last thing opened is the first thing shut.
		for i := len(pools) - 1; i >= 0; i-- {
			pools[i].Close()
		}
	}

	for _, m := range modules() {
		h, pool, err := m.build(ctx, log)
		if err != nil {
			log.Errorf("%s: %v", m.name, err)
			closeAll()
			os.Exit(1)
		}
		pools = append(pools, pool)
		// Every module's database is a dependency of this process. Named
		// individually, so a readiness probe that goes red says which module
		// cannot reach its data rather than only that one cannot.
		checks = append(checks, observe.Check{Name: m.name, Ping: pool.Ping})
		h.Register(mux)
	}
	defer closeAll()
	log.Infof("mounted %d modules", len(modules()))

	// The gateway's front door, in front of everything, in this process.
	//
	// Its verifier still reaches identity-service over HTTP at its configured
	// URL — which in this shape is this same process, through the loopback
	// address. One extra hop for every request, and the alternative is a second
	// path into session verification that only the modulith uses. A verification
	// that behaves differently depending on how the platform is deployed is the
	// kind of difference that is discovered during an incident.
	gwcfg := gatewayconfig.Load()
	gw := gatewayhandler.New(gwcfg, log)

	// Guard after the gateway, not instead of it. The gateway refuses at the
	// front and this refuses again at the module, which is the same two layers
	// the separate-process deployment has.
	metrics := observe.NewMetrics()
	mux.HandleFunc("/readyz", observe.Ready(checks...))
	mux.HandleFunc("/metrics", metrics.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := serve.Unguarded(gwcfg.ServerAddr,
		gw.CORS(gw.Middleware(authz.Guard(mux))), metrics)

	go func() {
		log.Infof("gavya listening on %s", gwcfg.ServerAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Errorf("listen: %v", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
}

// modules is every service this binary serves.
//
// Listed rather than discovered, because a module that is silently absent is a
// platform missing a fifth of itself and answering 404 for it — which has
// happened here before, through docker-compose, and is what the gateway's
// deployment test exists to catch. The test beside this file compares this list
// against the services in the repository.
func modules() []module {
	return []module{
		{"audit-service", auditapp.Build},
		{"balance-service", balanceapp.Build},
		{"billing-service", billingapp.Build},
		{"breeding-service", breedingapp.Build},
		{"canonical-service", canonicalapp.Build},
		{"cattle-market-service", cattlemarketapp.Build},
		{"cattle-service", cattleapp.Build},
		{"farm-service", farmapp.Build},
		{"feed-service", feedapp.Build},
		{"file-service", fileapp.Build},
		{"health-service", healthapp.Build},
		{"identity-service", identityapp.Build},
		{"ingestion-service", ingestionapp.Build},
		{"inventory-service", inventoryapp.Build},
		{"laboratory-service", laboratoryapp.Build},
		{"material-service", materialapp.Build},
		{"milk-service", milkapp.Build},
		{"notification-service", notificationapp.Build},
		{"observation-service", observationapp.Build},
		{"order-service", orderapp.Build},
		{"pooling-service", poolingapp.Build},
		{"procurement-service", procurementapp.Build},
		{"product-catalog-service", productcatalogapp.Build},
		{"production-service", productionapp.Build},
		{"reporting-service", reportingapp.Build},
		{"settlement-service", settlementapp.Build},
		{"shadow-settlement-service", shadowsettlementapp.Build},
		{"tenant-service", tenantapp.Build},
	}
}
