# [ADR-0005] カーソルページネーションへのデュアルサポート移行

| 項目 | 内容 |
|---|---|
| ステータス | 承認 |
| 決定日 | 2026-07-04 |
| 関連ADR | なし |

## コンテキスト

一覧系エンドポイント（顧客・取引・アラート・ケース）は offset/limit ベースのページネーションのみをサポートしていた。offset ベースは大きい offset での性能劣化（`OFFSET N` は N 件をスキャンして読み捨てる）が避けられず、データ量が増える運用（取引履歴等）では実用上の問題になる。

一方で、これらのエンドポイントは既に公開・利用されている可能性があるため、レスポンス形式やパラメータを破壊的に変更することは Contract Stability 原則に反する。

## 決定

- `cursor` クエリパラメータによるキーセットページネーション（`(created_at, id)` の組による境界判定）を追加する。
- 既存の `offset`/`limit` パラメータは受理し続ける（デュアルサポート）。`offset` 使用時は `Deprecation: true` および `Sunset`（+6 ヶ月）レスポンスヘッダを付与し、移行を促す。
- レスポンスは `{"data": [...], "pagination": {"next_cursor": ..., "has_more": ...}}` の形に統一する。既存の各要素（customer/alert/transaction/case オブジェクト）のフィールドは一切削除・変更しない。

## 根拠

- キーセットページネーションは `WHERE (created_at, id) < ($1, $2) ORDER BY created_at DESC, id DESC LIMIT $3` で一定時間に近い性能を維持できる
- offset を残すことで既存クライアントの即時破壊を避けつつ、ヘッダで移行期限を明示できる
- レスポンスをオブジェクトでラップする変更はトップレベルの形状（配列→オブジェクト）としては破壊的だが、要素ごとのフィールドは維持されるため、大半のクライアント実装（`response.data.map(...)` 等）は軽微な修正で追従できる。これは意図的なトレードオフであり、6 ヶ月の移行期間を設ける

## 棄却した代替案

- **offset を即時廃止** — Contract Stability 原則（後方互換の維持）に反する
- **`data` フィールドを追加せず配列のまま `pagination` を末尾に付与する等の非標準形式** — JSON配列に追加のメタデータを埋め込む標準的な方法がなく、クライアント実装の複雑化を招く

## 影響

- `api/internal/server/pagination.go` に `Cursor`/`PageRequest`/`PaginationMeta`/`BuildPaginationMeta` を新設
- `domain.Cursor` を repository 層の契約として追加し、`ListByCursor`/`ListOpenByCursor`/`ListByCustomerCursor` を各リポジトリに追加（既存の offset ベースメソッドは削除しない）
- OpenAPI ドキュメントに `pagination` スキーマと `offset` の `deprecated: true` を追加
- 6 ヶ月経過後、`offset` パラメータの完全撤廃を別途検討する
