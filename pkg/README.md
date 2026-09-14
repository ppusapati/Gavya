# pkg

The shared module `p9e.in/samavaya/packages`, cut down to what this platform
uses.

It arrived as a library carried over from another product: over a hundred
packages, most of which did not build here because the packages they imported
were never brought across. The platform imported two of them. Everything else
was deleted, so the module builds, vets and tests as a whole and is gated by
`scripts/check-all.sh` like every other module in the workspace.

What is left:

- `p9log` — the structured logger every service writes through.
- `ulid` — the identifier every row is keyed on.
- `database/schema/gen_ulid_polyfill.sql` — the SQL function that generates
  those identifiers inside PostgreSQL. `deploy/postgres-init` applies it before
  any service schema, and the e2e harness does the same.

The module keeps its original import path so nothing that imports it had to
change. A new dependency on this module is a new package here, written for
this platform; the deleted code is in the history if any of it turns out to be
wanted, and the commit that removed it says why it went.
