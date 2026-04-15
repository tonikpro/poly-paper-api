# Commands Reference

## Local Development

```bash
make docker-up       # Start PostgreSQL via docker-compose (required before server)
make docker-down     # Stop PostgreSQL
make run             # go run ./cmd/server — DB migrations run automatically on start
make build           # Produces bin/poly-server
```

## Code Generation

```bash
make generate        # Regenerate api/generated/{dashboard,clob}/ from OpenAPI specs
                     # Uses oapi-codegen — must be installed: go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest
```

## Testing

```bash
make test            # go test ./... -v (all Go unit/integration tests)
make test-sdk        # Python SDK compatibility tests in tests/sdk_compat/ (requires pytest)

# Run a single test
go test ./internal/trading/... -run TestMatchOrder -v
go test ./internal/auth/... -run TestVerifyL1 -v
go test ./internal/server/... -v   # integration tests (requires running DB)
```

## Dashboard

```bash
make dev-dashboard   # Starts Vite dev server (requires nvm + node)
                     # Sources ~/.nvm/nvm.sh first
```

## Docs

```bash
make docs            # Regenerate docs/ from current codebase (runs Claude Code)
```

## Module Maintenance

```bash
make tidy            # go mod tidy
```
