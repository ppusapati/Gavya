//go:build e2e

// An endpoint must be able to satisfy what its service requires.
//
// cattle-market's PlaceBid and RecordSale had never worked — not for any caller,
// since they were written. Both services called currency.Normalise on a field
// the handler never populated, because neither request type had a currency at
// all, and currency.Normalise("") refuses an empty code. Every call to either
// endpoint came back with "a currency code is three letters, as in INR or JPY".
//
// Nothing noticed for the same reason nothing usually notices this: the two
// endpoints had no end-to-end coverage, and a unit test of the service passes a
// domain object it filled in itself. The gap is between the request type and the
// service's expectations, which is exactly the seam neither side's tests look at.
//
// So this test looks at that seam directly. For each handler method it finds the
// request type, the service method it calls, and what that method insists on;
// then it asks whether the request could supply it. It is not a substitute for
// end-to-end coverage — it cannot tell whether an endpoint does the right thing,
// only whether it is capable of doing anything at all — but it is cheap, it
// needs no database, and it catches a defect that shipped twice.
//
// Verified against the defect it was written for: run against the tree as it
// stood at e3aa028, it names PlaceBid and RecordSale.
package e2e

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// requirement is something a service method refuses to proceed without, named
// as the request field that would carry it.
type requirement struct {
	field string // snake_case, as it appears in a json tag
	why   string // the line in the service that demands it
}

// Handlers that take a value their service requires from the verified transport
// rather than from the request body.
//
// The check below reads what a request carries and what the service it calls
// insists on, and flags the difference. That is right almost everywhere: a
// service method needing a field no request has is an endpoint that fails the
// same way every time, which is what PlaceBid and RecordSale did.
//
// It is wrong where the value deliberately does not come from the body, and
// there is exactly one of those. Listed by name with the reason, rather than
// inferred, so that adding a second is a decision somebody writes down — the
// same arrangement as the handlers that pass their whole message on.
var fromVerifiedTransport = map[string]string{
	"identity-service.ChangePassword": "reads whose password from the verified actor, " +
		"never from the body. This route requires no permission, so a body naming " +
		"its own subject would let anybody signed in change anybody else's password " +
		"with the current one as the only obstacle.",
}

func TestEveryEndpointCanSatisfyItsService(t *testing.T) {
	skippedVerified := 0
	root := repoRoot(t)
	dirs, err := filepath.Glob(filepath.Join(root, "services", "*-service"))
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) == 0 {
		t.Fatal("no services found; this check would pass against an empty repository")
	}

	checked := 0
	for _, dir := range dirs {
		svc := filepath.Base(dir)
		handler := parseDir(t, filepath.Join(dir, "internal", "handler"))
		service := parseDir(t, filepath.Join(dir, "internal", "service"))
		if handler == nil || service == nil {
			continue
		}

		requests := requestFields(handler)
		demands := serviceRequirements(service)

		for _, ep := range endpoints(handler) {
			for _, called := range ep.calls {
				needs, ok := demands[called]
				if !ok {
					continue
				}
				if reason, exempt := fromVerifiedTransport[svc+"."+ep.name]; exempt {
					_ = reason
					skippedVerified++
					continue
				}
				checked++
				carries := requests[ep.request]
				for _, need := range needs {
					if _, found := carries[need.field]; found {
						continue
					}
					t.Errorf("%s: %s takes a %s, which has no %q field, and %s insists on one\n"+
						"    %s\n"+
						"Every call to this endpoint fails, and it fails the same way every "+
						"time — which is what PlaceBid and RecordSale did from the day they "+
						"were written until somebody tried them end to end.",
						svc, ep.name, ep.request, need.field, called, need.why)
				}
			}
		}
	}

	// A check that examined nothing would pass. This one says how much it looked
	// at, and fails if that is none.
	if checked == 0 {
		t.Fatal("no handler method was matched to a service method, so this check " +
			"examined nothing and would have passed against any defect")
	}
	t.Logf("checked %d handler-to-service calls; %d take a required value from the "+
		"verified transport", checked, skippedVerified)
}

// endpoint is one Connect handler method.
type endpoint struct {
	name    string   // the handler method
	request string   // the request type it takes
	calls   []string // the service methods it calls
}

func endpoints(files []*ast.File) []endpoint {
	var out []endpoint
	for _, f := range files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Body == nil {
				continue
			}
			if !receiverIs(fn, "Handler") {
				continue
			}
			req, ok := connectRequestType(fn)
			if !ok {
				continue
			}
			ep := endpoint{name: fn.Name.Name, request: req}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				// h.svc.Something(...)
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				inner, ok := sel.X.(*ast.SelectorExpr)
				if !ok || inner.Sel.Name != "svc" {
					return true
				}
				ep.calls = append(ep.calls, sel.Sel.Name)
				return true
			})
			if len(ep.calls) > 0 {
				out = append(out, ep)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// connectRequestType reports the T in *connect.Request[T], which is the shape a
// caller actually sends.
func connectRequestType(fn *ast.FuncDecl) (string, bool) {
	for _, p := range fn.Type.Params.List {
		star, ok := p.Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		idx, ok := star.X.(*ast.IndexExpr)
		if !ok {
			continue
		}
		sel, ok := idx.X.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Request" {
			continue
		}
		if id, ok := idx.Index.(*ast.Ident); ok {
			return id.Name, true
		}
	}
	return "", false
}

// requestFields maps each struct type to the json names its fields carry.
func requestFields(files []*ast.File) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, f := range files {
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				names := map[string]bool{}
				for _, field := range st.Fields.List {
					if field.Tag == nil {
						continue
					}
					tag, err := strconv.Unquote(field.Tag.Value)
					if err != nil {
						continue
					}
					name, _, _ := strings.Cut(reflect.StructTag(tag).Get("json"), ",")
					if name != "" && name != "-" {
						names[name] = true
					}
				}
				out[ts.Name.Name] = names
			}
		}
	}
	return out
}

// serviceRequirements maps each Service method to what it refuses to proceed
// without.
//
// Two shapes, both of which are how these services actually say it:
//
//	invalid("tenant_id is required")
//	invalid("id and tenant_id are required")
//
// plus a call to currency.Normalise, which refuses an empty code and so demands
// a currency whether or not the message says so. That last one is the shape that
// hid the bid defect: the service never said "currency is required", it just
// could not run without one.
func serviceRequirements(files []*ast.File) map[string][]requirement {
	out := map[string][]requirement{}
	for _, f := range files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Body == nil || !receiverIs(fn, "Service") {
				continue
			}
			var reqs []requirement
			seen := map[string]bool{}
			add := func(field, why string) {
				if field == "" || seen[field] {
					return
				}
				seen[field] = true
				reqs = append(reqs, requirement{field: field, why: why})
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch fun := call.Fun.(type) {
				case *ast.Ident:
					if fun.Name != "invalid" || len(call.Args) == 0 {
						return true
					}
					lit, ok := call.Args[0].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						return true
					}
					msg, err := strconv.Unquote(lit.Value)
					if err != nil {
						return true
					}
					for _, field := range fieldsRequiredBy(msg) {
						add(field, "invalid("+strconv.Quote(msg)+")")
					}
				case *ast.SelectorExpr:
					pkg, ok := fun.X.(*ast.Ident)
					if !ok {
						return true
					}
					if pkg.Name == "currency" && fun.Sel.Name == "Normalise" {
						add("currency", "currency.Normalise refuses an empty code")
					}
				}
				return true
			})
			if len(reqs) > 0 {
				out[fn.Name.Name] = reqs
			}
		}
	}
	return out
}

// fieldsRequiredBy pulls the field names out of a "... is required" message.
func fieldsRequiredBy(msg string) []string {
	before, after, found := strings.Cut(msg, " is required")
	if found && after == "" {
		if isFieldName(before) {
			return []string{before}
		}
		return nil
	}
	before, after, found = strings.Cut(msg, " are required")
	if !found || after != "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(before, " and ") {
		part = strings.TrimSpace(part)
		if isFieldName(part) {
			out = append(out, part)
		}
	}
	return out
}

// isFieldName keeps the match to things that look like a json name, so a
// sentence such as "a currency is required" does not become a field called
// "a currency".
func isFieldName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && r != '_' {
			return false
		}
	}
	return true
}

func receiverIs(fn *ast.FuncDecl, typeName string) bool {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return false
	}
	t := fn.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	id, ok := t.(*ast.Ident)
	return ok && id.Name == typeName
}

// parseDir reads one package's non-test files, or nil if the directory is not
// there. A service without a handler or service package is not a failure; it is
// a service shaped differently.
func parseDir(t *testing.T, dir string) []*ast.File {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		// A directory that is not there is not an error worth failing on; one
		// that will not parse is.
		if strings.Contains(err.Error(), "no such file or directory") {
			return nil
		}
		t.Fatalf("parse %s: %v", dir, err)
	}
	var files []*ast.File
	for _, pkg := range pkgs {
		names := make([]string, 0, len(pkg.Files))
		for name := range pkg.Files {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			files = append(files, pkg.Files[name])
		}
	}
	if len(files) == 0 {
		return nil
	}
	return files
}

// A field a caller supplies must reach something.
//
// cattle-service's DeleteCattle took a `deleted_by` on the wire, and the handler
// dropped it. The repository took no actor at all, so the soft delete stamped
// `updated_at` to the moment of deletion and left `updated_by` holding whoever
// had last edited the row. Those two fields are read as a pair. The record did
// not merely omit who deleted the animal — it named somebody who had not done
// it, and the caller who supplied the right name had every reason to think it
// had been kept.
//
// That was found by hand, by reading every request type against its handler.
// A check done once is a check that has already stopped working, so this is it
// as a test.
//
// It is deliberately quiet about one case. A handler that passes the whole
// message on — `printOptions(*m)`, `h.svc.X(ctx, m)` — puts its fields somewhere
// this cannot follow, so those are skipped rather than guessed at. Three
// handlers do that today and all three are fine; flagging them would have made
// the check something to be ignored, which is worse than not having it.
func TestNoHandlerDiscardsAFieldItsCallerSupplied(t *testing.T) {
	root := repoRoot(t)
	dirs, err := filepath.Glob(filepath.Join(root, "services", "*-service"))
	if err != nil {
		t.Fatal(err)
	}

	checked, skipped := 0, 0
	for _, dir := range dirs {
		svc := filepath.Base(dir)
		handler := parseDir(t, filepath.Join(dir, "internal", "handler"))
		if handler == nil {
			continue
		}
		declared := declaredFields(handler)

		for _, f := range handler {
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv == nil || fn.Body == nil || !receiverIs(fn, "Handler") {
					continue
				}
				rtype, ok := connectRequestType(fn)
				if !ok {
					continue
				}
				fields, ok := declared[rtype]
				if !ok || len(fields) == 0 {
					continue
				}
				if passesWholeMessage(fn) {
					skipped++
					continue
				}
				checked++
				read := fieldsRead(fn)
				for _, field := range fields {
					if read[field.goName] {
						continue
					}
					t.Errorf("%s: %s declares %q on the wire and never reads it\n"+
						"A caller filling that in has every reason to believe it was kept. "+
						"cattle-service's DeleteCattle dropped its `deleted_by` exactly this "+
						"way, and the record then named the wrong person — not silently "+
						"blank, but confidently wrong.",
						svc, rtype, field.jsonName)
				}
			}
		}
	}

	if checked == 0 {
		t.Fatal("no handler was examined, so this check would have passed against any defect")
	}
	t.Logf("checked %d handlers; skipped %d that pass the whole message on", checked, skipped)
}

type wireField struct{ goName, jsonName string }

// declaredFields lists each struct's fields that carry a json name, keeping the
// Go name so the handler body can be searched for it.
func declaredFields(files []*ast.File) map[string][]wireField {
	out := map[string][]wireField{}
	for _, f := range files {
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				var fields []wireField
				for _, field := range st.Fields.List {
					if field.Tag == nil || len(field.Names) == 0 {
						continue
					}
					tag, err := strconv.Unquote(field.Tag.Value)
					if err != nil {
						continue
					}
					name, _, _ := strings.Cut(reflect.StructTag(tag).Get("json"), ",")
					if name == "" || name == "-" {
						continue
					}
					fields = append(fields, wireField{field.Names[0].Name, name})
				}
				out[ts.Name.Name] = fields
			}
		}
	}
	return out
}

// fieldsRead reports every selector the body reaches for, by Go field name.
//
// It does not check what the selector was on: a handler that reads `m.TenantID`
// and one that reads `other.TenantID` both count. That is deliberate — the
// question is whether the name appears at all, and being wrong in the generous
// direction keeps this quiet rather than noisy.
func fieldsRead(fn *ast.FuncDecl) map[string]bool {
	read := map[string]bool{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			read[sel.Sel.Name] = true
		}
		return true
	})
	return read
}

// passesWholeMessage reports whether the body hands the request on entire, in
// which case where its fields go is beyond what this test can follow.
func passesWholeMessage(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		for _, arg := range call.Args {
			if star, ok := arg.(*ast.StarExpr); ok {
				arg = star.X
			}
			switch a := arg.(type) {
			case *ast.Ident:
				// m := req.Msg, then f(m)
				if a.Name == "m" {
					found = true
				}
			case *ast.SelectorExpr:
				// f(req.Msg)
				if a.Sel.Name == "Msg" {
					found = true
				}
			}
		}
		return true
	})
	return found
}
