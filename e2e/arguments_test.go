//go:build e2e

// No call hands one parameter the value meant for another.
//
// cattle-market's GetOwnershipHistory had never returned a row. The handler
// passed (TenantID, CattleID) to a service declaring (cattleID, tenantID),
// which passed (cattleID, tenantID) to a repository method declaring
// (tenantID, cattleID). Two transpositions, and they do not cancel: the query
// looked for a tenant whose id was an animal's. It matched nothing for any
// input, and the endpoint answered 200 with an empty history — an animal sold
// last week reading as one that has never changed hands.
//
// Nothing could see it. Both values are strings and both are ULIDs, so the
// compiler is content. The service's own guard, `cattleID == "" || tenantID ==
// ""`, fires on a missing field whichever way round the two arrive, so the
// validation test passes. The repository integration test passes too: it calls
// the query directly with semantically correct values, exercising the one layer
// that was right.
//
// What the names do here is what the types cannot. A call that writes
// `req.Msg.TenantID` into a parameter called `cattleID` while writing
// `req.Msg.CattleID` into one called `tenantID` has said plainly, twice, which
// value it thinks it is passing. This reads those names back.
//
// It only reports a crossing: an argument whose name matches a *different*
// parameter of the same type. An argument whose name matches nothing is not a
// finding — plenty of call sites name things differently for good reason, and a
// check that complained about those would be turned off within a week.
package e2e

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestNoCallCrossesTwoArguments(t *testing.T) {
	root := repoRoot(t)
	dirs, err := filepath.Glob(filepath.Join(root, "services", "*-service"))
	if err != nil {
		t.Fatal(err)
	}
	dirs = append(dirs, filepath.Join(root, "libs", "integrity"))
	if len(dirs) == 0 {
		t.Fatal("no services found; this check would pass against an empty repository")
	}

	callsChecked, argsChecked := 0, 0
	for _, dir := range dirs {
		files := parseTree(t, dir)
		if len(files) == 0 {
			continue
		}

		// Every function this tree declares, by name — all of them, because a
		// name here is usually declared more than once on purpose. The handler,
		// the service and the repository each have a GetOwnershipHistory, and
		// that layering is the point: the calls worth checking are the ones
		// between those layers. Dropping a repeated name would have dropped the
		// bug this test exists for.
		//
		// Which declaration a call resolves to needs type information this check
		// does not have, so it holds every candidate and, below, reports only a
		// crossing that holds under all of them. A finding is then true however
		// the call resolves.
		decls := map[string][]*ast.FuncDecl{}
		for _, f := range files {
			for _, d := range f.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Type.Params == nil {
					continue
				}
				decls[fn.Name.Name] = append(decls[fn.Name.Name], fn)
			}
		}

		for _, f := range files {
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || call.Ellipsis != token.NoPos {
					return true
				}
				name := calleeName(call)
				if name == "" {
					return true
				}
				// Only declarations that could actually take this call. A
				// different arity is a different function that happens to share
				// a name, and says nothing about this one.
				var candidates [][]param
				for _, fn := range decls[name] {
					if p := flatParams(fn); len(p) > 0 && len(p) == len(call.Args) {
						candidates = append(candidates, p)
					}
				}
				if len(candidates) == 0 {
					return true
				}
				callsChecked++
				argsChecked += len(call.Args)
				for i, j := range transpositions(call.Args, candidates) {
					params := candidates[0]
					t.Errorf("%s: %s hands %s and %s to each other's parameters\n"+
						"  call: %s\n"+
						"  declared: %s(%s)\n"+
						"  %s is passed where %s goes, and %s where %s goes.\n"+
						"Both are %s, so the compiler cannot tell them apart, and a guard "+
						"refusing an empty one refuses it whichever way round the two arrive. "+
						"cattle-market's ownership history was written this way: it matched no "+
						"row for any input and still answered 200.",
						filepath.Base(dir), positionOf(call.Args[i]),
						render(call.Args[i]), render(call.Args[j]),
						render(call), name, paramList(params),
						render(call.Args[i]), params[i].name,
						render(call.Args[j]), params[j].name,
						params[i].typ)
				}
				return true
			})
		}
	}

	// A check that reached nothing passes. This one says what it reached.
	if callsChecked == 0 {
		t.Fatal("no call site was examined; the check proves nothing")
	}
	t.Logf("read %d arguments across %d calls into functions this repository declares",
		argsChecked, callsChecked)
}

type param struct{ name, typ string }

// transpositions finds pairs of arguments handed to each other's parameters:
// argument i named for parameter j, and argument j named for parameter i, both
// the same type. It reports i→j once per pair.
//
// Requiring the crossing to hold in *both* directions is what keeps this check
// worth leaving on. A single argument whose name matches some other parameter
// is common and usually innocent: observation-service calls
// SupersedeObservation(ctx, in.TenantID, in.Corrects, id) against
// (tenantID, id, supersededBy), where the local `id` is the new observation and
// `in.Corrects` the one it replaces. That is correct, and a one-directional
// rule calls it a bug — a generic name like `id` will collide with something
// eventually. It will not collide reciprocally. A pair that crosses both ways
// has said which value it is, twice, and been wrong twice.
//
// A pair is reported only if it crosses under every candidate declaration,
// since which one the call resolves to is not known here.
func transpositions(args []ast.Expr, candidates [][]param) map[int]int {
	out := map[int]int{}
	claimed := map[int]bool{}
	for i := range args {
		if claimed[i] {
			continue
		}
		for j := i + 1; j < len(args); j++ {
			if claimed[j] || !crossesInAll(args, i, j, candidates) {
				continue
			}
			out[i] = j
			claimed[i], claimed[j] = true, true
			break
		}
	}
	return out
}

func crossesInAll(args []ast.Expr, i, j int, candidates [][]param) bool {
	a, b := leafName(args[i]), leafName(args[j])
	if a == "" || b == "" {
		return false
	}
	for _, params := range candidates {
		if params[i].typ != params[j].typ {
			return false
		}
		// Named for each other's position, and not for their own.
		if !same(a, params[j].name) || !same(b, params[i].name) {
			return false
		}
		if same(a, params[i].name) || same(b, params[j].name) {
			return false
		}
	}
	return true
}

// flatParams expands `a, b string` into two parameters, so positions line up
// with the argument list.
func flatParams(fn *ast.FuncDecl) []param {
	var out []param
	for _, field := range fn.Type.Params.List {
		typ := render(field.Type)
		if len(field.Names) == 0 {
			out = append(out, param{typ: typ})
			continue
		}
		for _, n := range field.Names {
			out = append(out, param{name: n.Name, typ: typ})
		}
	}
	return out
}

// leafName is the last name in an argument expression: `req.Msg.TenantID` is
// TenantID, `tenantID` is tenantID, `o.CattleID` is CattleID. Anything that is
// not a plain name or selector — a literal, a call, an operator — has no name to
// read and is skipped.
func leafName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	}
	return ""
}

// same compares names the way a reader does, ignoring case and underscores:
// tenantID, TenantID and tenant_id are one name.
func same(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.EqualFold(strings.ReplaceAll(a, "_", ""), strings.ReplaceAll(b, "_", ""))
}

// calleeName is the function's own name, whether it is called plainly or
// through a receiver: both `validate(x)` and `h.svc.Validate(x)` give Validate.
func calleeName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		return fn.Sel.Name
	}
	return ""
}

func paramList(params []param) string {
	parts := make([]string, 0, len(params))
	for _, p := range params {
		parts = append(parts, strings.TrimSpace(p.name+" "+p.typ))
	}
	return strings.Join(parts, ", ")
}

func render(e ast.Node) string {
	var b strings.Builder
	if err := printer.Fprint(&b, token.NewFileSet(), e); err != nil {
		return "?"
	}
	s := b.String()
	if len(s) > 120 {
		s = s[:117] + "..."
	}
	return s
}

// Every tree is parsed into one shared FileSet, so a finding can name the file
// and line it sits on however deep in the repository it came from.
var treeFset = token.NewFileSet()

func positionOf(n ast.Node) string {
	p := treeFset.Position(n.Pos())
	if !p.IsValid() {
		return "?"
	}
	return fmt.Sprintf("%s:%d", filepath.Base(p.Filename), p.Line)
}

// parseTree reads every non-test Go file under one service, so a call in the
// handler is checked against a declaration in the service or repository
// package. Test files are left out: a test may well pass a tenant id as a
// cattle id on purpose, to see what happens.
func parseTree(t *testing.T, root string) []*ast.File {
	t.Helper()
	var paths []string
	err := filepath.Walk(root, func(path string, info fs.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(paths)
	var files []*ast.File
	for _, p := range paths {
		f, err := parser.ParseFile(treeFset, p, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		files = append(files, f)
	}
	return files
}
