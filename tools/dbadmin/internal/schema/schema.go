// Package schema is the order the platform's SQL is applied in.
//
// It exists so there is one answer to "what runs, and in what order". There were
// two: deploy/postgres-init, which runs once when a PostgreSQL data directory is
// first created and never again, and nothing at all for a database that already
// exists. A schema change therefore reached production by somebody opening psql,
// which is not a procedure, and the ordering constraints below — several of
// which are not obvious and were each found the hard way — lived only in that
// one script's comments.
//
// The order is load-bearing at four points, and each is a defect somebody met:
//
//   - Isolation is applied after the schemas, because it sweeps the tables that
//     exist.
//   - Foreign keys are made tenant-safe after isolation, because row-level
//     security does not reach a foreign key check: it runs as the system, so a
//     single-column key lets one tenant reference another's row and reports
//     whether it exists.
//   - The audit trail is made append-only after the grants, because it revokes
//     some of them back. The application may add to the trail and may not edit
//     it, and doing this before the grants hands that ability back.
//   - Isolation is swept a second time afterwards, because the step above
//     creates a table of its own that the first sweep could not have seen.
//
// A migration runner that applied the same files in a different order would
// produce a database that looks right and is not, which is why the order is here
// rather than in each caller.
package schema

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// A Step is one thing to do to the database, in order.
type Step struct {
	// Describe is what to print while it runs.
	Describe string
	// File is SQL to apply, relative to the repository root. Empty for a step
	// that only runs Command.
	File string
	// Glob expands to many files, applied in sorted order. A glob rather than a
	// list, because a list is a thing somebody has to remember to add a new
	// service to, and forgetting means a service starts against tables that are
	// not there.
	Glob string
	// Command is SQL to execute after the file, if any.
	Command string
	// Optional marks a glob that may legitimately match nothing.
	Optional bool
	// PerService means each file in the glob belongs to the service in its path
	// and applies into that service's own schema, which is created first.
	//
	// The reason is in libs/integrity/tenantdb/namespace.go: two services
	// defined a table of the same name, both deployments put every service in
	// one database, and CREATE TABLE IF NOT EXISTS made the second definition a
	// silent skip rather than an error.
	PerService bool
}

// NamespaceOf is the schema a file under services/<name>/... belongs in.
//
// Derived from the path rather than from a list, so a new service gets a schema
// by existing. Empty for a path that is not under a service, and for
// audit-service, whose one table is the shared audit trail and stays in public —
// twenty-five services write to it by an unqualified name.
func NamespaceOf(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i, p := range parts {
		if p == "services" && i+1 < len(parts) {
			service := parts[i+1]
			if service == "audit-service" {
				return ""
			}
			return strings.ReplaceAll(service, "-", "_")
		}
	}
	return ""
}

// Plan is the whole sequence, in the order it has to run.
//
// This is the same sequence deploy/postgres-init performs on a fresh data
// directory. The test in this package compares the two and fails if they drift,
// because two copies of an order this particular will not stay in step on their
// own.
func Plan() []Step {
	return []Step{
		{
			Describe: "the ULID polyfill",
			File:     "pkg/database/schema/gen_ulid_polyfill.sql",
		},
		{
			Describe:   "service schemas, each in its own",
			Glob:       "services/*/internal/db/schema.sql",
			PerService: true,
		},
		{
			Describe: "tenant isolation",
			File:     "libs/integrity/isolation/isolation.sql",
			Command:  "SELECT gavya_apply_tenant_isolation(); SELECT gavya_grant_app_access();",
		},
		{
			Describe: "tenant-safe foreign keys",
			File:     "libs/integrity/isolation/foreignkeys.sql",
			Command:  "SELECT gavya_make_foreign_keys_tenant_safe();",
		},
		{
			Describe: "the declared references",
			File:     "libs/integrity/isolation/references.sql",
			Command:  "SELECT constraint_name, outcome FROM gavya_enforce_references();",
		},
		{
			Describe:   "per-service isolation for tables with no tenant column",
			Glob:       "services/*/internal/db/isolation.sql",
			Optional:   true,
			PerService: true,
		},
		{
			Describe: "an append-only, tamper-evident audit trail",
			File:     "services/audit-service/internal/db/tamper_evidence.sql",
		},
		{
			Describe: "isolation again, for tables created since",
			Command:  "SELECT gavya_apply_tenant_isolation();",
		},
	}
}

// Prelude is what to run on the connection before applying this file.
//
// Emitted before every file rather than only before the ones that need it, so
// that a per-service search path cannot outlive the file it was set for and
// carry the next service's tables into somebody else's schema. The whole point
// of the split is that a table lands where it was meant to; leaving that to
// whatever the previous statement set would be the same bug one level up.
func (s Step) Prelude(path string) string {
	ns := ""
	if s.PerService {
		ns = NamespaceOf(path)
	}
	if ns == "" {
		return "SET search_path = public;"
	}
	return fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s; SET search_path = %s, public;", ns, ns)
}

// Files resolves a step to the files it applies, in the order they apply.
func (s Step) Files(root string) ([]string, error) {
	switch {
	case s.File != "":
		p := filepath.Join(root, s.File)
		if _, err := os.Stat(p); err != nil {
			return nil, fmt.Errorf("%s: %w", s.File, err)
		}
		return []string{p}, nil
	case s.Glob != "":
		matches, err := filepath.Glob(filepath.Join(root, s.Glob))
		if err != nil {
			return nil, err
		}
		sort.Strings(matches)
		if len(matches) == 0 && !s.Optional {
			return nil, fmt.Errorf("%s matched nothing; is %s the repository root?", s.Glob, root)
		}
		return matches, nil
	}
	return nil, nil
}
