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
			Describe: "service schemas",
			Glob:     "services/*/internal/db/schema.sql",
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
			Describe: "per-service isolation for tables with no tenant column",
			Glob:     "services/*/internal/db/isolation.sql",
			Optional: true,
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
