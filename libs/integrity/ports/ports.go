// Package ports says which port each service listens on when it is run
// directly, outside a container.
//
// It exists because the numbers were kept in two places and drifted. Every
// service declared its own default, the gateway declared where it expected to
// find each service, and fourteen of the pairs disagreed — so the gateway could
// not reach most of the platform on a developer's machine. Five services also
// claimed a port another service already had, which meant only one of each pair
// could start at all.
//
// Under compose and Kubernetes none of this applies: the address comes from the
// environment and every container is free to listen wherever it likes. These
// defaults are a development convenience — but a development convenience that
// does not work is just a trap, so both sides now read the same table and a test
// keeps it free of collisions.
package ports

import "fmt"

// The gateway, which is the only address a person outside the platform needs.
const Gateway = 8000

// The dairy and commerce services.
const (
	Tenant         = 8080
	Cattle         = 8081
	Milk           = 8082
	Breeding       = 8083
	Health         = 8084
	Feed           = 8085
	Farm           = 8086
	CattleMarket   = 8087
	ProductCatalog = 8088
	Inventory      = 8089
)

// The integrity layer occupies one contiguous block, deliberately: it is
// deployed and reasoned about as a unit.
const (
	ShadowSettlement = 8090
	Ingestion        = 8091
	Observation      = 8092
	Canonical        = 8093
	Pooling          = 8094
	Balance          = 8095
)

// The platform services. These sit above the integrity block rather than
// interleaved with it, so the block stays readable.
const (
	Order        = 8096
	Billing      = 8097
	Notification = 8098
	Reporting    = 8099
	Audit        = 8100
	File         = 8101
	Identity     = 8102
	Procurement  = 8103
)

// Addr is what a service passes to net/http as its listen address.
func Addr(port int) string { return fmt.Sprintf(":%d", port) }

// LocalURL is where another service reaches this one on the same machine.
func LocalURL(port int) string { return fmt.Sprintf("http://localhost:%d", port) }

// All is every assignment, named, so a test can prove none collide and a person
// can read the allocation in one place.
var All = map[string]int{
	"gateway":           Gateway,
	"tenant":            Tenant,
	"cattle":            Cattle,
	"milk":              Milk,
	"breeding":          Breeding,
	"health":            Health,
	"feed":              Feed,
	"farm":              Farm,
	"cattle-market":     CattleMarket,
	"product-catalog":   ProductCatalog,
	"inventory":         Inventory,
	"shadow-settlement": ShadowSettlement,
	"ingestion":         Ingestion,
	"observation":       Observation,
	"canonical":         Canonical,
	"pooling":           Pooling,
	"balance":           Balance,
	"order":             Order,
	"billing":           Billing,
	"notification":      Notification,
	"reporting":         Reporting,
	"audit":             Audit,
	"file":              File,
	"identity":          Identity,
	"procurement":       Procurement,
}
