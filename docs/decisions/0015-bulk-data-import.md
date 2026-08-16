# [ADR-0015] 初期移行のためのバルクデータインポート

| 項目 | 内容 |
|---|---|
| ステータス | 承認・実装済み |
| 決定日 | 2026-07-25 |
| 関連ADR | ADR-0012（エンジン設定ファイルのトラストバウンダリ）、ADR-0004（スコア駆動アーキテクチャ） |

## コンテキスト

導入企業が Merlon を本番投入するには、既存の基幹系から顧客マスタと取引履歴を
初期移行する必要がある。しかし現時点でバルクインポート手段は存在しない。

既存の投入経路と、それぞれが初期移行に使えない理由:

- **REST API（per-record）** — `POST /api/v1/customers` / `/api/v1/transactions`。
  唯一サポートされる経路であり、`docs/operations/initial-migration.md` はこれを前提に
  書かれている。ただし1レコード1リクエストであり、グローバルなレートリミッタ
  （`api/internal/server/ratelimit.go`）が掛かる。10万顧客規模では現実的な所要時間に
  収まらない。
- **`api/internal/seed`（demo ローダー）** — ファイル→リポジトリのバルクローダーとして
  既に機能しており、`api/cmd/merlon-api/main.go` の `runSeed` が単一トランザクションで
  包んでいる。しかし移行用途には使えない:
  - `loadDemoDataset` は10ファイル全部の存在を要求する。そこには alerts / cases /
    audit_logs / screening_results という **Merlon 側が生成すべき成果物**が含まれる。
    導入企業がこれらを持ち込むことはないし、持ち込ませてはならない。
  - `alreadySeeded` により顧客が1件でも存在すると中断する。再開・追加投入ができない。
  - dry-run が無く、投入前の検証ができない。
- **直接 SQL / `COPY`** — **採用不可**。直接 PII（`full_name`・`address`・
  `date_of_birth`・`phone`・`email`・`account_number`・`id_document_number`）は
  `api/internal/store/customer_pii.go` の `encryptDirectPII` によって
  **アプリケーション層で書き込み時に暗号化**される。SQL 直投入は暗号化前提のカラムに
  平文 PII を書き込み、読み出し時の復号を壊し、かつデータ保護上の違反となる。

## 決定

顧客・口座・取引の初期移行専用の `api/cmd/merlon-import` を提供する。設計制約に加え、
固定 CSV 契約、dry-run、行別 outcome、リポジトリ経由の apply を本 ADR の実装契約とする。

1. **リポジトリ層を通す。** 直接 SQL は使わない。`domain.CustomerRepository` 等の
   インターフェース経由で書き込み、PII 暗号化・FK 依存順・バリデーションを
   API 経路と共有する。`api/internal/seed/loader.go` の依存順ロジックを一般化して再利用する。
2. **対象エンティティを移行可能なものに限定する。** 顧客・口座・口座顧客リンク・取引のみ。
   alerts / cases / screening_results / audit_logs / score_history は Merlon が生成する
   ものであり、インポート対象にしない。demo ローダーの「10ファイル全部必須」は
   このローダーには持ち込まない。
3. **冪等キーは `external_id`。** `customers.external_id`（migrations/001）と
   `transactions.external_id`（migrations/002）は既に UNIQUE NOT NULL である。
   既存レコードは「スキップ」または「エラー」を選択可能とし、既定はスキップとする。
   これにより中断後の再実行が安全に再開できる。
4. **dry-run を必須機能とする。** 全件を解析・検証し、1行も書き込まずに件数と
   エラーを報告するモードを持つ。規制対象システムへの初回データ投入を
   検証なしに実行させない。
5. **インポート自体を監査する。** ソースファイル名と SHA-256、件数、実行者、
   開始・終了時刻を `import_runs` に、レコード単位の accepted/skipped/rejected と
   reason code を `import_record_outcomes` に記録する。
6. **入力ファイルを信頼境界の外として扱う。** ADR-0012 がエンジン設定ファイルに
   定めた扱いと同様に、インポートファイルは検証済み入力ではない。スキーマ検証・
   型検証・参照整合性検証を通過したものだけを書き込む。
7. **スコアリングは投入に含めない。** 投入後に既存の `POST /api/v1/batch/score` →
   `/api/v1/batch/monitor` でバックフィルする。ADR-0004 のスコア駆動アーキテクチャに従い、
   TM 閾値は CDD リスクティアから導出されるため、この順序は逆にできない。
   `/api/v1/batch/monitor` は `mode` を変えて **realtime・batch の2パス**を回す。
   シナリオは `evaluation_mode` でどちらのパスに属するかを宣言しており、片方だけでは
   ルールセットの一部が全履歴に対して未適用のまま「成功」と報告される。

## 根拠

- リポジトリ層経由は、PII 暗号化を「守るべき規約」ではなく「迂回できない構造」にする。
  直接 SQL を許した瞬間、平文 PII が入る経路が恒久的に開く。
- インポート対象を絞るのは、Merlon が生成すべきアラート・ケース・監査ログを
  外部から持ち込ませないため。持ち込みを許すと、検知結果の出所が追えなくなり
  Auditability First が崩れる。
- `external_id` を冪等キーにするのは、既に DB 制約として存在し、かつ導入企業側の
  自然キーでもあるため。新たな冪等キー概念を導入する必要がない。
- dry-run と監査記録は、本番データ投入という不可逆操作に対する最低限の統制である。

## 棄却した代替案

- **demo seed ローダーをそのまま初期移行に転用** — 生成物エンティティを必須とする
  ファイル構成、`alreadySeeded` による再開不能性、dry-run 非対応のいずれも
  移行要件と矛盾する。共有すべきは依存順ロジックであって、ローダーの契約ではない。
- **`COPY` による一括投入と、投入後の暗号化バッチ** — 平文 PII がディスクに
  書かれる期間が生じる。バックアップやレプリカに平文が伝播した場合、
  事後の暗号化では回収できない。
- **REST API にバルクエンドポイントを追加** — HTTP リクエストサイズとタイムアウトの
  制約を受け、大規模移行では結局分割が必要になる。また Contract Stability の対象である
  REST 契約に、移行という一時的用途のためのエンドポイントを恒久追加することになる。
  移行は CLI として提供し、REST 契約を汚さない。
- **実装を後回しにする** — dry-run と per-record outcome を備えない手作業経路を
  本番移行の正規手段に残すため採用しない。

## 影響

- `api/cmd/merlon-import/` と `api/internal/ingestion/` は本 ADR の固定契約を実装する。
- `migrations/059_bulk_import.sql` が run manifest と行別 outcome を追加する。
- REST 経路は後方互換のため維持するが、初期移行では CLI の dry-run/apply を優先する。
