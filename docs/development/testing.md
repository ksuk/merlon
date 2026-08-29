---
title: Testing Guide
---

# Testing Guide

Merlon has tests at every layer: Go and TypeScript. This
document covers how to run each and the overall testing strategy.

## Running tests per component

### Go (API)

```bash
cd api && go test ./...
```

With coverage:

```bash
cd api && go test -cover ./...
```

### UI (TypeScript / React)

```bash
cd ui && npm run test
```

## Running everything

```bash
make test
```

`make test` runs the Go and UI tests. CI uses the same target.

## PostgreSQL integration tests

Run the PostgreSQL integration gate against a fresh, dedicated database:

```bash
MERLON_DATABASE_URL=postgres://merlon:<password>@127.0.0.1:5432/merlon \
  make test-integration
```

The target applies the complete migration set twice before running all Go
tests. Integration packages share the dedicated database, so the target uses
`go test -p=1` to serialize packages and prevent one package's cleanup or
retention operations from changing another package's fixtures. It does not
skip packages or tests.

Do not point this target at a database used by another process or test run.
Create a fresh database (or fresh Compose project and volume) for each run.

## Testing strategy (TDD)

Feature work and bug fixes follow TDD (test-driven development).

1. **Write the test** — write a test that expresses the expected behavior
   first (confirm it fails).
2. **Implement** — write the minimal code needed to make the test pass.
3. **Refactor** — clean up while keeping the test green.

### Emphasis by layer

- **Native engine (Go)** — CDD scoring, TM evaluation, screening, and
  backtesting are the core
  business logic. To uphold reproducibility of decision rationale
  (Auditability First), tests pin the determinism of output for identical
  input.
- **API (Go)** — CRUD and orchestration. Tests verify service-boundary
  contracts and that failures fall toward alerting rather than silence
  (Fail-Alert).
- **UI** — component-level tests plus user-flow tests for the investigation
  workflow.

### Follow existing patterns

New tests should follow the existing test framework, naming conventions, and
patterns in each directory. Go uses the standard `testing` package, and the UI
follows its existing test-runner configuration.
