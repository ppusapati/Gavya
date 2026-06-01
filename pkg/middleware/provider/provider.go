package provider

import (
	"p9e.in/samavaya/packages/middleware/dbmiddleware"
)

// Middleware constructors for dependency injection.
// These can be used directly or with DI frameworks like Uber FX.
var Constructors = []interface{}{
	dbmiddleware.NewDBResolver,
}
