---
title: デプロイ運用手順
---

# デプロイ運用手順

本番起動前に migration role でマイグレーションと `make audit-harden` を適用し、serving role がアプリケーション表の owner ではなく、監査証跡を変更できないことを検証する。検証に失敗した場合、API は起動しない。

## 適用範囲

リポジトリ内の Docker Compose ファイルは開発・デモ用のトポロジーであり、本番環境の堅牢化ガイドではない。本番デプロイには、管理されたシークレット、TLS 終端、バックアップ・リストア手順、ネットワークセグメンテーション、及び組織固有の規制対応管理が必要である。

## 本番環境で必須の管理策

- `MERLON_ENV=production` を設定し、認証を有効化する。
- 外部 TLS は信頼できるイングレスまたはリバースプロキシで終端し、配備全体のレート制限もそこで実施する。API のメモリ内リミッターはプロセス単位の補助防御としてのみ扱う。
- Ingress で `X-Forwarded-For` を上書きまたは安全に追記し、その狭い送信元 CIDR のみを `MERLON_TRUSTED_PROXY_CIDRS` に指定する。これにより、監査記録とアプリ側リミッターが観測済みのクライアントアドレスを使用する。
- データベースパスワード、ブートストラップトークン、JWT 材料、暗号鍵はシークレットマネージャに保存し、コミットしたり本番用の値を `.env` ファイルに配置したりしないこと。
- ローカル開発環境以外では `MERLON_SEED=false` を維持する。
- PostgreSQL と API の `/metrics` は、プライベートネットワークまたは認証済みの監視基盤に限定する。
- `MERLON_ENCRYPTION_KEY_RING` をバックアップし、復旧手順がデータベースデータと必要な鍵材料の両方をリストアできることを検証する。
- API の起動前に `MERLON_MIGRATION_DATABASE_URL` で `make migrate`、続いて `make audit-harden` を実行する。serving role はアプリケーション表を所有してはならず、database-level `CREATE` も保持してはならない。

## 設定検証

ロールアウト前に、起動ログからネイティブエンジン設定ダイジェストを記録し、承認済みのルールファイルをリリース証跡とともに保管する。ADR-0012 を参照。

## アプリケーションのデータベースロール

マイグレーション後に、migration owner として `docs/operations/audit-hardening.sql` を適用する。この手順は serving role の権限を、通常のアプリケーション表では CRUD、追記専用の監査証跡では `SELECT`／`INSERT`、migration ledger と schema DDL では権限なしに正規化する。database-level `CREATE` は拒否し、継承したロールメンバーシップから禁止権限を得ている場合も fail closed となる。serving role はアプリケーション表を所有してはならない。

本番の API は監査ログの preflight に失敗すると起動を拒否する。読み取り専用の監査アクセスは組織が管理する別ロールへ付与すること。serving-role 手順はそのロールを作成・復元しない。両方の権限を検証し、その出力をデプロイ証跡として保管する。
