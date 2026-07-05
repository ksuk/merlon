# Data Retention Policy

本文書は、犯罪収益移転防止法（犯収法）の記録保持要件と、Merlon におけるデータライフサイクル設計を示す。データ種別ごとの保持期間はアプリケーション層の `retention_policies` テーブル（`migrations/017_retention.sql`）でも管理し、本文書の値と一致させる。

## 犯収法の保持期間要件

| 記録種別 | 保持期間 | 根拠 |
|---|---|---|
| 取引記録 | 7 年 | 犯収法（取引終了日から） |
| 確認記録（本人確認） | 7 年 | 犯収法（取引終了日から） |

7 年は `2555` 日として `config.yaml` の `audit.retention_days` 等に反映する（[configuration.md](../configuration.md) 参照）。

## デフォルト保持期間（RET-001, RET-002）

audit.md §6 の保持期間表と一致させる。犯収法上の法定保存義務があるデータ種別（監査ログ以外）は**延長のみ可（短縮不可）**とし、`retention_policies.min_retention_days` の CHECK 制約（`retention_no_shorten`）で強制する。

| データ種別 | デフォルト保持期間 | 起算点 | 設定変更可否 |
|---|---|---|---|
| 取引データ | 7年（2555日） | 取引の行われた日（`transactions.occurred_at`） | 延長のみ可（短縮不可） |
| 顧客データ | 7年（2555日） | 最終取引日 | 延長のみ可（短縮不可） |
| アラート・ケース | 7年（2555日） | 関連取引の `occurred_at` | 延長のみ可（短縮不可） |
| 監査ログ | 10年（3650日） | ログ記録日（`created_at`） | 変更可（法定下限なし） |
| CDDスコア履歴 | 7年（2555日） | `scored_at` | 延長のみ可（短縮不可） |

## Merlon でのデータライフサイクル設計

Auditability First 原則に基づき、判断根拠の再現性を最優先とする。

| データ種別 | ライフサイクル |
|---|---|
| 監査ログ | append-only。削除・更新不可 |
| 顧客データ | 取引終了後 7 年保持。保存期間経過後は APPI 削除要求に応じて `attributes` 内の直接 PII を匿名化できる（RET-004、`api/internal/retention/anonymize.go`） |
| ルール定義 | 全バージョンを永続保持（過去の判断を再現可能にするため削除しない） |
| スコア履歴 | 全履歴を保持（リスク格付けの変遷を追跡） |
| 取引データ | 7 年保持後にアーカイブ／削除対象 |

自動パージ（RET-003）は論理削除→一定期間後の物理削除の2段階で実行し（`api/internal/retention/purge.go`）、パージの実行自体を監査ログに記録する（action: `purge_execution`）。

### ルール定義とスコア履歴を永続保持する理由

過去のある時点での判断（なぜ当時この顧客が高リスクと判定されたか）を再現するには、当時適用されていたルール定義とスコアの両方が必要である。ルール定義を上書き・削除すると過去の判断根拠が失われるため、全バージョンを保持する（Configuration as the Product 原則とも整合）。

## アーカイブ戦略（新規導入企業向けパーティショニングテンプレート）

PostgreSQL の宣言的パーティショニング（`PARTITION BY RANGE`）は大量データを扱う導入企業にとって有効だが、**既存の稼働中テーブルへの後付けパーティション化は行わない**（[ADR-0010](../decisions/0010-audit-log-partitioning-template.md)）。以下は、これから環境を新規構築する導入企業がゼロから採用できる月次 RANGE パーティションの DDL テンプレート例である。

```sql
-- audit_logs: created_at 基準の月次パーティション例
CREATE TABLE audit_logs (
    id              BIGSERIAL,
    user_id         VARCHAR(255),
    action          VARCHAR(100) NOT NULL,
    resource_type   VARCHAR(100) NOT NULL,
    resource_id     VARCHAR(255),
    details         JSONB DEFAULT '{}',
    ip_address      INET,
    user_agent      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

CREATE TABLE audit_logs_2026_07 PARTITION OF audit_logs
    FOR VALUES FROM ('2026-07-01') TO ('2026-08-01');
-- 以降、月次で CREATE TABLE ... PARTITION OF ... を追加する
-- (運用自動化する場合は pg_partman 等の拡張の利用を推奨)

-- transactions: occurred_at 基準の月次パーティション例
CREATE TABLE transactions (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id           UUID NOT NULL,
    external_id           VARCHAR(255) NOT NULL,
    amount                NUMERIC(20,2) NOT NULL,
    currency              VARCHAR(3) NOT NULL,
    direction             VARCHAR(10) NOT NULL,
    occurred_at           TIMESTAMPTZ NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
    -- (実際の列は migrations/002_transactions_alerts.sql を参照)
) PARTITION BY RANGE (occurred_at);

CREATE TABLE transactions_2026_07 PARTITION OF transactions
    FOR VALUES FROM ('2026-07-01') TO ('2026-08-01');
```

運用上の指針：

- 保持期間（監査ログ10年、取引7年）を超えた古いパーティションは、コールドストレージへの移送または切り離し（`DETACH PARTITION`）で扱う。物理削除は RET-003 の自動パージジョブのポリシーに従う。
- パーティション単位の操作により、巨大テーブルでも保持ポリシー適用（範囲検索・パージ）のコストを一定に抑えられる。
- 既に非パーティション運用で稼働している導入企業がパーティション化へ移行する場合は、「新テーブル作成→並行書き込み→データ移行→参照切替」という一般的な手順を要する。この移行は破壊的でダウンタイムを伴い得るため、本リポジトリの自動マイグレーションでは提供しない。移行を検討する場合は個別に計画を立てること。
- `customer_score_history`・`alerts` を含む、より広範なテーブルのパーティショニング戦略・容量計画・移行ガイドは別途 ADR で扱う。

## Enterprise WORM 監査ログによる改竄検知

Enterprise エディションでは監査ログを WORM（Write Once Read Many）モードで運用できる（`config.yaml` の `audit.worm: true`）。

- 一度書き込んだ監査ログは変更・削除できない
- ハッシュチェーン等により改竄を検知する
- 監査・検査時に、記録の完全性を証明する手段を提供する

WORM モードの詳細運用は今後のマイルストーンで拡充する。
