# Docker Compose Production Setup — Design

**Date:** 2026-04-16

## Goal

Make the entire stack (PostgreSQL + Go API + React dashboard) runnable on a server with a single `docker compose up -d`. TLS is handled externally (Caddy/nginx/Cloudflare); compose serves plain HTTP.

## New Files

### `Dockerfile` (Go API, multi-stage)

- **Stage 1 `builder`:** `golang:1.24-alpine` — copies repo, runs `go build -o bin/poly-server ./cmd/server`
- **Stage 2:** `alpine:3.21` — copies binary only, runs as non-root user (`appuser`)
- `EXPOSE 8080`

### `dashboard/Dockerfile` (React, multi-stage)

- **Stage 1 `builder`:** `node:22-alpine` — runs `npm ci`, then `npm run build` → output in `dist/`
- **Stage 2:** `nginx:alpine` — copies `dist/` into `/usr/share/nginx/html`, custom `nginx.conf` for SPA routing (all paths → `index.html`), gzip enabled
- Listens on port `3000`

### `.env.example`

Template for production secrets. Copy to `.env` on the server before `docker compose up`.

```
JWT_SECRET=change-me-min-32-chars
ENCRYPTION_KEY=change-me-exactly-32-bytes!!!!!
DATABASE_URL=postgres://poly:poly@postgres:5432/poly?sslmode=disable
POLYMARKET_CLOB_URL=https://clob.polymarket.com
POLYMARKET_GAMMA_URL=https://gamma-api.polymarket.com
```

## Modified Files

### `docker-compose.yml`

Three services:

| Service | Build / Image | Host Port | Depends On |
|---|---|---|---|
| `postgres` | `postgres:16-alpine` | 5432 | — |
| `api` | `build: .` | 8080 | postgres (healthy) |
| `dashboard` | `build: ./dashboard` | 3000 | — |

- `postgres` healthcheck: `pg_isready -U poly`
- `api` uses `depends_on: postgres: condition: service_healthy`
- `api` env vars sourced from `.env` (or `environment:` block with `${VAR}` substitution)
- `pgdata` named volume persists between restarts

### `Makefile`

Add one target:

```makefile
docker-build:
    docker compose build
```

Existing `build` target (Go binary) is unchanged.

### `docs/commands.md`

Add Docker section entries:

```
make docker-build    # Build all Docker images (api + dashboard)
docker compose up -d # Start full stack (postgres + api + dashboard)
docker compose down  # Stop all services
```

## Ports Summary

| Service | Container Port | Host Port |
|---|---|---|
| postgres | 5432 | 5432 |
| api | 8080 | 8080 |
| dashboard | 80 (nginx) | 3000 |

## Not In Scope

- TLS/HTTPS — handled by external reverse proxy
- CI/CD pipeline
- Image registry push
