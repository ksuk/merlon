# Helm Templates

本ディレクトリは Merlon Helm チャートのテンプレートを格納する。M1.1 段階では骨格のみで、テンプレート本体は今後のマイルストーンで追加する。

## 今後の追加予定

| ファイル | 役割 |
|---|---|
| `deployment.yaml` | api / engine / ui の Deployment |
| `service.yaml` | 各コンポーネントの Service |
| `configmap.yaml` | `config.yaml` 等のアプリ設定 |
| `secret.yaml` | `MERLON_JWT_SECRET`・DB 認証情報等のシークレット |
| `ingress.yaml` | 外部公開用 Ingress |
| `_helpers.tpl` | 共通テンプレートヘルパー |
| `serviceaccount.yaml` | 各コンポーネントの ServiceAccount |

## 値の参照

各テンプレートは [`../values.yaml`](../values.yaml) の値を参照する。デフォルト値とその意味は `values.yaml` のコメントを参照。
