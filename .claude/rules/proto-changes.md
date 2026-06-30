---
paths:
  - "proto/**"
  - "api/gen/**"
---
When changing proto files, run `make proto` and include the generated `api/gen/` diffs in the same commit.
Do not commit Rust-side generated code (`engine/target/`).
Before making breaking changes, verify with `cd proto && buf breaking --against '.git#branch=main'`.
