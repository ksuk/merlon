# Protocol Buffers Workflow

Merlon の Go API と Rust Engine は gRPC で通信する。両者の契約は `proto/` 配下の Protocol Buffers 定義であり、[buf](https://buf.build/) で管理する。

## buf CLI の役割

`buf` は Protocol Buffers のリント・破壊的変更検出・コード生成を統合的に扱うツールである。Merlon では以下を担う。

- **lint** — proto 定義のスタイル・命名規則の検証
- **breaking** — 後方互換性の破壊を検出（Contract Stability 原則）
- **generate** — Go / Rust のコードを生成

設定は `proto/buf.yaml`（モジュール定義）と `proto/buf.gen.yaml`（生成設定）に置く。

## proto ファイルの編集手順

1. `proto/` 配下の `.proto` ファイルを編集する
2. リントを実行して規約違反を確認する

   ```bash
   cd proto && buf lint
   ```

3. 既存契約を変更する場合、破壊的変更がないか確認する

   ```bash
   cd proto && buf breaking --against '.git#branch=main'
   ```

4. コードを生成する（下記）
5. Go / Rust 両側のコードをビルドして整合を確認する

## コード生成

### buf generate / make proto

```bash
make proto
# 内部的には scripts/generate-proto.sh が buf lint → buf generate を実行
```

または直接:

```bash
cd proto && buf generate
```

## Go 側と Rust 側の生成コード管理の違い

両言語で生成コードの扱いが異なる点に注意する。

### Go 側（`api/gen/`）

生成された `.pb.go` / `.grpc.pb.go` は `api/gen/` にコミットする。Go のビルドはコミット済みの生成コードを直接参照するため、`buf generate` を実行しないと最新の proto 変更が反映されない。

- 生成物はリポジトリに含める
- proto 変更時は `make proto` を実行し、生成コードの差分も同じコミットに含める

### Rust 側（`build.rs`）

Rust 側はビルド時に `build.rs` が proto をコンパイルし、`OUT_DIR` にコードを生成する。生成物はリポジトリにコミットしない。

- 生成物は `cargo build` のたびに自動再生成される
- proto 変更は次回ビルドで自動反映される

### まとめ

| 項目 | Go (`api/gen/`) | Rust (`build.rs`) |
|---|---|---|
| 生成タイミング | `buf generate` 実行時 | `cargo build` 時に自動 |
| リポジトリへのコミット | する | しない |
| 反映に必要な操作 | `make proto` | ビルドのみ |

モノレポ構成により、proto 変更と Go/Rust 両側の更新を 1 コミットで完結できる（[ADR-0001](../decisions/0001-monorepo-structure.md) 参照）。
