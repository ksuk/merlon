---
title: CDD定期レビュー運用
sidebar_position: 9
---

# CDD定期レビュー運用

レビューキューは永続化された運用統制です。`customer_reviews` にサイクル
ごとのポリシーバージョン／ダイジェスト、担当、対象範囲、理由、証跡参照、
スコアリンク、完了者を保存します。`(customer_id, cycle)` の一意制約により
日次スイープを再実行しても重複しません。顧客の次回・前回日付は投影であり、
履歴行が正本です。

状態は `scheduled`、`due`、`overdue`、`in_progress`、`blocked`、`completed`
です。割当・開始は `version` による楽観ロックで競合を検出します。
`unable_to_complete` は理由と証跡を残して `blocked` とし、次回日付を進めません。

Analyst と Admin はすべてのリスク層を完了できます。完了には理由、証跡参照、
構造化された対象範囲が必要です。評価変更の場合は CDD エンジンを実行し、
スコア履歴をリンクします。レビュー、顧客投影、スコア履歴、監査、
`customer.review.completed` outbox は同じトランザクション境界で書き込み、
スコア・監査・outbox の失敗時はレビューを完了扱いにしません。

API は `GET /api/v1/customer-reviews`、`GET /{id}`、`PATCH /{id}`、
`POST /{id}/complete` を提供します。顧客詳細から履歴を辿れ、ダッシュボードと
キューで期限到来・期限超過・初回レビュー滞留を確認できます。
