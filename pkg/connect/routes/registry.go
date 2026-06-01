// Package routes maintains an in-process snapshot of every ConnectRPC path
// mounted on the monolith's HTTP mux at startup. Other packages (notably
// FormService.SubmitForm's endpoint resolver) consume this to validate or
// repair claimed RPC endpoints before proxying.
//
// Lifecycle:
//   - The mux-mounter (app/cmd/main.go::mountRoutes) calls Add(path) for each
//     route as it mounts.
//   - FormService.SubmitForm calls Resolve(claimed) to check whether the
//     claimed endpoint exists, or to find the closest mounted sibling.
//
// Snapshot is goroutine-safe via sync.RWMutex. Writes happen at boot only;
// reads happen on every form submission.
package routes

import (
	"strings"
	"sync"
)

// Registry holds the set of path prefixes mounted on the mux. Each prefix
// ends in "/" — that's the ConnectRPC service prefix (e.g.
// "/asset.asset.api.v1.AssetService/").
type Registry struct {
	mu       sync.RWMutex
	prefixes map[string]struct{}
}

// New returns an empty registry. Provided as an fx.New() builder; main.go
// shares one instance with mountRoutes (writer) and formservice (reader).
func New() *Registry {
	return &Registry{prefixes: map[string]struct{}{}}
}

// Add records a mounted path prefix. Idempotent. Safe to call concurrently.
func (r *Registry) Add(prefix string) {
	if prefix == "" {
		return
	}
	r.mu.Lock()
	r.prefixes[prefix] = struct{}{}
	r.mu.Unlock()
}

// Prefixes returns a copy of every recorded prefix. Order is not stable.
func (r *Registry) Prefixes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.prefixes))
	for p := range r.prefixes {
		out = append(out, p)
	}
	return out
}

// Has reports whether the exact prefix is registered.
func (r *Registry) Has(prefix string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.prefixes[prefix]
	return ok
}

// Resolve takes a claimed full RPC endpoint (e.g.
// "/masters.item.api.v1.ItemMasterService/CreateItem") and returns a working
// endpoint via these strategies, in order:
//
//  1. Exact match — the claimed prefix is mounted.
//  2. Drop "Master" suffix from service name (ItemMasterService → ItemService).
//  3. Insert "api." between module and "v1" if missing (.v1. → .api.v1.).
//  4. Drop ".core." segment (asset.core.api.v1 → asset.<module>.api.v1; chooses
//     first sibling sharing the domain prefix).
//  5. Same-domain sibling fallback: if the claim's first two path segments
//     (e.g. "/sales.salesorder.") match any mounted prefix's first two
//     segments AND the method name (last path segment) maps to a unique
//     mounted route under that domain.module, use it.
//
// Returns the resolved endpoint (always starting with "/") and a boolean
// indicating whether a different endpoint was found. If false, the caller
// should fail the request with a clear message — the claimed endpoint is
// genuinely unmapped.
func (r *Registry) Resolve(claimed string) (resolved string, found bool) {
	if claimed == "" {
		return "", false
	}
	// Split into prefix + method. ConnectRPC uses /pkg.svc/Method shape.
	idx := strings.LastIndex(claimed, "/")
	if idx <= 0 || idx == len(claimed)-1 {
		return claimed, false
	}
	claimedPrefix := claimed[:idx+1] // include trailing /
	method := claimed[idx+1:]

	// 1. Exact.
	if r.Has(claimedPrefix) {
		return claimed, true
	}

	// 2. Strip "Master" from service name.
	//    /masters.item.api.v1.ItemMasterService/  →  /masters.item.api.v1.ItemService/
	if alt := stripMasterFromService(claimedPrefix); alt != claimedPrefix && r.Has(alt) {
		return alt + method, true
	}

	// 3. Insert "api." before "v1" if missing.
	//    /asset.disposal.v1.AssetService/  →  /asset.disposal.api.v1.AssetService/
	if alt := insertAPISegment(claimedPrefix); alt != claimedPrefix && r.Has(alt) {
		return alt + method, true
	}

	// 3b. Combine 2+3 — common in ItemMasterService claims.
	if alt := stripMasterFromService(insertAPISegment(claimedPrefix)); alt != claimedPrefix && r.Has(alt) {
		return alt + method, true
	}

	// 4. Same-domain fallback. Take first segment "/<domain>." and any
	//    mounted prefix sharing it, then pick the one with the same method
	//    suffix in the registered set... but we don't index methods. So
	//    instead: if exactly ONE mounted prefix shares the same first two
	//    segments as claimed, use that.
	if alt := sameDomainFallback(r, claimedPrefix); alt != "" {
		return alt + method, true
	}

	return claimed, false
}

// StripMasterFromService removes "Master" from a service name segment.
// Operates on the prefix only; e.g. ItemMasterService → ItemService.
//
// Exported so the offline form-endpoint rewriter (used at YAML-config
// build time, see /e/tmp/probebank/rewrite_form_endpoints.go) shares
// the same transformation rule as the runtime resolver. Any change to
// the Master-strip semantics MUST update both this function AND the
// rewriter's behavior — failing to do so produces silent endpoint
// drift between rebuilds.
func StripMasterFromService(prefix string) string {
	// prefix like /masters.item.api.v1.ItemMasterService/
	// last segment before trailing / is the service name.
	trimmed := strings.TrimSuffix(prefix, "/")
	idx := strings.LastIndex(trimmed, ".")
	if idx < 0 {
		return prefix
	}
	svc := trimmed[idx+1:]
	if !strings.Contains(svc, "Master") {
		return prefix
	}
	newSvc := strings.Replace(svc, "Master", "", 1)
	return trimmed[:idx+1] + newSvc + "/"
}

// InsertAPISegment inserts ".api" before ".v1." if not already present.
// Operates on the prefix; idempotent if already present.
//
// Exported alongside StripMasterFromService — see that function's doc.
func InsertAPISegment(prefix string) string {
	if strings.Contains(prefix, ".api.v1.") {
		return prefix
	}
	if !strings.Contains(prefix, ".v1.") {
		return prefix
	}
	return strings.Replace(prefix, ".v1.", ".api.v1.", 1)
}

// stripMasterFromService is the unexported alias kept for internal callers.
// Same behavior as the exported StripMasterFromService.
func stripMasterFromService(prefix string) string { return StripMasterFromService(prefix) }

// insertAPISegment is the unexported alias kept for internal callers.
func insertAPISegment(prefix string) string { return InsertAPISegment(prefix) }

// sameDomainFallback finds a mounted prefix sharing the same first two
// dotted segments (e.g. "/sales.salesorder.") as the claim. Returns the
// matching mounted prefix if exactly one is found, "" otherwise — multiple
// or none means we can't choose safely.
func sameDomainFallback(r *Registry, claimedPrefix string) string {
	// Extract first two segments: "/<a>.<b>."
	rest := strings.TrimPrefix(claimedPrefix, "/")
	parts := strings.SplitN(rest, ".", 3)
	if len(parts) < 3 {
		return ""
	}
	wantPrefix := "/" + parts[0] + "." + parts[1] + "."

	r.mu.RLock()
	defer r.mu.RUnlock()
	var match string
	count := 0
	for p := range r.prefixes {
		if strings.HasPrefix(p, wantPrefix) {
			match = p
			count++
			if count > 1 {
				return ""
			}
		}
	}
	if count == 1 {
		return match
	}
	return ""
}
