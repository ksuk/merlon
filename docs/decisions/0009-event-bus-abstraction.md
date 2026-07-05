# [ADR-0009] イベントバス抽象化（pg_notify デフォルト、NATS オプション）

| 項目 | 内容 |
|---|---|
| ステータス | 承認 |
| 決定日 | 2026-07-05 |
| 関連ADR | ADR-0004 |

## コンテキスト

CDD スコア変動（ティア変更）を TM 再評価へ伝播させる仕組みが必要（CDD-009、Score-Driven Architecture）。Merlon はセルフホスト製品であり、導入企業ごとに取引量・API インスタンス構成が大きく異なる。小規模導入では追加のメッセージブローカーを運用させたくない一方、大規模導入や水平スケール構成では単一 PostgreSQL 接続への NOTIFY 配信では配信保証・スケーラビリティが不足する。

## 決定

`api/internal/events` に `Bus` インターフェース（`Publish`/`Subscribe`）を定義し、`EVENT_BUS` 環境変数（`pg_notify` | `nats`）で実装を切り替える。デフォルトは `pg_notify`。

選択基準（overview.md §4.4）：

| 基準 | 推奨 |
|---|---|
| 取引量：1万件/日未満、単一 API インスタンス | Pg LISTEN/NOTIFY（最小構成） |
| 取引量：1万件/日以上、または API サーバを水平スケールする場合 | NATS（JetStream） |

- **pg_notify：** PostgreSQL の `NOTIFY`/`LISTEN` を用いる。ペイロードサイズ上限（約8000バイト）があるため、通知には `event_id`/`topic`/`sequence_num` 等の最小情報のみを載せ、詳細は `customer_score_history` 等の正本テーブルへの再クエリで取得する「通知のみ」パターンとする。接続断からの再接続時は同テーブルの再クエリでキャッチアップする（再送保証がないため）。sequence number にギャップを検知した場合はデフォルト5秒待機し、到着しなければ正本テーブル再クエリにフォールバックする。
- **NATS：** JetStream（at-least-once、永続化）を使用する。JetStream なしの Core NATS（at-most-once）はメッセージ消失リスクがあるため、CDD→TM 等の重要イベント伝播には使用しない。本 WS では `events.Bus` を満たすインターフェーススタブのみ実装し、実接続（JetStream 接続ロジック）は別タスクで対応する。
- `EVENT_BUS=pg_notify` のまま API を水平スケールした構成を検出した場合（複数 API インスタンス想定の設定値）、起動時に警告ログを出す。PostgreSQL の `NOTIFY` は全リスニング接続にブロードキャストされるため、複数インスタンスが同一イベントを重複処理してしまうことを運用者に明示するため。

## 根拠

- 「1万件/日未満・単一インスタンス」を pg_notify の適用上限としたのは、NOTIFY のブロードキャスト特性上、複数インスタンスでは重複処理が避けられないという技術的制約に基づく（水平スケール＝NATS 必須は選択の余地がない）
- pg_notify をデフォルトにするのは、セルフホスト製品として追加ミドルウェア（NATS）なしで動作させたいという要件（Configuration as the Product、運用簡素化）に合わせるため
- NATS を JetStream 限定とするのは、Core NATS の at-most-once 配信では CDD→TM 連携イベントの消失がリスクベース・アプローチの前提を崩すため
- ペイロードを最小化し正本テーブル再クエリに寄せる設計は、NOTIFY のペイロードサイズ上限という技術的制約と、Auditability First（正本データからの再構成を常に可能にする）の両方に整合する

## 棄却した代替案

- **常に NATS を必須とする** — 小規模導入でも追加ミドルウェアの運用負担を強いることになり、セルフホスト製品としての導入障壁が上がる
- **pg_notify で複数インスタンス時の重複を自動的に排除する（advisory lock 等）** — 実装が複雑化する上、水平スケールを行う規模の導入では NATS の他の利点（永続化、配信保証）も併せて必要になるため、素直に NATS 必須とする方が設計として単純
- **NOTIFY ペイロードにイベント全体を載せる** — PostgreSQL の NOTIFY ペイロードサイズ上限（約8000バイト）を超えるイベントで破綻するため、最小情報＋正本テーブル参照方式を採用

## 影響

- `api/internal/events/bus.go` が `Bus` インターフェースと `Event`（`ChainID` を含む。cdd-scoring.md の循環依存遮断に使用）を定義する
- `api/internal/events/pgnotify` が pg_notify 実装、`api/internal/events/nats` がスタブ実装を提供し、`api/internal/events/factory.go` の `NewBus(cfg)` が `EVENT_BUS` に応じて選択する
- NATS の JetStream 実接続・永続化配信保証の実装は本 WS のスコープ外とし、D2 後半の別タスクで対応する
- WS-5 以降でイベント量が増加し水平スケールが必要になった場合、導入企業は `EVENT_BUS=nats` への切り替えと NATS サーバのプロビジョニングが必要になる
