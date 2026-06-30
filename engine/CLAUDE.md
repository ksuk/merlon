# Rust Engine

## Commands

```bash
cd engine && cargo test                   # All tests
cd engine && cargo test test_name         # Single test
cd engine && cargo build                  # Build
cd engine && cargo clippy -- -D warnings  # Lint
```

## Structure

Under `engine/crates/merlon-engine/src/`:

| Module | Role |
|---|---|
| `scoring/` | CDD scoring engine (central axis of the system) |
| `monitoring/` | TM scenario evaluation. Individual scenarios in `scenarios/` |
| `screening/` | Sanctions list and PEP matching |
| `backtest/` | Rule change impact verification against historical data |
| `grpc/` | gRPC service implementations for each engine |

Workspace root: `engine/Cargo.toml`

## Patterns

- Tests: `#[cfg(test)]` modules, files named `*_test.rs`
- Proto generation: `build.rs` auto-generates at build time → output in `OUT_DIR`. **Not committed**
- Each engine follows `config.rs` (config) + `engine.rs` (logic) + `*_test.rs` (tests) layout
