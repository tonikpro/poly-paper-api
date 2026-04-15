# Environment Variables

All variables are loaded via `github.com/kelseyhightower/envconfig` in `internal/config/config.go`.

| Variable | Default | Notes |
|---|---|---|
| `PORT` | `8080` | HTTP listen port |
| `DATABASE_URL` | `postgres://poly:poly@localhost:5432/poly?sslmode=disable` | pgx connection string |
| `JWT_SECRET` | `dev-secret-change-me` | HS256 signing key for dashboard Bearer tokens |
| `POLYMARKET_CLOB_URL` | `https://clob.polymarket.com` | Live CLOB — used for proxy and orderbook matching |
| `POLYMARKET_GAMMA_URL` | `https://gamma-api.polymarket.com` | Gamma API — used for token/market resolution and sync |
| `ENCRYPTION_KEY` | `dev-encryption-key-32bytes!!!!!!` | AES-256 key for Ethereum private key storage — **must be exactly 32 bytes** |

For local dev, defaults work out of the box once `make docker-up` is running.
