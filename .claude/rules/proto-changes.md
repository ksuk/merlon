---
paths:
  - "proto/**"
---
When changing proto files, run `make proto` to regenerate Go and Rust code.
Go stubs (`api/gen/`) are gitignored and regenerated at build time — do not commit them.
Do not commit Rust-side generated code (`engine/target/`).
Before making breaking changes, verify with `cd proto && buf breaking --against '.git#branch=main'`.
