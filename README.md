# Gavya

Cloud-native multi-tenant dairy ERP, with an integrity layer that lets it run in
shadow mode alongside an incumbent system before anyone is asked to trust it.

## Layout

```
services/     Go microservices (ConnectRPC, pgx, PostgreSQL)
ml/           Rust ML tier (cargo workspace) — reached only over the network
libs/         Go libraries shared across services
pkg/          Shared platform packages (p9e.in/samavaya/packages)
docs/         Architecture documentation
```

## The ERP services

Dairy domain: `cattle`, `milk`, `breeding`, `health`, `feed`, `farm`.
Commerce: `cattle-market`, `product-catalog`, `inventory`, `order`, `billing`.
Platform: `tenant`, `notification`, `reporting`, `audit`, `file`, `gateway`.

Each follows Handler → Service → Repository → DB, with handwritten SQL, ULID
identifiers, and `tenant_id` on every table and in every query.

## The integrity layer

`ingestion` · `canonical` · `observation` · `pooling` · `shadow-settlement`, plus
the Rust `ml/` tier for anomaly detection, measurement uncertainty, mass-balance
reconciliation and divergence explanation.

All ML and AI runs in Rust as separate processes, integrated with Go only through
service-to-service calls — no cgo, no shared memory. Every ML call is advisory: the
platform produces a complete, authoritative answer whether or not the ML tier is
reachable.

See [docs/integrity-platform.md](docs/integrity-platform.md) for what it guarantees
and where each guarantee is enforced.

## Building

```sh
go build ./...                    # from any service directory
cd ml && cargo build --workspace
```

Test instructions, including the Go↔Rust contract tests and the database tests, are
in [docs/integrity-platform.md](docs/integrity-platform.md#running-the-tests).
