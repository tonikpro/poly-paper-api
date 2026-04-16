.PHONY: run build migrate docker-up docker-down docker-build generate dev-dashboard test test-sdk tidy

# Start the server
run:
	go run ./cmd/server

# Build the binary
build:
	go build -o bin/poly-server ./cmd/server

# Run database migrations (handled automatically on server start)
migrate: run

# Start PostgreSQL
docker-up:
	docker compose up -d

# Stop PostgreSQL
docker-down:
	docker compose down

# Build Docker images
docker-build:
	docker compose build

# Generate code from OpenAPI specs
generate:
	oapi-codegen --config api/generated/dashboard/config.yaml api/openapi/dashboard.yaml
	oapi-codegen --config api/generated/clob/config.yaml api/openapi/clob.yaml

# Start React dashboard dev server
dev-dashboard:
	. $(HOME)/.nvm/nvm.sh && cd dashboard && npm run dev

# Run Go tests
test:
	go test ./... -v

# Run SDK compatibility tests
test-sdk:
	cd tests/sdk_compat && python3 -m pytest -v

# Tidy go modules
tidy:
	go mod tidy
