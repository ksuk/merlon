# [ADR-0020] Wave 3 証跡の保持と追記専用の両立

| 項目 | 内容 |
|---|---|
| ステータス | 承認 |
| 決定日 | 2026-08-07 |
| 関連ADR | ADR-0012 |

## コンテキスト

migration 045 が `customers(id)` を参照する外部キーを追加した際、retention purger 側の対応が
なかった。`server/customer.go` は顧客作成時に identity history 行を必ず 1 行書くため、
**045 以降に作られた顧客の purge はすべて FK 違反 23503 でトランザクションごと中断していた**。
1 件の顧客が purge できないことが、全顧客の保持期間処理を止める。

調査の結果、欠陥は 045 由来だけではなかった。`customers(id)` を参照する 12 テーブルのうち、
purger のガードは 4 件しかなく、`whitelist_entries` / `str_reports` /
`backtest_job_customers` / `pending_evaluations` は Wave 3 以前から抜けていた。

さらに構造的な矛盾があった。045 が導入した `merlon_reject_append_only_mutation()` は UPDATE と
DELETE を全面的に拒否する。このトリガを持つテーブルは**永久に purge できない**。追記専用義務と
保持義務が同時に満たせない状態だった。

## 決定

**purge 対応の追記専用トリガ**: `merlon_reject_append_only_mutation_purgeable()` を新設する。
`merlon_reject_audit_mutation()` (043) が audit_logs に与えているのと同じ唯一の例外
——`purge_marked_at` のみを変更する UPDATE と、マーク済み行の DELETE——を与え、それ以外は拒否する。
監査用の関数と違い固定のカラム列挙ではなく行全体を比較するため、1 つの関数がすべての history
テーブルに使え、カラム追加で陳腐化しない。

**子を親より先に**: purger は子テーブルを親より先に mark & delete する。

**ガードの網羅を機械検証する**: `retention.CustomerReferencingTables` を宣言し、
`TestCustomerGuardCoversEveryForeignKey` が `pg_constraint` の実カタログと突き合わせる。
migration で外部キーを追加してここを更新し忘れると統合テストが落ちる。

**`screening_results.case_id` を `ON DELETE SET NULL`**: purge されるケースが結果を人質に取らない。

**証跡テーブルの purge は親に従う**: `backtest_job_affected_customers` のように親なしでは
意味を持たない行は `ON DELETE CASCADE` とし、独自の `purge_marked_at` を持たない。

## 根拠

追記専用と保持は対立する義務ではなく、粒度の問題である。「改竄できない」は「永久に残る」を
意味しない。保持期間の満了による削除は改竄ではなく、規制上むしろ要求される。必要なのは、
削除が保持ライフサイクルを経たものだけであることをデータベース自身が保証することである。

ガードの機械検証を入れたのは、この欠陥が「migration を書いた人が purger の存在を知らなかった」
という形で起きたからである。ドキュメントで再発を防ぐことはできない。カタログと突き合わせる
テストなら防げる。

`customer_edd_events` と `cdd_score_overrides` は `RESTRICT` を維持した。顧客に関する証跡は
顧客行の削除の副作用として消えるのではなく、それ自身の保持期間を経て明示的に消えるべきである。

## 棄却した代替案

**追記専用トリガを外す。** 追記専用は改竄検知の基盤であり、保持のために放棄するものではない。

**purge 対象テーブルから証跡を除外する。** 個人データを無期限に保持することになり、
保持期間の規制に反する。

## 影響

保持期間処理が Wave 3 導入後の顧客に対しても完走する。この欠陥は issue 化されておらず、
本番でデータ保持義務違反を静かに積み上げていた。

新しい外部キーを `customers(id)` に張る migration は、`CustomerReferencingTables` と purger の
ガードを同時に更新しなければ CI が通らない。
