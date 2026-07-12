---
title: Protocol Buffers Workflow
---

# Protocol Buffers Workflow

Merlon's Go API and Rust Engine communicate over gRPC. The contract between
them is the Protocol Buffers definitions under `proto/`, managed with
[buf](https://buf.build/).

## What the buf CLI does

`buf` handles linting, breaking-change detection, and code generation for
Protocol Buffers in one tool. In Merlon it is responsible for:

- **lint** — checking proto definitions against style and naming rules
- **breaking** — detecting backward-incompatible changes (the Contract
  Stability principle)
- **generate** — generating Go and Rust code

Configuration lives in `proto/buf.yaml` (module definition) and
`proto/buf.gen.yaml` (generation settings).

## Editing a proto file

1. Edit the `.proto` files under `proto/`.
2. Run the linter to check for style violations:

   ```bash
   cd proto && buf lint
   ```

3. If you're changing an existing contract, check for breaking changes:

   ```bash
   cd proto && buf breaking --against '.git#branch=main'
   ```

4. Generate code (see below).
5. Build both the Go and Rust sides to confirm they stay in sync.

## Code generation

### buf generate / make proto

```bash
make proto
# Internally this runs scripts/generate-proto.sh, which does buf lint → buf generate
```

Or directly:

```bash
cd proto && buf generate
```

## Go and Rust generate code differently

Both sides regenerate code from the same proto definitions, but the timing
and mechanism differ. Neither side commits generated code to the
repository — `api/gen/` and the Rust `OUT_DIR` output are both listed in
`.gitignore`.

### Go side (`api/gen/`)

Generated `.pb.go` / `.grpc.pb.go` files land in `api/gen/`, which is
gitignored. The Go build imports this generated package directly
(`github.com/ksuk/merlon/api/gen/merlon/v1`), so **you must run
`make proto` (or `buf generate`) explicitly before `go build` / `go test`** —
plain `go build` does not regenerate it for you. On a fresh checkout, forget
this step and the build fails with missing-package errors.

- Generated output is not committed; it is regenerated on demand.
- When you change a proto file, run `make proto` and rebuild/retest before
  committing your change.

### Rust side (`build.rs`)

On the Rust side, `build.rs` compiles the proto files at build time and
writes the generated code into Cargo's `OUT_DIR`, which `tonic::include_proto!`
then pulls in. This output is never committed to the repository.

- Generated output is automatically regenerated on every `cargo build`.
- A proto change takes effect automatically the next time you build — no
  extra step required.

### Summary

| Aspect | Go (`api/gen/`) | Rust (`build.rs` / `OUT_DIR`) |
|---|---|---|
| When it regenerates | Only when you run `buf generate` (`make proto`) | Automatically, every `cargo build` |
| Committed to the repository | No (gitignored) | No (gitignored) |
| What you must do to pick up a proto change | Run `make proto` before building/testing | Just build — it's automatic |

The monorepo structure lets a proto change and the corresponding Go/Rust
updates land in a single commit (see
ADR-0001).
