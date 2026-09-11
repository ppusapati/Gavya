//go:build e2e

// Every field a service puts on the wire is named the same way.
//
// Most of this platform speaks snake_case — `tenant_id`, `contact_email`,
// `currency_scale` — because the handlers declare `json:"..."` on every field.
// Eleven services did not: they returned their domain types directly, and those
// carry no tags at all, so the API emitted Go field names. `tenant-service`
// answered with `{"ID":...,"ContactEmail":...,"CurrencyScale":2}`.
//
// That is not a style difference. Go's JSON decoder ignores case but not
// underscores, so a client written in the platform's own convention matches
// `id` against `ID` and reads it, and matches `currency_scale` against
// `CurrencyScale` and reads nothing. The field comes back as the zero value with
// no error anywhere — a tenant recording rupees looks like a tenant recording a
// currency with no minor unit, and the paise are dropped by the client rather
// than by the platform.
//
// It is the same shape as the `deleted_by` a handler discarded: a value the
// caller has every reason to think it received. This is that, one layer out, and
// it was found by a decoding trap in a test of this repository's own — a client
// struct that read `""` where it expected a calf's sex.
//
// So the format is pinned here rather than left to whatever a Go struct happens
// to be called. Every exported field reachable from a Connect response must
// carry a json name, and that name must be lower_snake_case.
package e2e

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestEveryWireFieldIsNamedInSnakeCase(t *testing.T) {
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
		domainPkg := parseDir(t, filepath.Join(dir, "internal", "domain"))
		if handler == nil {
			continue
		}

		// Every struct this service can reach from a response, by name.
		structs := map[string]*ast.StructType{}
		for _, files := range [][]*ast.File{handler, domainPkg} {
			for _, f := range files {
				for name, st := range structTypes(f) {
					if _, taken := structs[name]; !taken {
						structs[name] = st
					}
				}
			}
		}

		for _, name := range responseTypes(handler) {
			for _, bad := range untagged(name, structs, map[string]bool{}) {
				checked++
				t.Errorf("%s: %s is reachable from a Connect response and its field %s "+
					"is named %q on the wire\n"+
					"Every other service here answers in lower_snake_case. A client "+
					"written that way matches a single-word name by case-insensitive "+
					"comparison and reads it, and matches a multi-word one against "+
					"nothing — so the field arrives as its zero value with no error. "+
					"currency_scale coming back as 0 turns a rupee tenant into one with "+
					"no minor unit.",
					svc, bad.owner, bad.field, bad.wire)
			}
		}
	}

	// The check reports how much it looked at, because one that reached nothing
	// would pass.
	if total := len(dirs); total == 0 {
		t.Fatal("nothing examined")
	}
	t.Logf("examined the response types of %d services; %d fields are misnamed", len(dirs), checked)
}

type badField struct{ owner, field, wire string }

// untagged walks a type and everything it contains, reporting fields whose wire
// name is not lower_snake_case — including the ones that have no tag at all and
// so are emitted under their Go name.
func untagged(name string, structs map[string]*ast.StructType, seen map[string]bool) []badField {
	if seen[name] {
		return nil
	}
	seen[name] = true
	st, ok := structs[name]
	if !ok {
		return nil
	}
	var out []badField
	for _, field := range st.Fields.List {
		if len(field.Names) == 0 || !field.Names[0].IsExported() {
			continue
		}
		goName := field.Names[0].Name
		wire := goName
		if field.Tag != nil {
			if tag, err := strconv.Unquote(field.Tag.Value); err == nil {
				if n, _, _ := strings.Cut(reflect.StructTag(tag).Get("json"), ","); n != "" {
					if n == "-" {
						continue
					}
					wire = n
				}
			}
		}
		if !isSnakeCase(wire) {
			out = append(out, badField{owner: name, field: goName, wire: wire})
		}
		// Follow the field's type, so a response holding a domain type is
		// checked through to that type's own fields.
		if inner := namedType(field.Type); inner != "" {
			out = append(out, untagged(inner, structs, seen)...)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].field < out[j].field })
	return out
}

// isSnakeCase accepts lower-case words joined by underscores, and digits. It is
// deliberately strict: "TenantID", "tenantId" and "Tenant_ID" are all things
// somebody would write and none of them is what the rest of the platform emits.
func isSnakeCase(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

// namedType reduces *T, []T, []*T and map[K]T to T, or "" for anything else.
func namedType(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return namedType(t.X)
	case *ast.ArrayType:
		return namedType(t.Elt)
	case *ast.MapType:
		return namedType(t.Value)
	case *ast.SelectorExpr:
		// domain.Tenant and the like: the name in the other package.
		return t.Sel.Name
	}
	return ""
}

// responseTypes reports the R in *connect.Response[R] for every handler method.
func responseTypes(files []*ast.File) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Type.Results == nil || !receiverIs(fn, "Handler") {
				continue
			}
			for _, r := range fn.Type.Results.List {
				star, ok := r.Type.(*ast.StarExpr)
				if !ok {
					continue
				}
				idx, ok := star.X.(*ast.IndexExpr)
				if !ok {
					continue
				}
				sel, ok := idx.X.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Response" {
					continue
				}
				if id, ok := idx.Index.(*ast.Ident); ok && !seen[id.Name] {
					seen[id.Name] = true
					out = append(out, id.Name)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

func structTypes(f *ast.File) map[string]*ast.StructType {
	out := map[string]*ast.StructType{}
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
			if st, ok := ts.Type.(*ast.StructType); ok {
				out[ts.Name.Name] = st
			}
		}
	}
	return out
}
