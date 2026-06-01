package serviceclient

import (
	"go.uber.org/fx"

	"p9e.in/samavaya/packages/events/bus"
)

// Module is the default fx wiring for serviceclient.
//
// It provides a Registry backed by bus.Default so every service in the
// monolith shares one bus unless they explicitly ask Registry for a named
// one. Services register their handlers into the registry's default bus on
// startup (via fx.Invoke) and construct Invokers/Publishers/Subscribers
// against it.
//
// Composition roots that want a different topology (e.g. a dedicated bus
// per service, or a Kafka-backed publisher for split deployment) should
// replace this module rather than using it.
var Module = fx.Module("serviceclient",
	fx.Provide(
		provideBus,
		NewRegistryWithDefault,
	),
)

// provideBus exposes bus.Default via fx so consumers can take a
// *bus.EventBus parameter directly when they don't want the registry.
func provideBus() *bus.EventBus {
	return bus.Default
}
