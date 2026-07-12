# Getting Started

Merlon を最短で起動するためのクイックスタートガイド。

## 前提条件

- [Docker](https://docs.docker.com/get-docker/) 24+
- [Docker Compose](https://docs.docker.com/compose/) v2+
- `git`

ローカルに Go / Rust / Node.js をインストールする必要はない。最小構成はすべてコンテナ内で完結する。

## 手順

```bash
# 1. リポジトリを取得
git clone https://github.com/merlon-aml/merlon.git
cd merlon

# 2. 環境変数ファイルを用意
cp .env.example .env

# 3. 最小構成で起動（API + Engine + PostgreSQL）
docker compose -f docker-compose.minimal.yml up --build

# 4. ヘルスチェック（別ターミナルで）
curl localhost:8080/healthz
```

`{"status":"ok"}` が返れば起動成功。

## 次のステップ

- [アーキテクチャ概要](architecture.md) — システム全体の構成を理解する
- [設定リファレンス](configuration.md) — 環境変数と `config.yaml` を調整する
- [開発環境セットアップ](development/setup.md) — コードを編集する開発者向け
- [テスト実行ガイド](development/testing.md) — テストの動かし方
