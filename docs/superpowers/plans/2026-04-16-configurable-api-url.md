# Configurable Dashboard API URL Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow the Go API address used by the dashboard to be configured via an environment variable, so changing the API host/port doesn't break the dashboard.

**Architecture:** In production (nginx), the dashboard proxies API calls (`/auth`, `/api/`, `/clob/`) to the Go API via `proxy_pass`. The upstream URL is injected at container startup using nginx's built-in `envsubst` template support — no rebuild required. In dev (Vite dev server), the same env var drives the proxy target in `vite.config.ts`. The `client.ts` `baseURL` stays empty (relative URLs) — no client-side changes needed.

**Tech Stack:** nginx:alpine (envsubst templates), Vite (dev proxy), docker-compose env vars

---

## File Map

| File | Change |
|---|---|
| `dashboard/nginx.conf` | Add proxy_pass blocks for `/auth`, `/api/`, `/clob/`; replace with template using `${API_URL}` |
| `dashboard/Dockerfile` | Copy nginx.conf as template to `/etc/nginx/templates/default.conf.template` |
| `dashboard/vite.config.ts` | Read `process.env.API_URL` for proxy target instead of hardcoded value |
| `docker-compose.yml` | Add `API_URL` env var to `dashboard` service |
| `.env.example` | Add `API_URL` |
| `docs/env.md` | Document `API_URL` |

---

### Task 1: Update nginx.conf to proxy API calls with a configurable upstream

**Files:**
- Modify: `dashboard/nginx.conf`

The nginx official image (`nginx:alpine`) automatically processes files in `/etc/nginx/templates/` — it runs `envsubst` on `*.template` files and writes the output to `/etc/nginx/conf.d/`. We use `${API_URL}` as the placeholder.

- [ ] **Step 1: Replace nginx.conf with the new template content**

Open `dashboard/nginx.conf` and replace the entire content with:

```nginx
server {
    listen 80;
    root /usr/share/nginx/html;
    index index.html;

    gzip on;
    gzip_types text/plain text/css application/json application/javascript text/xml application/xml application/xml+rss text/javascript;

    location /auth {
        proxy_pass ${API_URL};
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }

    location /api/ {
        proxy_pass ${API_URL};
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }

    location /clob/ {
        proxy_pass ${API_URL};
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }

    location / {
        try_files $uri $uri/ /index.html;
    }
}
```

- [ ] **Step 2: Commit**

```bash
git add dashboard/nginx.conf
git commit -m "feat: add API proxy locations to nginx with configurable upstream"
```

---

### Task 2: Update dashboard Dockerfile to use nginx template mechanism

**Files:**
- Modify: `dashboard/Dockerfile`

The nginx image processes `/etc/nginx/templates/*.template` files at startup (runs `envsubst`, outputs to `/etc/nginx/conf.d/`). We copy `nginx.conf` as `default.conf.template` instead of `default.conf`.

- [ ] **Step 1: Change the COPY line in the Dockerfile**

In `dashboard/Dockerfile`, change:

```dockerfile
COPY nginx.conf /etc/nginx/conf.d/default.conf
```

to:

```dockerfile
COPY nginx.conf /etc/nginx/templates/default.conf.template
```

Full Dockerfile after change:

```dockerfile
FROM node:22-alpine AS builder

WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci

COPY . .
RUN npm run build

FROM nginx:alpine

COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/templates/default.conf.template

EXPOSE 80
```

- [ ] **Step 2: Commit**

```bash
git add dashboard/Dockerfile
git commit -m "feat: use nginx template mechanism for runtime env var substitution"
```

---

### Task 3: Update docker-compose.yml to pass API_URL to the dashboard service

**Files:**
- Modify: `docker-compose.yml`

Inside docker-compose, the Go API container is reachable at `http://api:<port>` (internal DNS). Default to `http://api:8080` matching the current `PORT` default.

- [ ] **Step 1: Add environment block to the dashboard service**

In `docker-compose.yml`, change the `dashboard` service from:

```yaml
  dashboard:
    build: ./dashboard
    ports:
      - "3000:80"
```

to:

```yaml
  dashboard:
    build: ./dashboard
    ports:
      - "3000:80"
    environment:
      API_URL: ${API_URL:-http://api:8080}
```

- [ ] **Step 2: Commit**

```bash
git add docker-compose.yml
git commit -m "feat: pass API_URL to dashboard container for nginx proxy target"
```

---

### Task 4: Update .env.example to document API_URL

**Files:**
- Modify: `.env.example`

The `.env.example` documents production values. `API_URL` is only needed when the user wants a non-default API address (e.g. custom port or external host).

- [ ] **Step 1: Add API_URL to .env.example**

In `.env.example`, add after the existing variables:

```
# Dashboard → API proxy target (default: http://api:8080 — internal docker DNS)
# Override if you expose the API on a different port or host
API_URL=http://api:8080
```

Full `.env.example` after change:

```
# Copy to .env and fill in production values before running docker compose up -d
# ENCRYPTION_KEY must be exactly 32 bytes

JWT_SECRET=change-me-min-32-chars
ENCRYPTION_KEY=change-me-exactly-32-bytes!!!!!
DATABASE_URL=postgres://poly:poly@postgres:5432/poly?sslmode=disable
POLYMARKET_CLOB_URL=https://clob.polymarket.com
POLYMARKET_GAMMA_URL=https://gamma-api.polymarket.com

# Dashboard → API proxy target (default: http://api:8080 — internal docker DNS)
# Override if you expose the API on a different port or host
API_URL=http://api:8080
```

- [ ] **Step 2: Commit**

```bash
git add .env.example
git commit -m "docs: add API_URL to .env.example"
```

---

### Task 5: Update vite.config.ts to read API_URL from environment for dev proxy

**Files:**
- Modify: `dashboard/vite.config.ts`

The Vite dev server proxy also needs to respect the same variable. `vite.config.ts` runs in Node context and can read `process.env` directly.

- [ ] **Step 1: Replace hardcoded URL in vite.config.ts**

Change:

```typescript
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 3000,
    proxy: {
      '/auth': 'http://localhost:8080',
      '/api/': 'http://localhost:8080',
    },
  },
})
```

to:

```typescript
const apiUrl = process.env.API_URL ?? 'http://localhost:8080';

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 3000,
    proxy: {
      '/auth': apiUrl,
      '/api/': apiUrl,
      '/clob/': apiUrl,
    },
  },
})
```

Note: also added `/clob/` proxy which was missing from the original (needed for cancel order and other CLOB calls in the dashboard).

- [ ] **Step 2: Commit**

```bash
git add dashboard/vite.config.ts
git commit -m "feat: read API_URL from env for vite dev proxy; add /clob/ proxy"
```

---

### Task 6: Update docs/env.md

**Files:**
- Modify: `docs/env.md`

- [ ] **Step 1: Add API_URL row to the env table**

Add `API_URL` as a new row:

```markdown
| `API_URL` | `http://api:8080` | Dashboard nginx proxy upstream — set to `http://api:<PORT>` if you change `PORT`; only used by the dashboard container |
```

The full updated table in `docs/env.md`:

```markdown
| Variable | Default | Notes |
|---|---|---|
| `PORT` | `8080` | HTTP listen port |
| `DATABASE_URL` | `postgres://poly:poly@localhost:5432/poly?sslmode=disable` | pgx connection string |
| `JWT_SECRET` | `dev-secret-change-me` | HS256 signing key for dashboard Bearer tokens |
| `POLYMARKET_CLOB_URL` | `https://clob.polymarket.com` | Live CLOB — used for proxy and orderbook matching |
| `POLYMARKET_GAMMA_URL` | `https://gamma-api.polymarket.com` | Gamma API — used for token/market resolution and sync |
| `ENCRYPTION_KEY` | `dev-encryption-key-32bytes!!!!!!` | AES-256 key for Ethereum private key storage — **must be exactly 32 bytes** |
| `API_URL` | `http://api:8080` | Dashboard nginx proxy upstream — set to `http://api:<PORT>` if you change `PORT`; only used by the dashboard container |
```

- [ ] **Step 2: Commit**

```bash
git add docs/env.md
git commit -m "docs: document API_URL env var"
```

---

## Usage After This Change

To run the API on a different port (e.g. 9090):

```env
# .env
PORT=9090
API_URL=http://api:9090
```

```bash
docker compose up -d
```

The dashboard nginx will now proxy API calls to `http://api:9090`.

For local dev:

```bash
API_URL=http://localhost:9090 npm run dev
```
