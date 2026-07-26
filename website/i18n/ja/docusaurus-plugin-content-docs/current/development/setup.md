---
title: 開発環境構築
---

# 開発環境構築

データベースマイグレーションは checksum ledger を使用する `MERLON_MIGRATION_DATABASE_URL` で実行する。ローカル以外では API role と migration role を分離する。

Merlon の開発環境構築手順。DevContainer（推奨）とローカル環境の 2 通りを示す。

## 方法 1: DevContainer（推奨）

[VS Code](https://code.visualstudio.com/) と [Dev Containers 拡張機能](https://marketplace.visualstudio.com/items?itemName=ms-vscode-remote.remote-containers)、Docker があれば、ツールチェーン一式が自動で揃う。

1. リポジトリを VS Code で開く
2. コマンドパレット（`F1`）→ **Dev Containers: Reopen in Container** を実行
3. 初回はイメージのビルドに数分かかる。完了後、Go / Node.js に加え、`psql`（PostgreSQL クライアント）、`gh`（GitHub CLI）、`claude`（Claude Code CLI）、`codex`（OpenAI Codex CLI）、`wrangler`（Cloudflare CLI）もすべて利用可能になる

設定は `.devcontainer/devcontainer.json` と `.devcontainer/Dockerfile` を参照。

## 方法 2: ローカル環境

DevContainer を使わない場合、以下のツールを個別にインストールする。

### Go 1.25+

```bash
# https://go.dev/dl/ から取得、またはパッケージマネージャ
go version   # go1.25 以上を確認
```

### Node.js 20+ / npm

```bash
# https://nodejs.org/ から LTS を取得、または nvm
node --version   # v20 以上
npm --version
```

### PostgreSQL 18+

Docker での起動を推奨。

```bash
docker run -d --name merlon-db \
  -e POSTGRES_USER=merlon \
  -e POSTGRES_PASSWORD=merlon \
  -e POSTGRES_DB=merlon \
  -p 5432:5432 \
  postgres:18
```

## 初回セットアップ手順

```bash
# 1. 環境変数
cp .env.example .env

# 2. 依存関係の取得
cd api && go mod download && cd ..
cd ui && npm install && cd ..

# 3. DB マイグレーション
make migrate

# 4. デモデータ投入（任意）
make seed
```

## make コマンド一覧

| コマンド | 説明 |
|---|---|
| `make build` | Go API と UI をビルド |
| `make test` | 全テストを実行（Go / UI） |
| `make lint` | 全リンターを実行 |
| `make fmt` | 全コードをフォーマット（Go、UI） |
| `make migrate` | `MERLON_DATABASE_URL` を使って DB マイグレーションを適用 |
| `make seed` | デモデータ付きでフル構成の docker-compose トポロジーを起動（`MERLON_SEED=true docker compose up --build`） |
| `make dev-up` / `make dev-down` | 開発用トポロジー（`docker-compose.yml` + `docker-compose.dev.yml`）を起動／停止 |
| `make minimal-up` / `make minimal-down` | 最小トポロジー（PostgreSQL + API のみ）を起動／停止 |
| `make generate-openapi` | OpenAPI 仕様を `docs/api/openapi.json` にエクスポート |

すでに起動中の PostgreSQL インスタンスに、compose トポロジー全体を起動せずにデモデータを投入するには、`scripts/seed-demo.sh` を実行する。このスクリプトは `psql` 経由で `deploy/seed/legacy/seed.sql` を読み込む。このファイルは現行スキーマと不一致（`deploy/seed/legacy/README.md` 参照）のため、動作するデモデータには `make seed`（`MERLON_SEED=true`）を推奨する。

詳細は [テストガイド](testing.md) を参照。
