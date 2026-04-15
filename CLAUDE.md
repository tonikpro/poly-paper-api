# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

Paper trading service replicating Polymarket's CLOB API for algo bot testing. Handles auth, order placement/matching, positions, and market resolution. Market data is proxied live to Polymarket.

## Quick Reference

| Topic | File |
|---|---|
| Build, test, generate commands | [`docs/commands.md`](docs/commands.md) |
| Environment variables | [`docs/env.md`](docs/env.md) |
| Full architecture (packages, DB schema, workers) | [`docs/architecture.md`](docs/architecture.md) |
| Auth layers (JWT / L1 EIP-712 / L2 HMAC) | [`docs/auth.md`](docs/auth.md) |
| CLOB endpoint parity (local vs proxied) | [`docs/clob-parity.md`](docs/clob-parity.md) |
| Polymarket CLOB API reference | [`docs/polymarket-clob-api.md`](docs/polymarket-clob-api.md) |
| Current parity status and gaps | [`PLAN.md`](PLAN.md) |

## Most Important Rules

- **Never edit generated files** in `api/generated/` — edit the OpenAPI specs in `api/openapi/` and run `make generate`
- **Polymarket CLOB API details** — read `docs/polymarket-clob-api.md` first, before external sources
- **L2 auth path signing** — SDKs sign paths *without* `/clob` prefix; middleware strips it before verifying
- **Update docs** — after changing `internal/`, `api/openapi/`, `Makefile`, or `docker-compose.yml`, update the relevant file in `docs/` in the same response

## Key Entry Points

- **`cmd/server/main.go`** — wires everything: DB, services, handlers, router, background workers
- **`api/openapi/clob.yaml`** / **`api/openapi/dashboard.yaml`** — source of truth for all HTTP contracts
- **`internal/server/clob_server.go`** — `CLOBServer` composes all CLOB handlers into one `ServerInterface`
- **`internal/database/migrations/`** — SQL schema (migrations run automatically on server start)
