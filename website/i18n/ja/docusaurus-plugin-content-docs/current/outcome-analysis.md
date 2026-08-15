---
sidebar_position: 10
title: アウトカム分析と既知事案カバレッジ
---

# バックテストのアウトカム分析と既知事案カバレッジ

バックテストジョブには `outcome_analysis` を加算形式で返します。共有
`outcome-matcher-v1` 契約が、ベースライン・候補・差分のアラートを、過去
時点の不変なアラート判定、ケース、提出済み STR と照合します。

## 率と証跡の境界

各種別は `tp`、`fp`、`unlabeled`、`unevaluable`、`investigated`、`rate`、
`denominator` を返します。分母に入るのは TP と FP だけです。イベント時点の
スコア層がなければ、現在の顧客層へフォールバックせず `unevaluable` とします。
詳細行には matcher の版、スナップショット、前提、由来が付きます。

`GET /api/v1/backtests/{id}/outcomes` は永続化された詳細を返し、
`variant`、`scenario_id`、`label`、カーソルで絞り込めます。

## 既知事案カバレッジ

`POST /api/v1/coverage-analyses` は
`comparison/known_matter_coverage` の永続ジョブをキューへ追加します。ワーカー
は内部証跡を決定的に統合し、優先順を「エスカレーション済み／STR提出済みケース、
提出済み STR、ケース未紐付けの closed true-positive アラート」とします。紐付く行は
ケースを主事案として重複排除します。

`GET /api/v1/coverage-analyses/{id}` は全体とシナリオ別の集計を、
`GET /api/v1/coverage-analyses/{id}/matters` は由来、カバー状態、matcher の前提、
スナップショットを含む事案単位の結果を返します。同一顧客内では候補シナリオの
union で照合し、`not_covered` と `unevaluable` を別々に数えます。

対象は組織内部で把握できた既知事案だけです。観測されていない事象を推定せず、UI/API
では分母、matcher 版、スナップショット、前提を常に表示します。
