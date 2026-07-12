# Development Environment Setup

Merlon の開発環境構築手順。DevContainer（推奨）とローカル環境の 2 通りを示す。

## 方法 1: DevContainer（推奨）

[VS Code](https://code.visualstudio.com/) と [Dev Containers 拡張機能](https://marketplace.visualstudio.com/items?itemName=ms-vscode-remote.remote-containers)、Docker があれば、ツールチェーン一式が自動で揃う。

1. リポジトリを VS Code で開く
2. コマンドパレット（`F1`）→ **Dev Containers: Reopen in Container** を実行
3. 初回はイメージのビルドに数分かかる。完了後、Go / Rust / Node.js / buf に加え、`psql`（PostgreSQL クライアント）、`gh`（GitHub CLI）、`claude`（Claude Code CLI）、`codex`（OpenAI Codex CLI）、`wrangler`（Cloudflare CLI）もすべて利用可能になる

設定は `.devcontainer/devcontainer.json` と `.devcontainer/Dockerfile` を参照。

## 方法 2: ローカル環境

DevContainer を使わない場合、以下のツールを個別にインストールする。

### Go 1.25+

```bash
# https://go.dev/dl/ から取得、またはパッケージマネージャ
go version   # go1.22 以上を確認
```

### Rust（rustup 経由）

```bash
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
rustc --version   # stable / 2024 edition 対応版
```

### Node.js 20+ / npm

```bash
# https://nodejs.org/ から LTS を取得、または nvm
node --version   # v20 以上
npm --version
```

### buf CLI

```bash
npm install -g @bufbuild/buf
# または https://buf.build/docs/installation
buf --version
```

### PostgreSQL 16+

Docker での起動を推奨。

```bash
docker run -d --name merlon-db \
  -e POSTGRES_USER=merlon \
  -e POSTGRES_PASSWORD=merlon \
  -e POSTGRES_DB=merlon \
  -p 5432:5432 \
  postgres:16
```

## 初回セットアップ手順

```bash
# 1. 環境変数
cp .env.example .env

# 2. Proto コード生成
make proto

# 3. 依存関係の取得
cd api && go mod download && cd ..
cd engine && cargo fetch && cd ..
cd ui && npm install && cd ..

# 4. DB マイグレーション
make migrate

# 5. デモデータ投入（任意）
make seed
```

## make コマンド一覧

| コマンド | 説明 |
|---|---|
| `make proto` | Proto から Go/Rust コードを生成（`buf lint` + `buf generate`） |
| `make build` | 全コンポーネントをビルド |
| `make test` | 全テストを実行（Go / Rust / UI / Proto lint） |
| `make lint` | 全リンターを実行 |
| `make migrate` | DB マイグレーションを適用 |
| `make seed` | デモデータを投入（`deploy/seed/seed.sql`） |
| `make run` | ローカルで全コンポーネントを起動 |
| `make clean` | ビルド成果物を削除 |

詳細は [testing.md](testing.md)、[proto-workflow.md](proto-workflow.md) を参照。
