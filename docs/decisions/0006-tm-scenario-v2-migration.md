# [ADR-0006] tm_scenario_v2 スキーマと v1 デュアルサポート

| 項目 | 内容 |
|---|---|
| ステータス | 承認 |
| 決定日 | 2026-07-04 |
| 関連ADR | なし |

## コンテキスト

現行実装の TM シナリオ設定は、フラットな `parameters`/`risk_tier_adjustments`（リスクティア別のパラメータ上書き）構造を持つ。この構造には以下の欠落がある。

- 顧客種別（`customer_type`）ごとの閾値分岐ができない（個人と法人で同じ閾値になる）
- 評価モード（`evaluation_mode`: リアルタイム/バッチ/両方）の概念がない
- 絶対閾値（`absolute_threshold`、ティア別閾値によらず発火する安全弁）がない

これらは正本仕様の TM シナリオ定義（`threshold.by_customer_type.<type>.by_risk_tier.<tier>` のネスト構造）で要求されているが、現行実装のスキーマとは非互換である。ルール定義は Contract Stability の対象であり、破壊的に置き換えることはできない。

## 決定

- 新スキーマ `content/schema/tm_scenario_v2.json` を追加する。`type`/`conditions`/`severity` フィールドの有無で v1/v2 を構造的に判定する（`schema_version` 文字列の値による判定は、現行の v1 コンテンツが `"1.0"` を使っており、スキーマ文書が定める `"tm_scenario_v1"` という定数と一致しないため、判定根拠として使わない）。
- 既存の v1 コンテンツは最低 12 ヶ月デュアルサポートする。エンジンは v1 読込み時、`risk_tier_adjustments` を「全顧客種別共通の `by_customer_type`」へ内部変換して評価する。意味論は変えない。
- `evaluation_mode` は v1 では常に `both`（現行挙動を維持）、v2 では未指定時 `batch` をデフォルトとする。
- `absolute_threshold` は v1 では未指定のため、`parameters` 内の最大数値をシステムデフォルトとして採用する。v2 で未指定の場合は評価時にシステムデフォルトを適用する（安全弁のデフォルト適用ロジック自体は WS-5 スコープ）。
- 同梱サンプル（`content/_sample/tm_scenarios/`）は v2 形式へ書き換え、元の v1 サンプルは `content/_sample/tm_scenarios_v1_compat/` へ互換性テストフィクスチャとして退避する。

## 根拠

- 構造的判定（`type`/`conditions` フィールドの有無）は、`schema_version` の値が実装とスキーマ文書とで既に食い違っているという既知の事実（現状把握）に対して頑健である
- v1→v2 変換で「全顧客種別共通」を採用するのは、v1 に顧客種別軸が存在しないため、既存の評価結果を変えないための唯一の変換である
- 12 ヶ月のデュアルサポートは Contract Stability 原則の一般要件（ルールスキーマの後方互換）に合わせた

## 棄却した代替案

- **v1 コンテンツを自動的に v2 へ書き換えて配布** — 導入企業がカスタマイズした v1 コンテンツを検証なしに書き換えることは、意図しない意味論の変化やリスクにつながる。移行はエンジン側の読込み時変換に留め、コンテンツ自体は改変しない
- **`schema_version` 文字列による判定** — 実装済みの v1 コンテンツが仕様書の定数と異なる値を使っているため、判定が不安定になる

## 影響

- `engine/crates/merlon-engine/src/monitoring/config.rs` に `ScenarioConfig::load_dual`/`from_yaml_dual`/`resolve_threshold` を追加
- 既存のシナリオ評価ロジック（`structuring.rs`/`rapid_movement.rs`）は本 WS では変更しない。v2 コンテンツの `by_customer_type`/`resolve_threshold` を実際の発火判定に使うのは TM-004a（WS-5）のスコープであり、それまでは v2 コンテンツで評価するとシナリオ側のハードコードされたデフォルト値にフォールバックする（v1 相当の挙動にはならない）。WS-5 で優先的に解消すべき既知の制約として記録する
- `content/schema/tm_scenario_v1.json` は 2027-07-04 まで維持し、それ以降に撤廃を検討する
