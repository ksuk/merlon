# パーティショニング・キャパシティ運用ガイド

本ガイドは data-model.md §3.10「テーブル成長・パーティショニング・アーカイブ」に基づき、`transactions`・`audit_logs`・`customer_score_history`・`alerts` の4テーブルを対象とした運用方針をまとめたものである。

DDL テンプレート自体は以下を参照:

- `transactions`・`audit_logs`: [ADR-0010](../decisions/0010-audit-log-partitioning-template.md)、`docs/compliance/data-retention.md`
- `customer_score_history`・`alerts`: [ADR-0011](../decisions/0011-partitioning-strategy.md)、`docs/compliance/data-retention.md`

**本リポジトリのマイグレーション（`migrations/*.sql`）は、既存の稼働中テーブルへの後付けパーティション化を一切行わない。** 以下は新規導入企業がゼロから環境を構築する場合の指針、および既に非パーティション運用で稼働している導入企業が将来パーティション化へ移行する際の一般的な手順である。

## 1. 既存稼働テーブルのパーティション化移行手順（参考）

非パーティションテーブルを宣言的パーティショニング（`PARTITION BY RANGE`）へ移行する場合、PostgreSQL は既存テーブルをインプレースでパーティション化できないため、以下の手順が一般的となる。本リポジトリはこの手順を自動化するマイグレーションを提供しない（ダウンタイムまたは二重書き込み期間を要する破壊的作業のため、実行判断・スケジュールは導入企業に委ねる）。

1. **新テーブル作成**: `<table>_partitioned` のような新規名でパーティション親テーブルを作成し、必要な月次パーティションを事前に用意する。
2. **並行書き込み**: アプリケーション側でトリガーまたはデュアルライトにより、既存テーブルへの書き込みを新テーブルにも複製する期間を設ける。
3. **データ移行**: `INSERT INTO <table>_partitioned SELECT * FROM <table>` を、ロック競合を避けるためバッチ（例: 主キー範囲や日付範囲で分割）に分けて実行する。
4. **整合性検証**: 行数・チェックサム等で新旧テーブルの内容が一致することを確認する。
5. **切替**: メンテナンスウィンドウ内で `ALTER TABLE <table> RENAME TO <table>_old; ALTER TABLE <table>_partitioned RENAME TO <table>;` のようにリネームして切り替える。外部キー・インデックス・権限は新テーブル側に事前に作成しておく。
6. **旧テーブル保持**: 切替後も旧テーブルは検証期間（例: 1〜2週間）保持し、問題がないことを確認してから削除する。

## 2. GIN インデックスのオペレータクラス

`attributes` / `metadata` / `definition` 等の JSONB カラムには `jsonb_path_ops` オペレータクラスを優先採用する（デフォルト GIN より索引サイズが小さく、等価検索中心のクエリに適する）。

```sql
CREATE INDEX idx_customers_attributes_path_ops
    ON customers USING GIN (attributes jsonb_path_ops);
```

書き込み頻度の高いテーブル（`transactions`）は GIN インデックスの更新コストが無視できないため、JSONB カラム全体を索引化するのではなく、頻繁に検索する必要があるフィールドのみを式インデックス（expression index）化する方針とする。

```sql
CREATE INDEX idx_transactions_counterparty_country
    ON transactions ((metadata->>'counterparty_country'));
```

## 3. キャパシティプランニング

持続的なスループット目標として、**標準構成で秒間100件の取引取込み・評価**を基準値とする（PERF-001 のレイテンシ目標と両立）。これを超える負荷が見込まれる場合は Kubernetes 構成（deployment.md §1.3）への移行を推奨する。

## 4. リードレプリカのルーティング方針

DB レプリカ構成時（AVAIL-002）、以下のようにクエリ種別でルーティング先を分離する。

| クエリ種別 | ルーティング先 | 理由 |
|---|---|---|
| レポート・検知分析・バックテスト等の読み取り専用クエリ | リードレプリカ | プライマリの負荷を分散 |
| TM/CDD/スクリーニングのリアルタイム評価 | プライマリ | レプリケーションラグによる評価結果の不整合を避けるため |

## 5. autovacuum 調整推奨値

高頻度書き込みテーブル（`transactions`、`alerts`、`customer_score_history`）は `autovacuum_vacuum_scale_factor` をデフォルト（0.2）より小さい値、**推奨 0.02〜0.05** に調整し、bloat の蓄積を抑制することをセルフホスト導入企業向けに推奨する（deployment.md 参照）。

```sql
ALTER TABLE transactions SET (autovacuum_vacuum_scale_factor = 0.02);
ALTER TABLE alerts SET (autovacuum_vacuum_scale_factor = 0.02);
ALTER TABLE customer_score_history SET (autovacuum_vacuum_scale_factor = 0.02);
```

## 6. アーカイブ済みパーティションの扱い

- `customer_score_history` は全履歴を永続保持する方針（`docs/compliance/data-retention.md`「ルール定義とスコア履歴を永続保持する理由」）のため、古いパーティションを detach してもコールドストレージへの移送に留め、パージ（物理削除）は行わない。
- `alerts` はケース管理・監査の根拠となるため、パーティションの detach 後もアーカイブとして保持する。
- `transactions`・`audit_logs` の保持期間経過後の扱いは `docs/compliance/data-retention.md`・RET-002/003 を参照。
