---
title: アダプタガイド
sidebar_position: 5
---

# アダプタガイド

本ガイドは、外部のコアバンキングシステム、ウォレット、取引処理システムを Merlon に接続する開発者向けである。アダプタとは何か、アダプタ設定の読み書き方法、取り込まれたデータが CDD スコアリングに到達するまでの流れを扱う。

## アダプタとは

Merlon の Go API はシステム固有の連携コードを組み込まない。代わりに、**アダプタ分離**（Adapter Isolation）の設計原則（[アーキテクチャ](./architecture.md)参照）により、フィールド名・認証方式・ページネーションといった外部システム固有の差異はすべて、ドメインロジック、HTTP ハンドラ、評価エンジンの実装に漏出させず、専用のアダプタ層（リポジトリ内の `api/internal/adapter/`）で吸収することが求められる。

アダプタはコーディングするものではなく設定するものである。外部の REST API を YAML ファイルで記述すれば、アダプタ層がその記述を用いて顧客・取引レコードを取得し、Merlon の内部形式に正規化する。これにより、カスタムの連携ロジックはアプリケーションバイナリの外部かつ BSL ライセンスのコードベース外に保たれ、**設定としてのプロダクト**（Configuration as the Product）原則に沿ったものとなる。

アダプタ設定ファイルのパスを `MERLON_ADAPTER_CONFIG_PATH` に設定する。API が環境変数をどのように読み込むかは[設定リファレンス](./configuration.md)を、以下で説明する送信ネットワークセキュリティ制御については `config.example.yaml` の `adapters.outbound_allowlist` / `adapters.block_private_ip_ranges` を参照。

## アダプタ設定の構造

リポジトリには実例として `adapters/example_core_banking.yaml` が同梱されている。これをコピーして環境に合わせて調整すること。以下の各フィールドはアダプタローダー（`api/internal/adapter/config.go`）によって読み込まれる。

```yaml
type: rest
base_url: https://core.example.com/api/v1
timeout_seconds: 30

auth:
  type: bearer
  token_env: CORE_API_TOKEN

endpoints:
  fetch_customer:
    method: GET
    path: /customers/{id}
    field_mapping:
      external_id: "$.customer_id"
      name: "$.full_name"
      country: "$.address.country_code"
      customer_type: "$.type"

  fetch_transactions:
    method: GET
    path: /transactions
    params:
      account_id: "{account_id}"
      since: "{last_sync_timestamp}"
    response_root: "$.transactions"
    field_mapping:
      external_id: "$.txn_id"
      amount: "$.amount"
      currency: "$.currency"
      type: "$.transaction_type"
      base_currency_equivalent: "$.base_currency_equivalent"
```

### トップレベルのフィールド

| フィールド | 意味 |
|---|---|
| `type` | アダプタのトランスポート種別。現在サポートされるのは `rest` のみで、それ以外の値は検証に失敗する。 |
| `base_url` | 外部システムのベース URL。有効な URL としてパースできる必要がある。 |
| `timeout_seconds` | リクエストごとの HTTP タイムアウト。未設定または非正の値の場合、デフォルトは `30`。 |
| `auth` | 認証設定。詳細は後述。 |
| `endpoints` | 名前付きエンドポイントのマップ。少なくとも1つが必須。 |

### 認証

`auth.type` は `api/internal/adapter/config.go` で検証される4つのモードのいずれかを選択する。

| `auth.type` | 必須フィールド | 動作 |
|---|---|---|
| `none` | — | `Authorization` ヘッダーは送信されない。 |
| `bearer` | `token_env` | 指定された環境変数からベアラートークンを読み取り、`Authorization: Bearer <token>` を送信する。 |
| `basic` | `username_env`, `password_env` | 環境変数からユーザー名・パスワードの組を読み取り、HTTP Basic 認証を送信する。 |
| `header` | `header_name`, `header_val_env` | 指定された環境変数から値を読み取り、カスタムヘッダー名の下に送信する。 |

認証情報の**値そのもの**はアダプタの YAML ファイルには一切書き込まれず、それを保持する環境変数の名前のみが書き込まれる。これにより、シークレットをソース管理の外に保ちつつ、「このアダプタがどのシークレットを使用するか」というマッピングをレビュー可能なバージョン管理下のファイルとして保持できる。

### エンドポイントとフィールドマッピング

`endpoints` 配下の各エントリは名前付きの操作である。アダプタ層は定義済みの契約を持つ2つのエンドポイント名、`fetch_customer` と `fetch_transactions` を認識する。

- `method` / `path` — HTTP メソッドと URL パス。`path` にはパスパラメータから置換される `{param}` プレースホルダーを含められる（例: `fetch_customer` の `{id}`）。
- `params` — クエリ文字列パラメータ。各値は、フェッチ呼び出しに渡されるパラメータから置換される `{param}` プレースホルダーを含みうるテンプレートである（例: `fetch_transactions` の `{account_id}` と `{last_sync_timestamp}`）。
- `response_root` — 反復対象の配列を特定する、JSON レスポンス内への `$.` 接頭辞付きドットパス。取引レスポンスはリストであるため `fetch_transactions` では必須。`fetch_customer` はレスポンスルートに単一の JSON オブジェクトを期待し、このフィールドは使用しない。
- `field_mapping` — Merlon の内部フィールド名から、（各）レスポンスオブジェクト内への `$.` 接頭辞付きドットパスへのマップ。エンドポイントごとに少なくとも1つのマッピングが必須。

フィールド抽出（`api/internal/adapter/fieldmap.go`）は、デコードされた JSON オブジェクトをドットパスに沿ってセグメントごとにたどる。キーが存在しない、あるいはパスがオブジェクトでない値を通過するなど、パスが解決できない場合は、フェッチ全体を失敗させるのではなく、単にそのフィールドを結果から省略する。導入先が依存するフィールドが、外部システムから確実に返されるフィールドでもあるように、マッピングを設計すること。

`fetch_customer` について認識される内部フィールドは `external_id`、`name`、`country`、`customer_type` である。`fetch_transactions` については `external_id`、`amount`、`currency`、`type` である。（サンプルの `base_currency_equivalent` のような）その他のマッピング済みフィールドは、破棄されることなく、認識済みフィールドと並んで raw フィールドマップに保持される。

## 送信ネットワークセキュリティ

アダプタは設定先のシステムへ送信 HTTP 呼び出しを行うため、アダプタ層は対象システムが強制する内容とは独立に、自身のネットワーク制御を適用する（`api/internal/adapter/security.go`）。

- `https` 以外の URL は、開発用に明示的に許可されない限り拒否される。
- `adapters.outbound_allowlist`（`SecurityConfig.OutboundAllowlist`）が空でない場合、リストに含まれるホスト名のみが許可される。
- `adapters.block_private_ip_ranges` が有効な場合、対象ホスト名が直接または DNS 経由でループバック、プライベート、リンクローカル、または未指定の IP アドレスに解決されるとき、リクエストは拒否される。この確認は設定検証時だけでなく接続時（`newSafeTransport`）にも再適用され、DNS リバインディングに耐性を持たせている。

これらはデプロイ設定の `config.yaml` の `adapters:` セクション配下に設定する（形式は `config.example.yaml` を参照）。アダプタの接続先が完全には信頼できないデプロイにおいては、`block_private_ip_ranges` を必須として扱うこと。

## データフロー: アダプタからスコアリングへ

設定が完了すると、アダプタの `fetch_customer` および `fetch_transactions` 操作は正規化された `CustomerData` / `TransactionData` の値（`api/internal/adapter/adapter.go`）を返す。そこから先、Merlon の他部分への経路は[アーキテクチャ](./architecture.md)に記載されたアーキテクチャに従う。

1. 取り込みプロセスがアダプタを呼び出して外部システムからレコードを取得し、上記のフィールドマッピングを用いて正規化する。
2. 正規化されたレコードは Go API の REST エンドポイント（顧客・取引の作成/更新）に送信され、永続化とドメイン検証が行われる。
3. Go API はインプロセスの評価エンジンを呼び出し、新たに取り込まれたデータに対して CDD スコアを計算・更新し、トランザクションモニタリングのシナリオを評価する。これは ADR-0004（Score-Driven Architecture、[アーキテクチャ](./architecture.md)参照）に記載された、CDD スコアと TM しきい値の間のスコア駆動の関係に従う。

顧客・取引レコードの REST スキーマについては[API リファレンス](./api/openapi.md)を参照。評価エンジンの契約は Go プロセス内部で管理される。

## アダプタの検証とテスト

アダプタを本番システムに向ける前に:

1. **設定構造を検証する。** `AdapterConfig.Validate()` は、`type` が `rest` であること、`base_url` がパース可能であること、`auth` がその種別に必要なフィールドを持つこと、すべてのエンドポイントが `method`、`path`、及び少なくとも1つの `field_mapping` エントリを持つことを確認する。
2. **接続性のドライランを行う。** アダプタ層の `DryRun` 操作（`api/internal/adapter/dryrun.go`）は、設定の妥当性、パース済み `base_url` ホストへの TCP 到達性の順に確認し、設定済みエンドポイントごとに1つのステータスを報告する。認証プロバイダを構築できることの確認を超えて、実システムに対する認証を実行するわけではない。ドライランの成功は接続性・設定チェックとして扱い、機能保証としては扱わないこと。
3. **実際のペイロードに対してフィールドマッピングを検証する。** 外部システムからサンプルレスポンスを取得し、想定される内部フィールドが（特にネストしたフィールドやオプションフィールドについて）正しく抽出されることを確認する。

## 運用上の注意点

- **冪等性**: アダプタ層自体はレコードの重複排除を行わず、各呼び出しについて外部システムのレスポンスに含まれる内容をそのまま返す。外部システムがポーリング間で重複のない結果セットを保証しない場合（例えば、クロックスキューのある `since` ウィンドウ）、アダプタ自体が重複排除を行うと想定するのではなく、顧客・取引エンドポイントにおける Merlon API 自体の外部 ID 処理に頼って重複レコードを回避すること。
- **エラー処理**: アダプタの呼び出しはフェイルクローズである。2xx 以外の HTTP レスポンス、JSON デコードの失敗、必須の `response_root` の欠如、送信セキュリティチェックに失敗した URL は、いずれも部分的なデータではなくエラーを返す（`api/internal/adapter/rest.go`）。レスポンスボディは読み込み時に 10 MiB に上限が設定され、エラーメッセージにはレスポンスボディの切り詰められたプレビューのみが含まれ、大きなペイロードや機密情報がログに漏出することを防ぐ。
- **タイムアウト**: すべての HTTP リクエストは `timeout_seconds`（デフォルト `30`）の対象となる。外部システムの想定レイテンシに応じて設定すること。タイムアウトを超えたフェッチは、部分的な結果としてではなくリクエストエラーとして失敗する。
