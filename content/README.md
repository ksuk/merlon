# Merlon Content

## Production warning

Sample rules and thresholds are public by design and can be used to infer detection behavior. They are not production-ready controls. Replace every sample threshold, scenario, and list with values derived from the deploying institution's documented risk assessment, and keep production values confidential. The sample content exists to demonstrate the configuration format, not to provide regulatory coverage.

本ディレクトリは、Merlon のルール定義コンテンツを格納する。AML/CFT のルール（CDD ウェイト、TM シナリオ等）は、Configuration as the Product 原則に基づき、JSON/YAML の設定として表現される。

## ディレクトリ構成

### `_sample/`
サンプルルール。**Apache-2.0** でライセンスされる（[`_sample/LICENSE`](_sample/LICENSE)）。

- `tm_structuring_basic` — 基本的な分割送金検知シナリオ（[`tm_structuring.json`](_sample/tm_structuring.json)）
- `cdd_basic_weights` — 基本的な CDD リスクウェイト（[`cdd_basic_weights.yaml`](_sample/cdd_basic_weights.yaml)）

サンプルを Apache-2.0 で別ライセンスとするのは、ユーザーが作成するルールのライセンス帰属を明確にするためである（[ADR-0003](../docs/decisions/0003-bsl-license-choice.md) 参照）。

### `schema/`
ルール定義のバリデーション用 JSON Schema。

- [`tm_scenario_v2.json`](schema/tm_scenario_v2.json) — TM シナリオ定義のスキーマ（現行）
- [`cdd_weight_v1.json`](schema/cdd_weight_v1.json) — CDD ウェイト定義のスキーマ
- [`country_risk_v1.json`](schema/country_risk_v1.json) — カントリーリスクテーブルのスキーマ
- [`kyc_required_fields_v1.json`](schema/kyc_required_fields_v1.json) — KYC 必須項目ポリシーのスキーマ
- [`travel_rule_v1.json`](schema/travel_rule_v1.json) — トラベルルールポリシーのスキーマ
- [`tm_scenario_v1.json`](schema/tm_scenario_v1.json) — TM シナリオ定義の旧スキーマ。新規作成には使用しない。エンジンは後方互換のため受理し続け、本ファイルは [ADR-0006](../docs/decisions/0006-tm-scenario-v2-migration.md) に基づき 2027-07-04 まで維持する

各ルール定義の `schema_version` フィールドが、対応するスキーマを指す。

## カスタムルール作成の概要手順

1. 対象スキーマ（`schema/` 配下）を確認する
2. サンプル（`_sample/` 配下）をひな形としてコピーする
3. `id`・`name`・条件・ウェイト等を編集する
4. `is_sample` を `false` に設定する
5. スキーマでバリデーションする
6. `config.yaml` の `scoring.active_weight_id` / `monitoring.enabled_scenarios` で有効化する

詳細は [configuration.md](../docs/configuration.md) を参照。

## このディレクトリが含むもの

`content/` には、`_sample/`（動作確認用サンプル）、`schema/`（スキーマ定義）、
および出荷時デフォルトのポリシーファイル（`case_priority_v1.yaml`、
`edd_policy_v1.yaml`、`sla_policy_v1.yaml` 等）を置く。

## Engine configuration audit boundary

When the native Go engine loads content files directly, those files are outside the database-backed rule audit trail. Deploying organizations must control, approve, and retain the exact files they deploy. The engine records deterministic configuration digests at startup for post-hoc verification; this does not replace file access controls or change management. See [ADR-0012](../docs/decisions/0012-engine-config-file-trust-boundary.md).
