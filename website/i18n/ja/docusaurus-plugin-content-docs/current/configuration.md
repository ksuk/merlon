---
title: 設定リファレンス
sidebar_position: 3
---

# 設定リファレンス

schema/object-owner操作には`MERLON_MIGRATION_DATABASE_URL`、定期backupには専用read-only roleの`MERLON_BACKUP_DATABASE_URL`を使用し、APIの最小権限接続から分離する。既存databaseを台帳へ登録する場合だけ`MERLON_MIGRATION_BASELINE`を明示すること。

Merlon は環境変数で設定する。ローカル開発では `.env.example` を `.env` にコピーする。そこに含まれる認証情報やシークレットを本番環境で使用してはならない。

## 環境変数

| 変数 | デフォルト | 本番運用ガイダンス |
|---|---|---|
| `MERLON_ENV` | `development` | `production` に設定する。 |
| `MERLON_HTTP_ADDR` | `:8080` | TLS 終端を行うリバースプロキシの背後でバインドする。 |
| `MERLON_DATABASE_URL` | 未設定 | TLS（`sslmode=require` 以上）と最小権限のアプリケーションロールを使用する。 |
| `MERLON_BACKUP_DATABASE_URL` | 未設定 | `make backup` だけで使用する専用の read-only backup 接続。文書化された既存・将来の table／sequence 読取権限を付与し、serving-role URL や schema-owner URL で代用しない。 |
| `MERLON_MIGRATION_DATABASE_URL` | 未設定 | `make migrate`、`make restore`、`make audit-harden` で使用する、分離されたschema/object-owner接続。このroleはtargetの`public` schemaを管理し、そこで`CREATE`を持つ必要がある。別roleがfresh restore databaseを所有する場合、そのownerは`public`をこのroleへ移譲し、このroleとapplication roleの両方へdatabaseのdirect `CONNECT`を事前付与する。serving-role URLで代用しない。 |
| `MERLON_MIGRATIONS_DIR` | `migrations` | マイグレーションコマンド専用。バージョン付き SQL マイグレーションを格納するディレクトリ。`--migrations-dir` フラグを指定した場合はその値を優先する。 |
| `MERLON_ENCRYPTION_KEY_RING` | 未設定 | 本番環境の PII 保護に必須。`merlon-keyrotate` が受け付ける文書化されたキーリング形式を使用する。参照されているすべての鍵を失うと、過去の暗号化された値は復元不能になる。鍵は保護された KMS またはシークレットマネージャーでバックアップする。 |
| `MERLON_INBOUND_WEBHOOK_SECRET` | 未設定 | 顧客・取引 push webhook の durable HMAC シークレット。シークレットマネージャーで管理し、未設定時は inbound エンドポイントがリクエストを拒否する。 |
| `MERLON_JWT_PRIVATE_KEY_FILE` / `MERLON_JWT_PUBLIC_KEY_FILE` | 未設定 | ローカルユーザー認証には RS256 鍵ペアを使用する。 |
| `MERLON_JWT_SECRET` | 未設定 | 開発用フォールバックのみ。ローカルユーザー認証を使用する場合、本番環境では設定しないこと。 |
| `MERLON_BOOTSTRAP_TOKEN` | 未設定 | 初回セットアップ用のワンタイムシークレット。最初の管理者・API キー作成後、直ちにローテーションまたは削除する。 |
| `MERLON_POSTGRES_PASSWORD` | 未設定 | Compose 専用の開発用パスワード。本番環境ではシークレットマネージャーを使用する。 |
| `MERLON_AUTH_ENABLED` | `false` | 本番環境では `true` が必須。 |
| `MERLON_SEED` | `false` | 開発・デモデータ専用。本番環境では必ず `false` にする。 |
| `MERLON_DEMO_DATA_DIR` | 未設定 | 生成済みデモデータセットを含むディレクトリ。`MERLON_SEED` 有効時に読み込む。内容が不完全な場合は内蔵サンプルにフォールバックする。開発・デモ専用。 |
| `MERLON_CONFIG_PATH` | `config.yaml` | アプリケーション設定へのパス。 |
| `MERLON_CACHE_BACKEND` | `memory` | 使用するキャッシュバックエンドを選択する。 |
| `MERLON_EVENT_BUS` | `pg_notify` | PostgreSQL と併用するイベントバスドライバ。 |
| `MERLON_RATE_LIMIT` | `0` | 解決済みクライアント IP ごとの、任意のプロセス単位・1分あたりリクエスト数。補助防御としてのみ使用し、配備全体の制限は信頼済み Ingress で実施する。本番で非ゼロにする場合は `MERLON_TRUSTED_PROXY_CIDRS` が必須。 |
| `MERLON_TRUSTED_PROXY_CIDRS` | 未設定 | `X-Forwarded-For` の供給を許可するリバースプロキシの狭い CIDR をカンマ区切りで指定する。未信頼 peer または信頼側 hop の不正値は直接 peer アドレスへフォールバックし、`/0` は拒否する。監査記録に元のクライアント IP が必要な場合は、アプリ内制限が無効でも設定する。 |
| `MERLON_ADAPTER_CONFIG_PATH` | 未設定 | 運用担当者が管理するアダプタ設定へのパス。 |
| `MERLON_UI_DIR` | 未設定 | ビルド済み UI を含むディレクトリ（任意）。 |
| `MERLON_SCREENING_IMPORT_ENABLED` | `false` | 承認済みエンドポイントの場合のみ、外部制裁リストのインポートを有効化する。 |
| `MERLON_SCREENING_RESCREEN_ENABLED` | `false` | 定期的な顧客再スクリーニングを有効化する。 |
| `MERLON_SCREENING_IMPORT_INTERVAL` | `24h` | スクリーニングリストのインポート間隔。 |
| `MERLON_SCREENING_CHECK_INTERVAL` | `1h` | 再スクリーニングの間隔。 |
| `MERLON_SCREENING_OFAC_URL` / `MERLON_SCREENING_EU_URL` | 未設定 | 承認済みの OFAC/EU リストエンドポイント。 |
| `MERLON_SCREENING_UN_URL` / `MERLON_SCREENING_MOF_URL` | 未設定 | 承認済みの UN/MOF リストエンドポイント。 |
| `MERLON_SCREENING_PEP_URL` | 未設定 | 承認済みの PEP プロバイダーエンドポイント。 |
| `MERLON_SCREENING_THRESHOLD` | エンジン既定値 | 氏名照合スコアの閾値（`0`〜`1`）。この値以上でスクリーニングヒットとする。下げると再現率とレビュー件数が増える。範囲外の値やパースできない値は無視され、既定値が維持される。検知感度を変更するため、ルール変更と同等のレビューを経ること。 |
| `MERLON_SMTP_HOST` / `MERLON_SMTP_PORT` | 未設定 / `587` | 通知用の SMTP エンドポイント。TLS と認証情報を設定する。 |
| `MERLON_SMTP_USERNAME` / `MERLON_SMTP_PASSWORD` | 未設定 | SMTP 認証情報。シークレットマネージャーを使用する。 |
| `MERLON_SMTP_FROM` / `MERLON_SMTP_TO` | 未設定 | 通知の送信元と、カンマ区切りの宛先一覧。 |
| `MERLON_SMTP_USE_TLS` | `false` | SMTP の TLS を有効化する。 |
| `MERLON_NOTIFY_ROUTING_PATH` | 未設定 | 通知ルーティング用 YAML のパス（任意）。 |
| `MERLON_PUBLIC_URL` | 未設定 | 通知リンクで使用する公開ベース URL。 |
| `MERLON_WHITELIST_MAX_VALID_DAYS` | `365` | ホワイトリストの最大有効期間。 |
| `MERLON_EDD_STAGE2_DAYS` / `MERLON_EDD_STAGE3_DAYS` | `60` / `90` | EDD エスカレーションの閾値。 |
| `MERLON_TM_SCENARIOS_PATH` | `tm_scenarios` | ディレクトリを管理下のソース管理に保管する。実行時ダイジェストは読み込まれた内容を識別するが、変更を承認するものではない。 |
| `MERLON_OPERATOR_TEAMS` | 未設定 | 永続的な割当チーム一覧をカンマ区切りで指定する。キュー行を走査してチームを推測することはなく、認証済みの割当には設定値が必要となる。 |
| `MERLON_CASE_PRIORITY_PATH` | `content/case_priority_v1.yaml` | 保存された CDD ティア／スコアからケース優先度への対応を定義するバージョン付き YAML。 |
| `MERLON_KYC_REQUIRED_FIELDS_PATH` | `content/kyc_required_fields_v1.yaml` | 顧客タイプごとに必須となる本人特定事項を定義するバージョン付き YAML。`enforcement: warn` は不足を報告しつつ登録を受理し、`reject` は拒否する。 |
| `MERLON_EDD_POLICY_PATH` | `content/edd_policy_v1.yaml` | EDD の段階スケジュール・期限・完了要件・リスク等級降格時の挙動を保持するバージョン付き YAML。`MERLON_EDD_STAGE2_DAYS` / `MERLON_EDD_STAGE3_DAYS` を置き換える。 |
| `MERLON_CDD_RULE_SELECTION_PATH` | `content/cdd_rule_selection_v1.yaml` | 顧客タイプ・商品・法域から適用 CDD ルールセットへの対応を定義するバージョン付き YAML。ルールセットの選択が一覧順ではなく設定によって決まる。 |
| `MERLON_TRAVEL_RULE_POLICY_PATH` | `content/travel_rule_v1.yaml` | トラベルルールの閾値・対象取引・必要証跡・適用対象外理由コード、およびクライアントの矛盾する主張を記録するか拒否するかを保持するバージョン付き YAML。 |
| `MERLON_SCREENING_READINESS_PATH` | `content/screening_readiness_v1.yaml` | 期待するリストソース・鮮度ウィンドウと、必須ソースが未整備のとき実行を degraded として記録するか停止するかを定義するバージョン付き YAML。 |
| `MERLON_TM_BATCH_SCHEDULE` | `02:00` | 取引モニタリングのバッチ評価を実行する日次時刻（`HH:MM`）。 |
| `MERLON_TM_BATCH_TIMEZONE` | ローカルタイムゾーン | 本番環境では IANA タイムゾーンを明示的に設定する。 |
| `MERLON_LOG_LEVEL` | `info` | `info` 以上を維持する。機密性の高いワークロードに debug ログを使用しないこと。 |

ネイティブ Go エンジンは、運用担当者が指定したパスから CDD ウェイトとスクリーニングコンテンツも読み込む。これらのファイルはデータベースの監査証跡の対象外である。ソース管理、変更承認、アクセス制御、バックアップ、デプロイメント手順によって管理すること。ADR-0012 を参照。

## 暗号鍵のローテーション

`merlon-keyrotate` は `--key-ring-env`（デフォルト `MERLON_ENCRYPTION_KEY_RING`）を受け付け、キーリング仕様を含む環境変数を選択する。既存データの復号に必要な旧鍵を保持したまま新しいアクティブ鍵を導入し、更新後のリングをデプロイし、移行を完了してから、初めて旧鍵を退役させる。バックアップに依拠する前に、アプリケーションデータベースと鍵材料の両方の復元テストを行うこと。

## 運用上の閾値

サンプルの TM・スクリーニングルールは例であり、本番環境のデフォルト値ではない。閾値は導入組織のリスク評価に基づいて選択し、変更をテストし、ルールガバナンスプロセスを通じて承認すること。特に、スクリーニングの一致閾値を下げると誤検知（false positive）が増加し、上げると見逃し（missed match）リスクが増加する。フェイルアラートの原則の下、不確実性がある場合はアラートを抑制する方向ではなく、追加レビューを行う方向で解消すること。
