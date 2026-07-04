# Go API

## Commands

```bash
cd api && go test ./...                   # All tests
cd api && go test ./internal/server/...   # Single package
cd api && go test -run TestName ./...     # Single test
cd api && go test -cover ./...            # With coverage
cd api && go build ./cmd/merlon-api       # Build
cd api && go vet ./...                    # Lint
```

## Structure

Under `api/internal/`:

| Package | Role |
|---|---|
| `domain/` | Entity definitions and repository interfaces (core) |
| `server/` | HTTP handlers (one file per resource) |
| `store/` | Repository implementations: `postgres.go` (production) and `memory.go` (dev/test) |
| `engine/` | gRPC client to Rust Engine. Engine interfaces defined in `interface.go` |
| `adapter/` | Ingestion layer for external system integration (Adapter Isolation principle) |
| `config/` | Config loading from env vars and YAML |
| `seed/` | Dev demo data seeder |

Entry point: `api/cmd/merlon-api/main.go`

## Patterns

- Repository pattern: interfaces in `domain/repository.go` → implementations in `store/`
- DI: `server.Deps` struct for dependency injection (assembled in main.go)
- Tests: standard `testing` package. Files named `*_test.go`
- Generated code: protobuf output in `api/gen/` is **gitignored and regenerated at build time** (`cd proto && buf generate`, or `make proto`). Run this before `go build`/`go test` on a fresh checkout.
