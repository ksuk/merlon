---
title: CDD 定期レビュー・ポリシー
sidebar_position: 8
---

# CDD 定期レビュー・ポリシー

CDD 定期レビューの周期はレビュー worker の定数ではなく設定である。既定ファイルは
`content/cdd_review_policy_v1.yaml` で、機関承認済みの版を使う場合は
`MERLON_CDD_REVIEW_POLICY_PATH` を設定する。

周期は High 365日、Medium 730日、Low 1,095日である。未スコアの顧客はフェイルアラートの原則に従い
High として扱う。anchor は、最後に完了したレビュー、最後の CDD スコア、顧客作成時刻の順で選択する。
等級が上がった場合、次回レビューは評価時刻まで前倒しされる。予定には30日の grace period が付く。

レビュー完了もスコアもない顧客には、IDから決定的に計算した cold-start offset を付ける。上限は High 30日、
Medium 90日、Low 180日である。同じ顧客・ポリシーダイジェスト・入力からは常に同じ日付が得られる。完了には
rationale が必要で、実行できる role は Analyst と Admin に限定される。durable なレビューキューと完了記録は
CDD レビューワークフロー文書で扱う。

loader はスキーマ／ポリシーバージョンの誤り、欠落または非正の周期、未知の YAML フィールド、不完全な anchor
一覧、許可されない完了 role を拒否する。パース済みポリシーの SHA-256 ダイジェストとバージョンは
`GET /api/v1/system/status` と読み取り専用ポリシー API に含まれるため、レビュー予定の根拠を再現できる。
