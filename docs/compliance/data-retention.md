# Data Retention Policy

本文書は、犯罪収益移転防止法（犯収法）の記録保持要件と、Merlon におけるデータライフサイクル設計を示す。

## 犯収法の保持期間要件

| 記録種別 | 保持期間 | 根拠 |
|---|---|---|
| 取引記録 | 7 年 | 犯収法（取引終了日から） |
| 確認記録（本人確認） | 7 年 | 犯収法（取引終了日から） |

7 年は `2555` 日として `config.yaml` の `audit.retention_days` 等に反映する（[configuration.md](../configuration.md) 参照）。

## Merlon でのデータライフサイクル設計

Auditability First 原則に基づき、判断根拠の再現性を最優先とする。

| データ種別 | ライフサイクル |
|---|---|
| 監査ログ | append-only。削除・更新不可 |
| 顧客データ | 取引終了後 7 年保持 |
| ルール定義 | 全バージョンを永続保持（過去の判断を再現可能にするため削除しない） |
| スコア履歴 | 全履歴を保持（リスク格付けの変遷を追跡） |
| 取引データ | 7 年保持後にアーカイブ／削除対象 |

### ルール定義とスコア履歴を永続保持する理由

過去のある時点での判断（なぜ当時この顧客が高リスクと判定されたか）を再現するには、当時適用されていたルール定義とスコアの両方が必要である。ルール定義を上書き・削除すると過去の判断根拠が失われるため、全バージョンを保持する（Configuration as the Product 原則とも整合）。

## アーカイブ戦略

PostgreSQL のパーティショニングを用いる。

- 取引・スコア履歴・監査ログは時間ベース（例: 月次・年次）でパーティション分割する
- 保持期間を超えた古いパーティションは、コールドストレージへの移送または切り離し（detach）で扱う
- パーティション単位の操作により、巨大テーブルでも保持ポリシー適用のコストを抑える

既存の稼働中テーブルへの後付けパーティション化は行わない（[ADR-0010](../decisions/0010-audit-log-partitioning-template.md)、[ADR-0011](../decisions/0011-partitioning-strategy.md)）。以下は、これから環境を新規構築する導入企業がゼロから採用できる月次 RANGE パーティションの DDL テンプレート例である（`customer_score_history`/`alerts` 分。`audit_logs`/`transactions` は ADR-0010 参照）。

```sql
-- customer_score_history: scored_at 基準の月次パーティション例
CREATE TABLE customer_score_history (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id  UUID NOT NULL,
    risk_score   NUMERIC NOT NULL,
    risk_tier    TEXT NOT NULL,
    factors      JSONB,
    rule_set_id  TEXT,
    rule_set_version INTEGER,
    scored_at    TIMESTAMPTZ NOT NULL DEFAULT now()
) PARTITION BY RANGE (scored_at);

CREATE TABLE customer_score_history_2026_07 PARTITION OF customer_score_history
    FOR VALUES FROM ('2026-07-01') TO ('2026-08-01');
-- 以降、月次で CREATE TABLE ... PARTITION OF ... を追加する

-- alerts: detected_at 基準の月次パーティション例
CREATE TABLE alerts (
    id          TEXT PRIMARY KEY,
    customer_id TEXT NOT NULL,
    scenario_id TEXT NOT NULL,
    severity    TEXT NOT NULL,
    status      TEXT NOT NULL,
    score       NUMERIC,
    description TEXT,
    detected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
) PARTITION BY RANGE (detected_at);

CREATE TABLE alerts_2026_07 PARTITION OF alerts
    FOR VALUES FROM ('2026-07-01') TO ('2026-08-01');
-- 以降、月次で CREATE TABLE ... PARTITION OF ... を追加する
```

- `customer_score_history` は全履歴を永続保持する方針（本文書「ルール定義とスコア履歴を永続保持する理由」）のため、古いパーティションは detach してもコールドストレージへ移送するのみで、パージ（物理削除）は行わない。
- `alerts` はケース管理・監査の根拠となるため、パーティションの detach 後もアーカイブとして保持する。
- 既に非パーティション運用で稼働している導入企業がパーティション化へ移行する場合の一般的な手順、GIN インデックスのオペレータクラス選択方針、キャパシティプランニング、リードレプリカのルーティング方針、autovacuum 調整推奨値は [`docs/operations/partitioning-guide.md`](../operations/partitioning-guide.md) を参照。

## Enterprise WORM 監査ログによる改竄検知

Enterprise エディションでは監査ログを WORM（Write Once Read Many）モードで運用できる（`config.yaml` の `audit.worm: true`）。

- 一度書き込んだ監査ログは変更・削除できない
- ハッシュチェーン等により改竄を検知する
- 監査・検査時に、記録の完全性を証明する手段を提供する

WORM モードの詳細運用は今後のマイルストーンで拡充する。
