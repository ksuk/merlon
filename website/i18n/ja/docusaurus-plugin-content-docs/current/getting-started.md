---
title: はじめに
sidebar_position: 1
---

# はじめに

Merlon を最短で起動し、空の状態からオペレーターダッシュボードに到達するまでの手順。

空のシステムではなくデータの入った状態を見たい場合は、[デモツアー](demo-tour.md)から始めるとよい。約1,015件の合成顧客データと98件のアラートが同梱されており、アカウント作成も不要である。

## 前提条件

- [Docker](https://docs.docker.com/get-docker/) 24+
- [Docker Compose](https://docs.docker.com/compose/) v2+
- `git`

ローカルに Go / Node.js をインストールする必要はない。以下はすべてコンテナ内で完結する。

## 1. 起動する

```bash
git clone https://github.com/ksuk/merlon.git
cd merlon
cp .env.example .env
docker compose up --build
```

初回ビルドには数分かかる。Compose はデータベースパスワードとブートストラップトークンを `.env` から読み込むため、2行目のコピーは省略できない。省略すると `set MERLON_POSTGRES_PASSWORD` で起動が停止する。

次の行が出るまで待つ。

```
{"level":"INFO","msg":"merlon-api starting","env":"development","mode":"all","addr":":8080"}
```

## 2. 管理者アカウントを作成する

**[http://localhost:8080](http://localhost:8080)** を開く。

このトポロジーでは認証が有効であり、まだアカウントが1つも存在しないため、ログイン画面のままでは先に進めない。ログインフォームの下にある「管理者アカウントを作成する」から作成するか、[http://localhost:8080/setup](http://localhost:8080/setup) を直接開く。

メールアドレスと12文字以上のパスワードを入力する。このルートはアカウントが存在しない間だけ有効であり、最初の管理者が作成された後はリクエストを拒否する。以降のユーザー追加はアプリケーション内の「ユーザ管理」から行う。

:::note `healthy` はセットアップ完了を意味しない

コンテナのヘルスチェックは `GET /healthz/live` を参照するため、`docker ps` はこの手順の前でも、プロセスが応答した時点で API コンテナを `healthy` と表示する。readiness はこれとは別であり、最初の管理者が作成されるまで `GET /healthz/ready` は `503` を返す。誰もログインできないインスタンスはリクエストを処理できる状態ではないためである。[トラブルシューティング](troubleshooting/index.md)を参照。

:::

## 3. ログインする

作成したアカウントでログインする。顧客一覧が空でアラートも無いダッシュボードが表示されれば正常である。まだ何も投入していないため、これは期待どおりの状態である。

## 次のステップ

| 目的 | 参照先 |
|---|---|
| データが入った状態で製品を見る | [デモツアー](demo-tour.md) |
| 自組織の顧客・取引を投入する | [初期移行](operations/initial-migration.md) |
| スコアとアラートの仕組みを理解する | [アーキテクチャ](architecture.md) |
| ルールを調整する | [ルール作成](rule-authoring.md) |
| 設定を変更する | [設定リファレンス](configuration.md) |
| 実運用する | [デプロイ](operations/deployment.md) |
| うまく動かない | [トラブルシューティング](troubleshooting/index.md) |

## この環境を手元から出す前に

`.env.example` には開発専用の認証情報が含まれており、そのすべてに `MUST change in production` が明記されている。上記の compose ファイルはポート 8080 を全インターフェースで公開する点にも注意すること。

このクイックスタートはローカルマシンでの評価用である。それ以外の場所で Merlon を動かす前に、[デプロイ](operations/deployment.md)と[設定リファレンス](configuration.md)を読むこと。開発用の初期認証情報のまま規制記録を生成するシステムは、後から説明のつくものではない。
