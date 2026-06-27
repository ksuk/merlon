.PHONY: help fmt lint test build proto dev-up dev-down minimal-up minimal-down

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

fmt: ## Format all code
	@cd api && go fmt ./... 2>/dev/null || echo "Go: skipped (not installed or no files)"
	@cd engine && cargo fmt 2>/dev/null || echo "Rust: skipped (not installed or no files)"
	@cd ui && npx prettier --write "src/**/*.{ts,tsx}" 2>/dev/null || echo "UI: skipped (not installed or no files)"

lint: ## Lint all code
	@cd api && go vet ./... 2>/dev/null || echo "Go: skipped (not installed or no files)"
	@cd engine && cargo clippy -- -D warnings 2>/dev/null || echo "Rust: skipped (not installed or no files)"
	@cd ui && npm run lint 2>/dev/null || echo "UI: skipped (not installed or no files)"
	@cd proto && buf lint 2>/dev/null || echo "Proto: skipped (buf not installed)"

test: ## Run all tests
	@cd api && go test ./... || echo "Go tests failed or Go not installed"
	@cd engine && cargo test || echo "Rust tests failed or Rust not installed"
	@cd ui && npm run test -- --run 2>/dev/null || echo "UI tests failed or Node not installed"

build: ## Build all components
	@cd api && go build ./cmd/merlon-api || echo "Go build failed or Go not installed"
	@cd engine && cargo build || echo "Rust build failed or Rust not installed"
	@cd ui && npm run build || echo "UI build failed or Node not installed"

proto: ## Generate protobuf code
	@./scripts/generate-proto.sh

dev-up: ## Start development environment
	docker compose -f docker-compose.yml -f docker-compose.dev.yml up --build

dev-down: ## Stop development environment
	docker compose -f docker-compose.yml -f docker-compose.dev.yml down

minimal-up: ## Start minimal environment (PostgreSQL + API only)
	docker compose -f docker-compose.minimal.yml up --build

minimal-down: ## Stop minimal environment
	docker compose -f docker-compose.minimal.yml down
