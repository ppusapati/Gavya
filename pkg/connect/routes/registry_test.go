package routes

import "testing"

func TestResolveExactMatch(t *testing.T) {
	r := New()
	r.Add("/asset.asset.api.v1.AssetService/")
	got, ok := r.Resolve("/asset.asset.api.v1.AssetService/CreateCategory")
	if !ok {
		t.Fatalf("expected ok, got !ok")
	}
	if want := "/asset.asset.api.v1.AssetService/CreateCategory"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveStripMaster(t *testing.T) {
	r := New()
	r.Add("/masters.item.api.v1.ItemService/")
	got, ok := r.Resolve("/masters.item.api.v1.ItemMasterService/CreateItem")
	if !ok {
		t.Fatalf("expected ok via Master-strip")
	}
	if want := "/masters.item.api.v1.ItemService/CreateItem"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveInsertAPI(t *testing.T) {
	r := New()
	r.Add("/asset.disposal.api.v1.AssetService/")
	got, ok := r.Resolve("/asset.disposal.v1.AssetService/DisposeAsset")
	if !ok {
		t.Fatalf("expected ok via api-segment insert")
	}
	if want := "/asset.disposal.api.v1.AssetService/DisposeAsset"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveStripMasterAndInsertAPI(t *testing.T) {
	r := New()
	r.Add("/masters.item.api.v1.ItemService/")
	// Combined: missing "api" AND has "Master" suffix.
	got, ok := r.Resolve("/masters.item.v1.ItemMasterService/CreateItem")
	if !ok {
		t.Fatalf("expected ok via combined transform")
	}
	if want := "/masters.item.api.v1.ItemService/CreateItem"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveSameDomainFallback(t *testing.T) {
	r := New()
	r.Add("/sales.salesorder.api.v1.SalesOrderService/")
	// Claim invents "SalesOrderManagementService" — same domain prefix as the
	// only mounted route under /sales.salesorder. → fallback should pick it.
	got, ok := r.Resolve("/sales.salesorder.v1.SalesOrderManagementService/CreateOrder")
	if !ok {
		t.Fatalf("expected ok via same-domain fallback")
	}
	if want := "/sales.salesorder.api.v1.SalesOrderService/CreateOrder"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveAmbiguousFallbackReturnsNotFound(t *testing.T) {
	r := New()
	r.Add("/sales.salesorder.api.v1.SalesOrderService/")
	r.Add("/sales.salesorder.api.v1.SalesOrderLineService/")
	// Two mounted prefixes share /sales.salesorder. → ambiguous → no resolve.
	_, ok := r.Resolve("/sales.salesorder.v1.SomethingElseService/Method")
	if ok {
		t.Errorf("expected not ok (ambiguous), got ok")
	}
}

func TestResolveUnmappedReturnsClaim(t *testing.T) {
	r := New()
	r.Add("/asset.asset.api.v1.AssetService/")
	got, ok := r.Resolve("/totally.unrelated.v1.NopeService/DoThing")
	if ok {
		t.Errorf("expected not ok, got ok")
	}
	if got != "/totally.unrelated.v1.NopeService/DoThing" {
		t.Errorf("expected unchanged claim on miss, got %q", got)
	}
}

func TestResolveEmptyAndMalformed(t *testing.T) {
	r := New()
	if _, ok := r.Resolve(""); ok {
		t.Errorf("expected !ok for empty")
	}
	if _, ok := r.Resolve("noSlashes"); ok {
		t.Errorf("expected !ok for no slashes")
	}
	if _, ok := r.Resolve("/trailing/"); ok {
		t.Errorf("expected !ok for trailing slash with no method")
	}
}

func TestStripMasterPureFunction(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/masters.item.api.v1.ItemMasterService/", "/masters.item.api.v1.ItemService/"},
		{"/masters.item.api.v1.ItemService/", "/masters.item.api.v1.ItemService/"}, // no Master
		{"/", "/"},                              // no dot
		{"/no.service.dots/", "/no.service.dots/"}, // no Master
	}
	for _, c := range cases {
		if got := stripMasterFromService(c.in); got != c.want {
			t.Errorf("stripMaster(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestInsertAPIPureFunction(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/asset.disposal.v1.AssetService/", "/asset.disposal.api.v1.AssetService/"},
		{"/asset.disposal.api.v1.AssetService/", "/asset.disposal.api.v1.AssetService/"}, // already has api
		{"/no_v1_in_dots/", "/no_v1_in_dots/"},                                            // no .v1. token
	}
	for _, c := range cases {
		if got := insertAPISegment(c.in); got != c.want {
			t.Errorf("insertAPI(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestResolveConcurrentReadsAndWrites stresses the registry with concurrent
// Add (writers) and Resolve (readers) — production runs Resolve on every
// SubmitForm while a future feature might call Add at runtime (e.g. dynamic
// route mounting on tenant provisioning). The sync.RWMutex must keep both
// safe.
//
// Run with `go test -race ./packages/connect/routes/` to detect races.
func TestResolveConcurrentReadsAndWrites(t *testing.T) {
	r := New()
	for i := 0; i < 50; i++ {
		r.Add(buildPrefix(i))
	}

	const writers = 4
	const readers = 16
	const ops = 1000

	done := make(chan struct{})
	for w := 0; w < writers; w++ {
		go func(seed int) {
			for i := 0; i < ops; i++ {
				r.Add(buildPrefix(seed*ops + i + 1000))
			}
			done <- struct{}{}
		}(w)
	}
	for read := 0; read < readers; read++ {
		go func(seed int) {
			for i := 0; i < ops; i++ {
				_, _ = r.Resolve(buildPrefix(seed*ops+i) + "DoSomething")
			}
			done <- struct{}{}
		}(read)
	}
	for i := 0; i < writers+readers; i++ {
		<-done
	}
}

func buildPrefix(i int) string {
	// Deterministic distinct prefixes the resolver can match exactly.
	return "/" + asciiDomain(i) + ".asset.api.v1.AssetService/"
}

func asciiDomain(i int) string {
	// Small alphabet so multiple iterations collide on the same prefix —
	// exercises the read-after-write pattern.
	letters := "abcdefghij"
	return string(letters[i%len(letters)])
}

// BenchmarkResolveExactMatch measures the hot-path: a claim that matches
// a mounted prefix verbatim. With 118 prefixes (current production count)
// this is a single map lookup and should be ~tens of ns/op.
func BenchmarkResolveExactMatch(b *testing.B) {
	r := New()
	for i := 0; i < 118; i++ {
		r.Add(buildPrefix(i))
	}
	target := buildPrefix(50) + "DoSomething"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = r.Resolve(target)
	}
}

// BenchmarkResolveSameDomainFallback measures the slow-path: claim doesn't
// match exactly, doesn't match any of the 4 transform strategies, and falls
// through to same-domain scan — O(n) over the prefix set.
func BenchmarkResolveSameDomainFallback(b *testing.B) {
	r := New()
	for i := 0; i < 118; i++ {
		r.Add(buildPrefix(i))
	}
	// Claim with same first-two segments as exactly one mounted prefix
	// (asciiDomain returns 'a' for i%10==0; pick an i so unique match).
	target := "/a.asset.v1.SomethingElseService/CreateThing"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = r.Resolve(target)
	}
}
