# Docker Compose Production Setup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the full stack (PostgreSQL + Go API + React dashboard) runnable on a server with `docker compose up -d`.

**Architecture:** Multi-stage Docker builds for both the Go API and the React dashboard. The API image uses golang:1.24-alpine to build and alpine:3.21 to run. The dashboard image uses node:22-alpine to build and nginx:alpine to serve static files. docker-compose wires all three services together with proper healthchecks and dependency ordering.

**Tech Stack:** Docker, docker compose v2, Go 1.25, Node 22, nginx:alpine, PostgreSQL 16

---

## File Map

| Action | Path | Responsibility |
|---|---|---|
| Create | `Dockerfile` | Multi-stage Go API build |
| Create | `dashboard/Dockerfile` | Multi-stage React build → nginx |
| Create | `dashboard/nginx.conf` | SPA routing + gzip for nginx |
| Create | `.env.example` | Production secrets template (already excluded from git) |
| Modify | `docker-compose.yml` | Add api + dashboard services, postgres healthcheck |
| Modify | `Makefile` | Add `docker-build` target |
| Modify | `docs/commands.md` | Document new Docker commands |

---

### Task 1: Go API Dockerfile

**Files:**
- Create: `Dockerfile`

- [ ] **Step 1: Create `Dockerfile`**

```dockerfile
FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o bin/poly-server ./cmd/server

FROM alpine:3.21

RUN addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /app
COPY --from=builder /app/bin/poly-server .

USER appuser

EXPOSE 8080
CMD ["./poly-server"]
```

- [ ] **Step 2: Verify image builds**

```bash
docker build -t poly-api-test .
```

Expected: build completes, final image ~20MB. No errors.

- [ ] **Step 3: Commit**

```bash
git add Dockerfile
git commit -m "feat: add multi-stage Dockerfile for Go API"
```

---

### Task 2: nginx config for React SPA

**Files:**
- Create: `dashboard/nginx.conf`

- [ ] **Step 1: Create `dashboard/nginx.conf`**

```nginx
server {
    listen 80;
    root /usr/share/nginx/html;
    index index.html;

    gzip on;
    gzip_types text/plain text/css application/json application/javascript text/xml application/xml application/xml+rss text/javascript;

    location / {
        try_files $uri $uri/ /index.html;
    }
}
```

The `try_files` directive is what enables SPA routing — any path that doesn't match a static file falls back to `index.html`, letting React Router handle it client-side.

- [ ] **Step 2: Commit**

```bash
git add dashboard/nginx.conf
git commit -m "feat: add nginx config for React SPA serving"
```

---

### Task 3: React dashboard Dockerfile

**Files:**
- Create: `dashboard/Dockerfile`

- [ ] **Step 1: Create `dashboard/Dockerfile`**

```dockerfile
FROM node:22-alpine AS builder

WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci

COPY . .
RUN npm run build

FROM nginx:alpine

COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80
```

- [ ] **Step 2: Verify image builds**

```bash
docker build -t poly-dashboard-test ./dashboard
```

Expected: build completes, `dist/` compiled by Vite, served by nginx. No errors.

- [ ] **Step 3: Commit**

```bash
git add dashboard/Dockerfile
git commit -m "feat: add multi-stage Dockerfile for React dashboard"
```

---

### Task 4: Update docker-compose.yml

**Files:**
- Modify: `docker-compose.yml`

- [ ] **Step 1: Replace `docker-compose.yml` with full content**

```yaml
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: poly
      POSTGRES_PASSWORD: poly
      POSTGRES_DB: poly
    ports:
      - "5432:5432"
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U poly"]
      interval: 5s
      timeout: 5s
      retries: 5

  api:
    build: .
    ports:
      - "8080:8080"
    environment:
      DATABASE_URL: ${DATABASE_URL:-postgres://poly:poly@postgres:5432/poly?sslmode=disable}
      JWT_SECRET: ${JWT_SECRET:-dev-secret-change-me}
      ENCRYPTION_KEY: ${ENCRYPTION_KEY:-dev-encryption-key-32bytes!!!!!!}
      POLYMARKET_CLOB_URL: ${POLYMARKET_CLOB_URL:-https://clob.polymarket.com}
      POLYMARKET_GAMMA_URL: ${POLYMARKET_GAMMA_URL:-https://gamma-api.polymarket.com}
    depends_on:
      postgres:
        condition: service_healthy

  dashboard:
    build: ./dashboard
    ports:
      - "3000:80"

volumes:
  pgdata:
```

Key points:
- `DATABASE_URL` uses `postgres` as hostname (Docker internal DNS, not `localhost`)
- All secrets have dev defaults via `${VAR:-default}` syntax — safe for local dev, overridden by `.env` on server
- `api` waits for postgres healthcheck before starting

- [ ] **Step 2: Smoke test — start all services**

```bash
docker compose up -d
docker compose ps
```

Expected output: all three services show `Up` or `running`, no `Exit` or `Restarting`.

Check api is accessible:
```bash
curl http://localhost:8080/health
```
Expected: `200 OK`

Check dashboard is accessible:
```bash
curl -I http://localhost:3000
```
Expected: `200 OK` with `Content-Type: text/html`

- [ ] **Step 3: Stop and clean up test containers**

```bash
docker compose down
```

- [ ] **Step 4: Commit**

```bash
git add docker-compose.yml
git commit -m "feat: add api and dashboard services to docker-compose"
```

---

### Task 5: `.env.example`

**Files:**
- Create: `.env.example`

Note: `.gitignore` already has `.env` excluded and `!.env.example` whitelisted — no gitignore changes needed.

- [ ] **Step 1: Create `.env.example`**

```bash
# Copy to .env and fill in production values before running docker compose up -d
# ENCRYPTION_KEY must be exactly 32 bytes

JWT_SECRET=change-me-min-32-chars
ENCRYPTION_KEY=change-me-exactly-32-bytes!!!!!
DATABASE_URL=postgres://poly:poly@postgres:5432/poly?sslmode=disable
POLYMARKET_CLOB_URL=https://clob.polymarket.com
POLYMARKET_GAMMA_URL=https://gamma-api.polymarket.com
```

- [ ] **Step 2: Commit**

```bash
git add .env.example
git commit -m "feat: add .env.example for production deployment"
```

---

### Task 6: Update Makefile and docs

**Files:**
- Modify: `Makefile`
- Modify: `docs/commands.md`

- [ ] **Step 1: Add `docker-build` to `.PHONY` line and add the target in `Makefile`**

Change `.PHONY` line from:
```makefile
.PHONY: run build migrate docker-up docker-down generate dev-dashboard test test-sdk tidy
```
To:
```makefile
.PHONY: run build migrate docker-up docker-down docker-build generate dev-dashboard test test-sdk tidy
```

Add the target after `docker-down`:
```makefile
# Build Docker images
docker-build:
	docker compose build
```

- [ ] **Step 2: Update `docs/commands.md` — expand the Docker section**

Replace the existing Docker section (lines with `docker-up` / `docker-down`) with:

```markdown
## Docker

```bash
make docker-build     # Build all Docker images (api + dashboard)
make docker-up        # Start PostgreSQL via docker-compose (local dev only)
make docker-down      # Stop docker-compose services
docker compose up -d  # Start full stack: postgres + api + dashboard
docker compose down   # Stop full stack
```
```

- [ ] **Step 3: Verify `make docker-build` works**

```bash
make docker-build
```

Expected: both `api` and `dashboard` images build without errors.

- [ ] **Step 4: Commit**

```bash
git add Makefile docs/commands.md
git commit -m "feat: add docker-build make target; update commands docs"
```
