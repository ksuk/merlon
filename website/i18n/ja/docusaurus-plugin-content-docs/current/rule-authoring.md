---
title: ルール記述ガイド
sidebar_position: 6
---

# ルール記述ガイド

本ガイドは、Merlon の CDD（顧客デューデリジェンス）リスクウェイト、国リスクテーブル、トランザクションモニタリング（TM）シナリオを定義するコンプライアンス担当者・ルール作成者向けである。**設定としてのプロダクト**（Configuration as the Product）の設計原則の下、これらのルールはアプリケーションコードではなくバージョン管理された JSON/YAML 設定として表現される。これにより、ソフトウェアのリリースを経ることなく、自組織のルールガバナンスプロセスを通じて作成・レビュー・承認できる。

## ルールの所在

ルールの内容はリポジトリ内の `content/` 配下に存在する。

- `content/schema/` — 各ルール種別を検証する JSON Schema。
- `content/_sample/` — フォーマットを示す Apache-2.0 ライセンスのサンプルルール。これらは本番運用に対応した管理策ではなく、サンプルディレクトリの `README.md` および各サンプルファイルがこの点を明示している（該当する場合は `is_sample: true`）。

ネイティブ Go エンジンは CDD ウェイト、国リスクテーブル、TM シナリオを運用者が提供するファイルから直接読み込む。例えば `MERLON_CDD_WEIGHTS_PATH` や `MERLON_TM_SCENARIOS_PATH`（[設定リファレンス](./configuration.md)参照）である。これらのファイルはデータベースに基づくルール監査証跡の外にあるため、ADR-0012（Engine Configuration File Trust Boundary）に記載の通り、そのソース管理、変更承認、アクセス制御、バックアップは自組織の責任となる。エンジンは起動時に、読み込んだ設定の決定論的ダイジェストを出力するため、事後にデプロイ済みのルールセットを検証できる。これは監査を支援するものであり、ファイルレベルのアクセス制御を代替するものではない。

ルール API で管理するルールは、これとは別のデータベース経路を使用する。新しいルール版は常に無効な状態で作成され、作成者とは異なる認証済み Admin が正確な対象版を有効化または無効化する。ADR-0014 の通り、作成者と承認者の判定、状態変更、追記専用 `rule_activation_events` への記録は原子的にコミットされる。いずれかの本人性を確認できない場合は処理を拒否する。このアプリケーション統制は `content/` 配下の運用者提供ファイルには及ばないため、導入組織はそれらのファイルについて独立した作成者・デプロイ担当者の承認を保持する。

## CDD ウェイト定義

CDD ウェイト定義は、重み付けされたリスク要因（「軸」）を顧客に割り当て、組み合わされたスコアからリスクティアを導出する。スキーマは `content/schema/cdd_weight_v1.json`（[リファレンス](./api/schema/cdd_weight_v1.md)）。最小限の実例は `content/_sample/cdd_basic_weights.yaml`。

```yaml
schema_version: cdd_weight_v1
id: cdd_basic_weights
name: Basic CDD Risk Weights
version: 1
is_sample: true

axes:
  customer_attributes:
    weight: 0.25
    factors:
      - name: customer_type
        scores:
          individual: 1
          corporate_domestic: 2
          corporate_foreign: 4
      - name: account_age_months
        ranges:
          - max: 3
            score: 4
          - max: 12
            score: 2
          - min: 12
            score: 1

  geography:
    weight: 0.30
    factors:
      - name: country_risk
        scores:
          low: 1
          medium: 3
          high: 5

tier_thresholds:
  low:
    max_score: 2.0
  medium:
    min_score: 2.0
    max_score: 3.5
  high:
    min_score: 3.5
```

スキーマ上の必須フィールド: `schema_version`（リテラル `cdd_weight_v1` である必要がある）、`id`（小文字、`^[a-z][a-z0-9_]*$`）、`name`、`version`（正の整数）、`axes`、及び `tier_thresholds`（`low`、`medium`、`high` を定義する必要がある）。`axes` 配下の各エントリには `weight`（0〜1）と `factors` 配列が必須。factor は値を直接スコアリングする（観測値をキーとする `scores`）か、数値範囲をスコアリングする（`min`/`max` の境界と `score` を持つ `ranges`）ことができる。

factor はインラインの `scores` マップの代わりに、`source` を `country_risk_table` に設定することで国リスクテーブルへスコアリングを委譲することもできる（`api/internal/engine/native` の国リスク実装を参照）。国別のリスクロジックを、すべての CDD ウェイト定義に重複させるのではなく一箇所で保守すべき場合にこれを利用する。

サンプルを本番用ルールに置き換えた場合は、`content/README.md` の指針に従い `is_sample: false` を設定する（または省略する）。

## 国リスクテーブル

国リスクテーブルは、明示的に列挙されていない国向けのデフォルト値とともに、ISO 国コードごとに数値のリスクスコア（1〜5）を割り当てる。スキーマは `content/schema/country_risk_v1.json`（[リファレンス](./api/schema/country_risk_v1.md)）。例は `content/_sample/country_risk_sample.yaml` より。

```yaml
schema_version: "1.0"
content_type: country_risk_table
name: Sample Country Risk Table
effective_date: 2026-07-01
default_score: 3
countries:
  JP: { score: 1 }
  US: { score: 1 }
  KP: { score: 5, reason: "FATF blacklist / foreign exchange sanctions" }
  IR: { score: 5, reason: "FATF blacklist" }
  MM: { score: 4, reason: "FATF grey list" }
```

必須フィールド: `schema_version`、`content_type`（リテラル定数 `country_risk_table`）、`effective_date`（日付）、`default_score`（1〜5）、及び `countries`。各国キーは `^[A-Z]{2}$`（ISO 3166-1 alpha-2 コード）に一致する必要があり、そのエントリには `score` が必須。`reason` は任意だが、**監査可能性優先**（Auditability First）原則に沿い、ある法域がなぜそのようにスコアリングされたかの追跡可能性のために推奨される。

## TM シナリオ

TM（トランザクションモニタリング）シナリオは検知ルール（例えば集計ベースのストラクチャリングチェック）と、それが生成するアラートの重大度を定義する。**新規シナリオは v2 スキーマ**である `content/schema/tm_scenario_v2.json`（[リファレンス](./api/schema/tm_scenario_v2.md)）に対して作成すること。例は `content/_sample/tm_scenarios/structuring_basic.yaml` を基にしたもの。

```yaml
schema_version: "2.1"
scenario_id: tm_structuring_basic
detector: structuring
name: "Structuring Detection (Basic)"
description: "Detects transaction splitting intended to evade reporting thresholds"
type: aggregation
conditions:
  transaction_type:
    - deposit
    - transfer_in
  aggregation:
    field: amount
    function: sum
    period: 24h
    group_by: customer_id
  threshold:
    by_customer_type:
      individual:
        by_risk_tier:
          LOW: 2000000
          MEDIUM: 1000000
          HIGH: 500000
      corporate_domestic:
        by_risk_tier:
          LOW: 2000000
          MEDIUM: 1000000
          HIGH: 500000
  additional:
    min_transaction_count: 3
evaluation_mode: both
severity: HIGH
tags:
  - structuring
```

必須フィールド: `schema_version`、`scenario_id`、`name`、`type`（現時点では `aggregation` のみ定義済み）、`conditions`、及び `severity`。v2.1 では明示的な `detector` も必須である。対応する検知器は `structuring`、`rapid_movement`、`high_frequency_small_amount`、`dormant_account_reactivation`、`high_risk_country_transfer` の5つであり、`scenario_id` から推測されない。`conditions` の下で、`threshold.by_customer_type` はシナリオごとに顧客種別ごとの異なるしきい値を設定でき、各顧客種別内では `by_risk_tier.{LOW,MEDIUM,HIGH}` が顧客の CDD リスクティアごとにしきい値を設定する。これは Score-Driven Architecture 原則の具体的な仕組みであり、顧客の CDD スコアがどの TM しきい値が適用されるかを決定する。`evaluation_mode` はシナリオがバッチジョブ・インライン・両方のいずれで実行されるかを制御し、省略された場合 v2 シナリオはデフォルトで `batch` となる。

`conditions.transaction_type` はシナリオを正規化された送信元トークンに任意で限定する。明示的なトークンがない取引は、方向に応じて `inbound → transfer_in`、`outbound → transfer_out`、`internal → transfer` にフォールバックする。集計では `field: amount`、`group_by: customer_id`、`24h` のような正の期間、`function: sum` または `count` を設定し、その期間が評価に使うイベント時刻ウィンドウとなる。未知のキーや未対応の集計形状は起動時の検証で失敗する。`absolute_threshold` は検知器固有の候補を作った後に適用され、リスクティアのしきい値を下げても迂回できない安全弁である（金額メトリクスの既定値は10,000,000、高頻度カウントの既定値は25）。

### レガシーな v1 コンテンツ

同梱コンテンツで旧来の `tm_scenario_v1` 形式を使用しているものはもはや存在しない。
`content/_sample/tm_scenarios/` 配下のサンプルはすべて v2 であり、元の v1 ファイルは
互換性テスト用のフィクスチャとして `content/_sample/tm_scenarios_v1_compat/` に
保存されているのみである。新規シナリオは v2 で作成すること。

Engine は、明示的な検知器の導入以前に作成されたコンテンツが動作し続けるよう、
v1 と v2.0 のファイルを引き続き受理する。ADR-0006 と ADR-0026 に基づき
**2027-08-15** までデュアルサポートし、v1 のフラットな `risk_tier_adjustments` を、
評価結果のセマンティクスを変えることなく、読み込み時に同等の「すべての顧客種別で
同一しきい値」という v2 形式へ内部変換する。旧形式は既知のシナリオIDプレフィックスからのみ
検知器を推測し、System Status は各ファイルの互換性警告を表示する。未知のIDは起動に失敗する。
期限前に移行し、推測に依存しないこと。`content/schema/tm_scenario_v1.json`
（[リファレンス](./api/schema/tm_scenario_v1.md)）は 2027-07-04 まで維持され、
撤廃の判断はその時点で別途行われる。

## ルールファイルの検証

デプロイ前に、すべてのルールファイルをそのスキーマに対して検証すること。これは組織的な承認ステップに先立ち、レビューまたは CI プロセスの一部として実行すべき機械的なチェックである。[ルールスキーマ](./api/schema/index.md)配下のスキーマリファレンスページは、`cdd_weight_v1`、`country_risk_v1`、`tm_scenario_v2` のすべてのフィールドと制約を文書化しており、`content/schema/` 内の JSON Schema ファイルから直接生成されるため、Engine が実際に強制する内容と同期し続ける。

## スコア駆動のロールアウト

CDD スコアはシステムの中心軸であるため（[アーキテクチャ](./architecture.md)及び ADR-0004、Score-Driven Architecture 参照）、CDD ウェイト定義の変更は、顧客がどのリスクティアに分類されるかを変化させ、それが今度はどの TM しきい値が適用されるか、ケースの優先度、スクリーニング頻度を変化させる。CDD ウェイトの変更は、その影響が単一の検知ルールに限定されず間接的かつシステム全体に及ぶため、TM シナリオの変更と同等かそれ以上の注意を払って扱うこと。

`docs/configuration.md` の運用しきい値に関する指針はここにも同様に適用される。`content/_sample/` のサンプルルールは一例であり、本番のデフォルト値ではない。しきい値は自組織の文書化されたリスクアセスメントから選定し、デプロイ前に変更をテストし、変更を自組織のルールガバナンスプロセスに通すこと。**フェイルアラート**（Fail-Alert）原則の下、新規または変更されたルールに関する不確実性は、アラートの抑制ではなく追加のレビューへ向けて解消すること。例えば、しきい値の選択が曖昧な場合、より狭い網（アラート数は少ないが見逃しの可能性が高い）よりも、より広い網（レビューして棄却される一致が多くなる）を選好する。
