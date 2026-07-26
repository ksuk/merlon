# Helm Templates

**このチャートはまだ動作しない。** 本ディレクトリにテンプレートは1つも無く、`helm install` は何もデプロイしない。`values.yaml` は将来のテンプレートが参照する値の形を先に定めたものである。

Kubernetes へ導入する場合は、現時点では `ghcr.io/ksuk/merlon` を用いて自組織のマニフェストを作成すること。イメージの前提（非 root 実行、書き込み不要、イメージ組み込みの healthcheck は `/healthz/live`（liveness）、ポート 8080）は [コンテナイメージ](../../../../docs/operations/container-images.md) に記載している。手書きのマニフェストでは、readinessProbe に `/healthz/ready`、livenessProbe に `/healthz/live` を使うこと。

## 今後の追加予定

| ファイル | 役割 |
|---|---|
| `deployment.yaml` | api の Deployment（UI は同一イメージが配信するため単独の Deployment は不要） |
| `service.yaml` | 各コンポーネントの Service |
| `configmap.yaml` | `config.yaml` 等のアプリ設定 |
| `secret.yaml` | `MERLON_JWT_SECRET`・DB 認証情報・`MERLON_ENCRYPTION_KEY_RING` 等のシークレット |
| `ingress.yaml` | 外部公開用 Ingress |
| `_helpers.tpl` | 共通テンプレートヘルパー |
| `serviceaccount.yaml` | 各コンポーネントの ServiceAccount |

マイグレーションは起動時に自動実行されないため、テンプレート追加時には `make migrate` 相当を別ロールで実行する Job（または明示的な運用手順）の設計が必要になる。詳細は[アップグレード](../../../../docs/operations/upgrade.md)を参照。

## 値の参照

各テンプレートは [`../values.yaml`](../values.yaml) の値を参照する。デフォルト値とその意味は `values.yaml` のコメントを参照。
