# [ADR-0011] customer_score_history・alerts のパーティショニングとキャパシティ/運用方針

| 項目 | 内容 |
|---|---|
| ステータス | 承認 |
| 決定日 | 2026-07-06 |
| 関連ADR | [0010](0010-audit-log-partitioning-template.md) |

## コンテキスト

data-model.md §3.10 は、運用に伴い無期限に成長するテーブルとして `transactions`、`audit_logs`、`customer_score_history`、`alerts` の4つを挙げている。このうち `transactions`（`occurred_at` 基準）と `audit_logs`（`created_at` 基準）の月次パーティション DDL テンプレートは ADR-0010（WS-9）が既に `docs/compliance/data-retention.md` に提供済みである。

`customer_score_history` はスコアリング契機（取引評価・定期再評価・手動再スコア等）のたびに1行追加されるため、`transactions` と同等以上の速度で成長し得る。`alerts` は取引監視の評価結果として生成され、件数は取引量とシナリオ数に比例して増える。両テーブルとも ADR-0010 のテンプレートの対象外だったため、本ADRで扱う。

また §3.10 はパーティショニング以外にも、GIN インデックスのオペレータクラス選択、キャパシティプランニングの基準値、リードレプリカのルーティング方針、autovacuum 調整推奨値を規定しており、これらは特定のテーブルに限らずシステム全体の運用方針であるため、本ADRと `docs/operations/partitioning-guide.md` で併せて文書化する。

ADR-0010 と同様、Merlon は既に稼働中の導入先が存在し得る自己ホスト型ソフトウェアであるため、既存テーブルへの後付けパーティション化（破壊的かつダウンタイムを伴う移行）は本リポジトリのマイグレーションとして提供しない（`03_implementation-plan.md` Global Constraints の additive-only 原則）。

## 決定

- `customer_score_history`（スコアリング日時基準）と `alerts`（`detected_at` 基準）の月次 RANGE パーティション DDL テンプレートを、新規導入企業向けの参考例として `docs/compliance/data-retention.md` に追記する。
- `transactions`、`audit_logs`、`customer_score_history`、`alerts` の4テーブルを対象に含めた、稼働中の非パーティションテーブルをパーティション化する際の一般的な移行手順（並行書き込み→データ移行→切替）を `docs/operations/partitioning-guide.md` にガイドとして記載する。本リポジトリの自動マイグレーションとしては実行しない。
- JSONB カラム（`attributes`/`metadata`/`definition` 等）には `jsonb_path_ops` オペレータクラスを優先採用する方針を明記する。書き込み頻度の高い `transactions` は、頻繁に検索する必要があるフィールドのみを式インデックス化する。
- キャパシティプランニングの基準値（標準構成で秒間100件の取引取込み・評価）、リードレプリカのルーティング方針（読み取り専用クエリはレプリカ、TM/CDD/スクリーニングのリアルタイム評価はプライマリ）、autovacuum 調整推奨値（高頻度書き込みテーブルの `autovacuum_vacuum_scale_factor` を 0.02〜0.05 に調整）を `docs/operations/partitioning-guide.md` に転記する。
- 本リポジトリの `migrations/` には変更を加えない（既存の `customer_score_history`/`alerts` テーブルは非パーティションのまま維持する）。

## 根拠

- ADR-0010 と同一の理由（additive-only 原則、稼働中導入先への意図しない破壊的変更の回避）が `customer_score_history`/`alerts` にも等しく当てはまる。
- `customer_score_history` はリスク格付けの変遷を追跡するために全履歴を永続保持する設計（`docs/compliance/data-retention.md`「ルール定義とスコア履歴を永続保持する理由」）であり、パーティション化しても保持方針（削除しない）自体は変わらない。パーティションは検索・vacuum コストの抑制が目的であり、パージ対象ではない。
- `alerts` はケース管理・監査の根拠となるため、パーティションの切り離し（detach）後もアーカイブとして保持する運用を前提とする。
- キャパシティ・レプリカ・autovacuum の方針は個別テーブルのDDLとは独立した運用ガイドラインであるため、ADRではなく `docs/operations/partitioning-guide.md`（実務者向け手順書）に集約し、ADR本文は「何を・なぜ決定したか」に留める。

## 棄却した代替案

- **`customer_score_history`/`alerts` も本リポジトリのマイグレーションとして強制的にパーティション化する** — ADR-0010 と同じ理由で却下（additive-only 原則違反、稼働中データへの破壊的移行の強制）。
- **`customer_score_history` は全履歴保持のため古いパーティションを自動 detach しない方針にする** — 保持方針上は妥当だが、パーティション自体の主目的（検索・vacuum コスト抑制）とは独立した運用判断であるため、本ADRでは規定せず `docs/operations/partitioning-guide.md` で「検索対象パーティション」と「アーカイブ済みパーティション」を分ける運用例として案内するに留めた。

## 影響

- `docs/compliance/data-retention.md` の「アーカイブ戦略」節に、`customer_score_history`/`alerts` の DDL テンプレート例を追記する（ADR-0010 が追加済みの `audit_logs`/`transactions` 例と並記）。
- `docs/operations/partitioning-guide.md`（新規）に、4テーブル共通の移行手順、GIN インデックス方針、キャパシティプランニング、リードレプリカ方針、autovacuum 推奨値を記載する。
- `migrations/` ディレクトリには変更を加えない。
