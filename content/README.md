# Merlon Content Packs

## Production warning

Sample rules and thresholds are public by design and can be used to infer detection behavior. They are not production-ready controls. Replace every sample threshold, scenario, and list with values derived from the deploying institution's documented risk assessment, and keep production values confidential. The sample content exists to demonstrate the configuration format, not to provide regulatory coverage.

本ディレクトリは、Merlon のルール定義コンテンツを格納する。AML/CFT のルール（CDD ウェイト、TM シナリオ等）は、Configuration as the Product 原則に基づき、JSON/YAML の設定として表現される。

## ディレクトリ構成

### `_sample/`
無償で提供するサンプルルール。**Apache-2.0** でライセンスされる（[`_sample/LICENSE`](_sample/LICENSE)）。

- `tm_structuring_basic` — 基本的な分割送金検知シナリオ（[`tm_structuring.json`](_sample/tm_structuring.json)）
- `cdd_basic_weights` — 基本的な CDD リスクウェイト（[`cdd_basic_weights.yaml`](_sample/cdd_basic_weights.yaml)）

サンプルを Apache-2.0 で別ライセンスとするのは、ユーザーが作成するルールのライセンス帰属を明確にするためである（[ADR-0003](../docs/decisions/0003-bsl-license-choice.md) 参照）。

### `schema/`
ルール定義のバリデーション用 JSON Schema。

- [`tm_scenario_v1.json`](schema/tm_scenario_v1.json) — TM シナリオ定義のスキーマ
- [`cdd_weight_v1.json`](schema/cdd_weight_v1.json) — CDD ウェイト定義のスキーマ

各ルール定義の `schema_version` フィールドが、対応するスキーマを指す。

## カスタムルール作成の概要手順

1. 対象スキーマ（`schema/` 配下）を確認する
2. サンプル（`_sample/` 配下）をひな形としてコピーする
3. `id`・`name`・条件・ウェイト等を編集する
4. `is_sample` を `false` に設定する
5. スキーマでバリデーションする
6. `config.yaml` の `scoring.active_weight_id` / `monitoring.enabled_scenarios` で有効化する

詳細は [configuration.md](../docs/configuration.md) を参照。

## Enterprise コンテンツパック（将来）

業種別・リスク類型別にチューニングされたルールプリセットを、Enterprise 向けコンテンツパックとして提供する予定である。コンテンツパックはソフトウェア本体（BSL 1.1）とは独立した商用ライセンスで提供される。

## Engine configuration audit boundary

When the Engine loads content files directly, those files are outside the database-backed rule audit trail. Deploying organizations must control, approve, and retain the exact files they deploy. The Engine records deterministic configuration digests at startup and exposes them through ConfigService for post-hoc verification; this does not replace file access controls or change management. See [ADR-0012](../docs/decisions/0012-engine-config-file-trust-boundary.md).
