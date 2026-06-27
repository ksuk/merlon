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
| `MERLON_JWT_SECRET` | API 認証用の署名シークレット | 空（必須） | 32 バイト以上のランダム値 |
| `MERLON_CONFIG_PATH` | `config.yaml` のパス | `/etc/merlon/config.yaml` | 環境に応じて |
| `MERLON_AUDIT_WORM` | 監査ログの WORM モード（Enterprise） | `false` | `true` |

`MERLON_JWT_SECRET` が未設定の場合、API は起動を拒否する（Secure by Default 原則）。

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
