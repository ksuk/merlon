---
title: Testing Guide
---

# Testing Guide

Merlon has tests at every layer: Go, Rust, TypeScript, and Proto. This
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

### Rust (Engine)

```bash
cd engine && cargo test
```

### UI (TypeScript / React)

```bash
cd ui && npm run test
```

### Proto (Protocol Buffers)

```bash
cd proto && buf lint
```

Detecting breaking changes:

```bash
cd proto && buf breaking --against '.git#branch=main'
```

## Running everything

```bash
make test
```

`make test` runs the Go tests, Rust tests, UI tests, and `buf lint` together.
CI uses the same target.

## Testing strategy (TDD)

Feature work and bug fixes follow TDD (test-driven development).

1. **Write the test** — write a test that expresses the expected behavior
   first (confirm it fails).
2. **Implement** — write the minimal code needed to make the test pass.
3. **Refactor** — clean up while keeping the test green.

### Emphasis by layer

- **Engine (Rust)** — CDD scoring, TM evaluation, and screening are the core
  business logic. To uphold reproducibility of decision rationale
  (Auditability First), tests pin the determinism of output for identical
  input.
- **API (Go)** — CRUD and orchestration. Tests verify service-boundary
  contracts and that failures fall toward alerting rather than silence
  (Fail-Alert).
- **Proto** — Contract Stability (12+ months of backward compatibility) is
  mechanically enforced with `buf breaking`.
- **UI** — component-level tests plus user-flow tests for the investigation
  workflow.

### Follow existing patterns

New tests should follow the existing test framework, naming conventions, and
patterns in each directory. Go uses the standard `testing` package, Rust
uses `#[cfg(test)]` modules, and the UI follows its existing test-runner
configuration.
