# データ保持ポリシー

本文書は、犯罪収益移転防止法（犯収法）を主眼とした設定例と、Merlon におけるデータライフサイクル設計を示す。他法域（GDPR等を含む）では、導入組織が適用法と自組織の保持義務に応じて保持ポリシーを評価・設定する必要がある。データ種別ごとの保持期間はアプリケーション層の `retention_policies` テーブル（`migrations/017_retention.sql`）でも管理し、本文書の値と一致させる。

## 犯収法の保持期間要件

| 記録種別 | 保持期間 | 根拠 |
|---|---|---|
| 取引記録 | 7 年 | 犯収法（取引終了日から） |
| 確認記録（本人確認） | 7 年 | 犯収法（取引終了日から） |

7 年は `2555` 日として表現される。これは `retention_policies` テーブルにおける `transaction_data`・`customer_data`・`alert_case_data`・`cdd_score_history` の各行の初期投入値である（保持期間が `config.yaml` ではなく、このテーブルと対応する API を通じて管理される仕組みについては、[設定リファレンス](../configuration.md) を参照）。

## デフォルト保持期間（RET-001, RET-002）

以下は初期値であり固定値ではない。導入組織は、適用法、契約、監査要件と自組織の削除方針に従い、すべてのデータ種別について正の保持日数を設定する。設定変更は監査ログに記録され、期限到達後は30日の猶予を経て物理削除される。

| データ種別 | デフォルト保持期間 | シード済み下限 | 起算点 | 設定変更可否 |
|---|---|---|---|---|
| 取引データ | 7年（2555日） | 2555日 | 取引の行われた日（`transactions.executed_at`） | 下限以上の日数に変更可 |
| 顧客データ | 7年（2555日） | 2555日 | 最終取引日 | 下限以上の日数に変更可 |
| アラート・ケース | 7年（2555日） | 2555日 | `resolved_at` / `closed_at`（未解決・未完了は対象外） | 下限以上の日数に変更可 |
| 監査ログ | 10年（3650日） | なし | ログ記録日（`created_at`） | 正の日数に変更可 |
| CDDスコア履歴 | 7年（2555日） | 2555日 | `scored_at` | 下限以上の日数に変更可 |

### 保持期間の下限ガードレール

`retention_policies` の各行は、任意の下限値 `min_retention_days` を持てる。シードデータは、犯収法の保存義務に対応する 2555 日（7 年）を `transaction_data`・`customer_data`・`alert_case_data`・`cdd_score_history` に設定する。`audit_log` には下限をシードしない。保持期間 API は行の下限を下回る設定を拒否する（`api/internal/server/retention.go`）。これにより、誤入力によって法定期間内のレコードがパージ対象になる事故を防ぐ。

この下限はソフトウェアの固定ルールではなく、導入環境のデータである。義務が異なる導入組織 — 例えば GDPR 等でより短い削除期限が求められる場合 — は、自組織の法的評価の下で、データベース上の下限を直接変更する：

```sql
-- 例: customer_data の保持期間を 7 年未満に設定できるようにする
UPDATE retention_policies
SET min_retention_days = 1095   -- 下限自体を撤廃する場合は NULL
WHERE data_category = 'customer_data';
```

下限の調整後は、保持期間 API は下限以上の任意の正の日数を受け付ける。

## Merlon でのデータライフサイクル設計

Auditability First 原則に基づき、判断根拠の再現性を最優先とする。

| データ種別 | ライフサイクル |
|---|---|
| 監査ログ | 保持期間中は append-only。期限到達時に論理削除し、30日後に物理削除 |
| 顧客データ | 取引終了後 7 年保持。保存期間経過後は APPI 削除要求に応じて `attributes` 内の直接 PII を匿名化できる（RET-004、`api/internal/retention/anonymize.go`） |
| ルール定義 | 全バージョンを永続保持（過去の判断を再現可能にするため削除しない） |
| スコア履歴 | 設定保持期間中にリスク格付けの変遷を追跡。期限到達時に論理削除し、30日後に物理削除 |
| 取引データ | 設定保持期間後に論理削除し、30日後に物理削除 |

`PurgeJob` は PostgreSQL バックエンドの導入環境で毎日実行される。設定された保持期限の到達時に `purge_marked_at` を記録し、通常の API 読み取りではマーク済みデータを除外する。30日間の猶予期間の後、マーク済みの取引データ、アラート・ケースデータ、CDD スコア履歴、顧客データ、依存するスクリーニング結果、監査ログを物理削除する。顧客データは、独立して保持される子レコードがそれぞれのライフサイクルを完了するまで残存する。

### ルール定義を永続保持する理由

保持期間中は、当時適用されていたルール定義とスコア履歴により判断根拠を再現できる。ルール定義は上書き・削除すると過去の判断根拠を失わせるため全バージョンを保持する。一方、スコア履歴は利用者設定の保持期間と30日間の猶予を経た後に削除されるため、その期間を超える再現性は保証しない。

## アーカイブ戦略（新規導入企業向けパーティショニングテンプレート）

PostgreSQL の宣言的パーティショニング（`PARTITION BY RANGE`）は大量データを扱う導入企業にとって有効だが、**既存の稼働中テーブルへの後付けパーティション化は行わない**（ADR-0010）。以下は、これから環境を新規構築する導入企業がゼロから採用できる月次 RANGE パーティションの DDL テンプレート例である。

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

-- transactions: executed_at 基準の月次パーティション例
CREATE TABLE transactions (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id           UUID NOT NULL,
    external_id           VARCHAR(255) NOT NULL,
    amount                NUMERIC(20,2) NOT NULL,
    currency              VARCHAR(3) NOT NULL,
    direction             VARCHAR(10) NOT NULL,
    executed_at           TIMESTAMPTZ NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
    -- (実際の列は migrations/002_transactions_alerts.sql を参照)
) PARTITION BY RANGE (executed_at);

CREATE TABLE transactions_2026_07 PARTITION OF transactions
    FOR VALUES FROM ('2026-07-01') TO ('2026-08-01');
```

運用上の指針：

- 保持期間（監査ログ10年、取引7年）を超えた古いパーティションは、コールドストレージへの移送または切り離し（`DETACH PARTITION`）で扱う。物理削除を行う前に、導入組織が自組織の法務・監査要件に沿った削除実装と検証を行う。
- パーティション単位の操作により、巨大テーブルでも保持ポリシー適用（範囲検索・パージ）のコストを一定に抑えられる。
- 既に非パーティション運用で稼働している導入企業がパーティション化へ移行する場合は、「新テーブル作成→並行書き込み→データ移行→参照切替」という一般的な手順を要する。この移行は破壊的でダウンタイムを伴い得るため、本リポジトリの自動マイグレーションでは提供しない。移行を検討する場合は個別に計画を立てること。
- `customer_score_history`・`alerts` を含む、より広範なテーブルのパーティショニング戦略・容量計画・移行ガイドは別途 ADR で扱う。

既存の稼働中テーブルへの後付けパーティション化は行わない（ADR-0010、ADR-0011）。以下は、これから環境を新規構築する導入企業がゼロから採用できる月次 RANGE パーティションの DDL テンプレート例である（`customer_score_history`/`alerts` 分。`audit_logs`/`transactions` は ADR-0010 参照）。

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

- `customer_score_history` は利用者設定の保持期間を超えた後、`purge_marked_at` から30日経過で物理削除される。パーティション構成では、削除対象の古いパーティションを切り離す前に保持ポリシーと猶予期間を確認する。
- `alerts` はケース管理・監査の根拠となるため、パーティションの detach 後もアーカイブとして保持する。
- 既に非パーティション運用で稼働している導入企業がパーティション化へ移行する場合の一般的な手順、GIN インデックスのオペレータクラス選択方針、キャパシティプランニング、リードレプリカのルーティング方針、autovacuum 調整推奨値は [`docs/operations/partitioning-guide.md`](../operations/partitioning-guide.md) を参照。

## リーガルホールド

本リリースはレコード単位のリーガルホールド機構を提供しない。設定済み保持期間を超えて保全義務が生じたレコード — 当局からの照会、訴訟、内部調査など — については、導入組織が運用でホールドを実現する必要がある：

- **保持期限の到達前**: 保持期間 API で該当データ種別の保持期間を延長する。これは種別単位の手段であり、ホールド対象だけでなく種別内の全レコードが保持される。
- **パージマーク済みレコードの場合**: 保持期間を延長しても、既に `purge_marked_at` が設定されたレコードは救済**されない** — 30 日の猶予期間が経過すると物理削除される。猶予期間内に、対象レコードを Merlon 外にエクスポートして自組織の証跡管理手順で保全するか、自組織の変更管理・法的評価の下でマークを直接解除する：

  ```sql
  -- 例: リーガルホールド対象取引のパージマークを解除する
  UPDATE transactions SET purge_marked_at = NULL
  WHERE purge_marked_at IS NOT NULL AND customer_id = '<ホールド対象顧客>';
  ```

  マークを解除したレコードは、種別の保持期間も延長していなければ次回のパージ実行で再マークされるため、通常は両方の手当てが必要となる。

未解決アラートと未完了ケースは常にパージ対象外であり、Merlon 上で追跡中の調査は手動対応なしに保護される。

## 監査ログ改竄耐性の境界

自己ホスト版は `merlon-audit verify` による事後検証を提供するが、DB強制の不変性、WORMストレージ、ハッシュチェーンは現行リリースには含まれない。導入組織はDBロール、バックアップ、アクセス制御、監視を設定する責任を負う。詳細は [Regulatory Scope](regulatory-scope.md) を参照。
