# Protocol Buffers ワークフロー

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

## Go と Rust とでコード生成の仕組みが異なる

両者とも同じ proto 定義からコードを再生成するが、タイミングと仕組みが異なる。どちらの側も生成コードをリポジトリにコミットしない — `api/gen/` と Rust の `OUT_DIR` 出力は、いずれも `.gitignore` に含まれている。

### Go 側（`api/gen/`）

生成された `.pb.go` / `.grpc.pb.go` は `api/gen/`（gitignore 対象）に配置される。Go のビルドはこの生成パッケージ（`github.com/ksuk/merlon/api/gen/merlon/v1`）を直接インポートするため、**`go build` / `go test` の前に必ず `make proto`（または `buf generate`）を明示的に実行する必要がある** — 通常の `go build` では自動再生成されない。新規チェックアウト時にこの手順を忘れると、パッケージ不足エラーでビルドが失敗する。

- 生成物はコミットしない。必要に応じて都度再生成する。
- proto ファイルを変更したら、コミット前に `make proto` を実行してリビルド・再テストする。

### Rust 側（`build.rs`）

Rust 側では、ビルド時に `build.rs` が proto ファイルをコンパイルし、生成コードを Cargo の `OUT_DIR` に書き出す。それを `tonic::include_proto!` が取り込む。この出力はリポジトリに一切コミットされない。

- 生成物は `cargo build` のたびに自動的に再生成される。
- proto の変更は、次回ビルド時に自動的に反映される。追加の操作は不要。

### まとめ

| 項目 | Go (`api/gen/`) | Rust (`build.rs` / `OUT_DIR`) |
|---|---|---|
| 再生成タイミング | `buf generate`（`make proto`）を実行したときのみ | `cargo build` のたびに自動 |
| リポジトリへのコミット | しない（gitignore 対象） | しない（gitignore 対象） |
| proto 変更を反映するために必要な操作 | ビルド・テスト前に `make proto` を実行する | ビルドするだけ（自動） |

モノレポ構成により、proto 変更と対応する Go/Rust 側の更新を 1 コミットにまとめることができる（ADR-0001 参照）。
