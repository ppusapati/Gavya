package serve

import "net/http"

// Registrar is a service's handler: the thing that knows its own routes.
//
// It exists so a composition root can return one type whichever service it
// builds, which is what lets the modulith hold a list of twenty-eight build
// functions rather than twenty-eight lines of near-identical code. Go will not
// convert a func returning *handler.Handler into one returning an interface, so
// the interface has to be what they return.
type Registrar interface {
	Register(mux *http.ServeMux)
}
