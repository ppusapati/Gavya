package classregistry

import (
	"context"

	"connectrpc.com/connect"
	"go.uber.org/fx"

	classregistryv1 "p9e.in/samavaya/packages/classregistry/api/v1"
	"p9e.in/samavaya/packages/classregistry/api/v1/classregistryv1connect"
	"p9e.in/samavaya/packages/errors"
)

// Handler implements the ClassRegistryServiceHandler generated from
// classregistry.proto. It wraps a Registry and translates wire types
// to/from the Go types in types.go.
type Handler struct {
	reg Registry
}

// NewHandler constructs a ClassRegistryService handler over the given
// registry. Safe to share across goroutines — the underlying Registry
// is read-only after load.
func NewHandler(reg Registry) *Handler {
	return &Handler{reg: reg}
}

// Compile-time check the handler satisfies the generated interface.
var _ classregistryv1connect.ClassRegistryServiceHandler = (*Handler)(nil)

// ListClasses returns every class defined for a domain, sorted by
// Name. Empty response (no error) for a domain the registry doesn't
// know about — that's the "domain not yet consolidated" signal.
func (h *Handler) ListClasses(
	ctx context.Context,
	req *connect.Request[classregistryv1.ListClassesRequest],
) (*connect.Response[classregistryv1.ListClassesResponse], error) {
	domain := req.Msg.GetDomain()
	if domain == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.BadRequest("CLASSREGISTRY_DOMAIN_REQUIRED", "domain is required"))
	}
	classes := h.reg.ListClasses(domain)
	out := &classregistryv1.ListClassesResponse{
		Classes: make([]*classregistryv1.ClassSummary, 0, len(classes)),
	}
	for _, cd := range classes {
		out.Classes = append(out.Classes, &classregistryv1.ClassSummary{
			Name:         cd.Name,
			Label:        cd.Label,
			Description:  cd.Description,
			Industries:   append([]string(nil), cd.Industries...),
			HasProcesses: len(cd.Processes) > 0,
		})
	}
	return connect.NewResponse(out), nil
}

// GetClassSchema returns one class's resolved definition.
// Inheritance is already applied by the loader; the response reflects
// the merged view. Callers render forms from this response.
func (h *Handler) GetClassSchema(
	ctx context.Context,
	req *connect.Request[classregistryv1.GetClassSchemaRequest],
) (*connect.Response[classregistryv1.GetClassSchemaResponse], error) {
	domain := req.Msg.GetDomain()
	class := req.Msg.GetClass()
	if domain == "" || class == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.BadRequest("CLASSREGISTRY_DOMAIN_CLASS_REQUIRED", "domain and class are both required"))
	}
	cd, err := h.reg.GetClass(domain, class)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	return connect.NewResponse(&classregistryv1.GetClassSchemaResponse{
		Class: classDefToPB(cd),
	}), nil
}

// ListDomains returns the list of domains the registry holds YAML
// for, sorted. Admin UIs + tooling consume this to enumerate the
// registry.
func (h *Handler) ListDomains(
	ctx context.Context,
	_ *connect.Request[classregistryv1.ListDomainsRequest],
) (*connect.Response[classregistryv1.ListDomainsResponse], error) {
	return connect.NewResponse(&classregistryv1.ListDomainsResponse{
		Domains: h.reg.Domains(),
	}), nil
}

// ---------------------------------------------------------------------------
// HandlerModule — fx wiring
// ---------------------------------------------------------------------------

// HandlerModule provides the ClassRegistryService handler to the fx
// graph. Requires a Registry (provided by classregistry.Module).
// Composition roots that mount this module expose the service over
// their HTTP/Connect mux using the route below.
var HandlerModule = fx.Module("classregistry-handler",
	fx.Provide(NewHandler),
	fx.Provide(NewClassRegistryRoute),
)

// Route is the Connect-style handler registration returned by
// NewClassRegistryRoute. The composition root mounts it against
// the service's HTTP mux.
type Route struct {
	Path    string
	Handler func() interface{}
}

// NewClassRegistryRoute wraps the Handler as a Connect service
// registration. The function form lets callers defer the expensive
// interceptor wiring until mux assembly.
func NewClassRegistryRoute(h *Handler) Route {
	path, handlerFunc := classregistryv1connect.NewClassRegistryServiceHandler(h)
	return Route{
		Path:    path,
		Handler: func() interface{} { return handlerFunc },
	}
}
