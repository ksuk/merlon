# Configuration Reference

Merlon の設定は環境変数と `config.yaml` の二層構成である。環境変数はデプロイ環境ごとの差異（接続先・シークレット）を、`config.yaml` はアプリケーションの振る舞いを定義する。

## 環境変数

`.env.example` をコピーして `.env` を作成する。

| 変数名 | 説明 | デフォルト値 | 本番推奨 |
|---|---|---|---|
| `MERLON_ENV` | 実行環境（`development` / `staging` / `production`） | `development` | `production` |
| `MERLON_API_PORT` | Go API のリッスンポート | `8080` | `8080`（背後にリバースプロキシ） |
| `MERLON_ENGINE_ADDR` | Rust Engine の gRPC アドレス | `engine:50051` | 内部ネットワーク限定 |
| `MERLON_DATABASE_URL` | PostgreSQL 接続文字列 | `postgres://merlon:merlon@db:5432/merlon?sslmode=disable` | `sslmode=require` 必須 |
| `MERLON_REDIS_URL` | Redis 接続文字列（オプション） | 空（無効） | キャッシュ利用時に設定 |
| `MERLON_OBJECT_STORE_URL` | レポート成果物の保存先 | `file:///var/lib/merlon/objects` | S3 互換ストレージ推奨 |
| `MERLON_LOG_LEVEL` | ログレベル（`debug` / `info` / `warn` / `error`） | `info` | `info` |
| `MERLON_JWT_PRIVATE_KEY_FILE` | JWT アクセストークン署名用 RSA 秘密鍵ファイルのパス（PEM、PKCS1/PKCS8） | 空 | 必須（RS256、auth.md §2） |
| `MERLON_JWT_PUBLIC_KEY_FILE` | JWT 検証用 RSA 公開鍵ファイルのパス（PEM、PKIX） | 空 | 必須（RS256、auth.md §2） |
| `MERLON_JWT_SECRET` | **非推奨・開発専用。** RS256 鍵ペア（`MERLON_JWT_PRIVATE_KEY_FILE`/`MERLON_JWT_PUBLIC_KEY_FILE`）が未設定の場合のみ、HS256 の暫定署名シークレットとして使用される（auth.md §2.5「現行実装からの移行」） | 空 | 本番では未設定とし、RS256 鍵ペアを使用すること |
| `MERLON_BOOTSTRAP_TOKEN` | 初期セットアップ完了前に限り、最初の APIキーを発行できるブートストラップトークン。初期セットアップ完了後（`users` テーブルに1件以上存在、または既に APIキーが1件以上存在）は自動的に無効化される（AUTH-006） | 空 | ワンタイムで払い出し、使用後は破棄 |
| `MERLON_TM_SCENARIOS_PATH` | TM シナリオ YAML ディレクトリ | `tm_scenarios` | 環境に応じて |
| `MERLON_CONFIG_PATH` | `config.yaml` のパス | `/etc/merlon/config.yaml` | 環境に応じて |
| `MERLON_AUDIT_WORM` | 監査ログの WORM モード（Enterprise） | `false` | `true` |

JWT 署名鍵（`MERLON_JWT_PRIVATE_KEY_FILE` / `MERLON_JWT_PUBLIC_KEY_FILE`）と `MERLON_JWT_SECRET` がいずれも未設定の場合、API 自体は起動するが、ローカルユーザ認証（メール+パスワードのログイン、`POST /api/v1/auth/login` 等）は無効化される。既存の APIキー認証（M2M、AUTH-006）には影響しない。

## config.yaml

アプリケーションの振る舞いをセクション別に定義する。

### `scoring`
CDD スコアリングの全体設定。

```yaml
scoring:
  active_weight_id: cdd_basic_weights   # 適用する CDD ウェイト定義の ID
  review_interval_days: 365             # 定期見直しの間隔
  recalculate_on_transaction: true      # 取引発生時に再計算するか
```

### `monitoring`
取引モニタリング（TM）の設定。

```yaml
monitoring:
  enabled_scenarios:                    # 有効化する TM シナリオの ID 列
    - tm_structuring_basic
  alert_retention_days: 2555            # アラート保持期間（7年 = 2555日）
```

### `screening`
制裁対象者・PEP スクリーニングの設定。

```yaml
screening:
  lists:                                # 参照する制裁リスト
    - mof_japan
  match_threshold: 0.85                 # 名寄せの一致閾値（0.0–1.0）
  rescreen_interval_days: 30            # 再スクリーニング間隔
```

### `audit`
監査ログの設定（Auditability First 原則）。

```yaml
audit:
  enabled: true                         # 常時 true（無効化不可）
  retention_days: 2555                  # 7年
  worm: false                           # Enterprise WORM 監査ログ
```

### `case`
ケース管理ワークフローの設定。

```yaml
case:
  auto_assign: false                    # アラートからのケース自動割当
  sla_hours: 72                         # ケース対応のSLA時間
```

## デフォルト値と本番推奨値

| 項目 | デフォルト | 本番推奨 | 理由 |
|---|---|---|---|
| `sslmode` | `disable` | `require` | DB 通信の暗号化 |
| `MERLON_LOG_LEVEL` | `info` | `info` | `debug` は機微情報漏洩リスク |
| `audit.worm` | `false` | `true` | 監査ログの改竄防止 |
| `screening.match_threshold` | `0.85` | リスク許容度に応じ調整 | 低くすると誤検知増、高くすると見逃しリスク |

Fail-Alert 原則により、スクリーニングや TM の閾値設定は迷う場合はアラート側（より多く検知する側）に倒す。
