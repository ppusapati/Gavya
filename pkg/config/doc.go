// Package config is the multi-source configuration loader used by
// ServiceDeps and every service's bootstrap path.
//
// Supports:
//   - File loading (TOML / YAML / JSON) via koanf under the hood
//   - Environment-variable overlay (env vars override file values)
//   - Structured Scan into user-defined Go structs
//   - Observer callbacks for dynamic config-change events
//
// The canonical flow is:
//
//	var cfg AppConfig
//	err := config.LoadFile("config.toml", &cfg)   // parse file
//	err  = config.LoadEnv(&cfg)                   // overlay env vars
//	err  = config.Validate(&cfg)                  // app-level validation
//
// A Config implementation backs the loader; callers typically construct
// one via config.New(opts...) and register Observers for hot-reload paths.
// Observer is the callback type invoked when a watched key changes.
package config
