# [ADR-0019] CDD スコアリングの権限分離と説明可能性

| 項目 | 内容 |
|---|---|
| ステータス | 承認 |
| 決定日 | 2026-08-07 |
| 関連ADR | ADR-0004, ADR-0014, ADR-0016 |

## コンテキスト

ADR-0004 のとおり CDD スコアはシステムの中心軸であり、EDD の要否、TM の閾値、再スクリーニング
頻度がここから導かれる。その中心軸を動かす操作が、システム中で最も保護されていなかった。

- `POST /customers/{id}/score` はどの role からも到達できた。閲覧のみの Viewer が顧客のリスク
  等級を書き換えられた
- `override_evidence` は無検証で `ScoreRecord` に書き込まれ、同じリクエストの結果として
  `customers.risk_tier` が即座に動いた。**一人の人間が、構造化されていない注記だけで、顧客を
  High から外せた**
- `domain.Factor` の `Score` と `Contribution` に同じ値が入っていた。`Score` を合算する
  消費者は加重を二重に数える。スコア説明自身の照合ループもこれに引きずられ、
  `if factor.Contribution == 0 { sum += factor.Score }` というフォールバックを持っていたため、
  **不一致を検出することが原理的にできなかった**
- スコア説明は tier を報告するが、それを決めた閾値を報告しなかった。Medium まで 0.01 の顧客と
  Low の中央にいる顧客が同じに見えた
- `rule_set_version` が二義的だった。未バージョン経路は digest の fingerprint を書き込み、
  バージョン経路は DB の実バージョンを書き込む。未バージョンのスコアがバージョン 1,750,295,863
  に見えた

## 決定

**スコアリングの権限（互換性破壊②）**: `cdd:score` 権限を新設し、Admin と Analyst に付与する。
Viewer は 403 になる。ゲートはデプロイが認証を構成しているかを見る（ADR-0018 と同じ規則）。
認証なしのデプロイでは従来どおり動く。

**override のデュアルコントロール**: `override_evidence` を閉じた形状
（`{reason, proposed_tier, supporting_documents}`、未知キーは拒否）で検証し、**提案**として
`cdd_score_overrides` に `pending_approval` で積む。`customers.risk_tier` は動かさない。
`POST /customers/{id}/score-overrides/{id}/approve`（`cdd:override:approve`、Admin のみ、
申請者による承認を拒否）が初めて等級を動かす。

**理由の要求**: override または rule_set version の明示ピン止めがあるときに `rationale` を必須とする。
機械的な既定再スコアには要求しない。

**`factors[].score` の意味変更（互換性破壊③）**: `Score` を因子自身の正規化値、
`Contribution` を加重寄与とする。説明は `reconciled` と `reconciliation_delta` を返し、
`tier_thresholds` と `tier_reason` を添える。

**`rule_set_version` の一義化**: 未バージョン経路で fingerprint を書き込むのをやめ、0 とする。
pin は `rule_set_sha256` である。

**適用ルールセットの解決**: `GET /api/v1/customers/{id}/cdd-rule-sets` が候補ごとに
id / name / version / is_active / digest / マッチした条項 / `recommended` を返す。

## 根拠

Viewer の 403 に加算的な代替はない。目的が「できたことをできなくする」ことだからである。
閲覧専用の role がリスク等級を動かせるのは統制上の欠陥であり、加算的に直すことはできない。

override を提案にしたのは、whitelist_entries が migration 010 以来そうしているからである。
アラートの抑止に第二の署名を要求しながら、リスク等級の変更に要求しないのは一貫していない。

`Score` と `Contribution` の分離は、説明可能性の前提である。両者が同じ値である限り、
「因子の合計がスコアと一致するか」という検算は自分自身を検算していることになり、
何も証明しない。パリティコーパスの scoring レコードは再凍結したが、**レコード単位のスコアは
変わっていない**。これがこの変更の要点である: 守るべき合計は動かず、内訳が合計を説明できる
ようになった。

`rationale` の要求を rule_set_id 全般ではなく version のピン止めに絞ったのは、ルールセットを
名指しすること自体は通常の利用であって逸脱ではないからである。計画では `rule_set_id` があれば
必須としていたが、それはルールセットを明示する全クライアントを壊す。帰属が必要なのは、
現行設定からの意図的な逸脱である。

## 棄却した代替案

**override を維持しつつ audit だけ強化する。** 事後に誰がやったか分かることと、事前に一人では
できないことは別の統制である。ADR-0014 でルール有効化について同じ判断をしている。

**`factors[].score` を維持して新フィールドを足す。** `score` という名前が加重寄与を指し続ける
限り、それを合算するクライアントは黙って誤り続ける。名前が意味と一致しないことがこの欠陥の
原因だった。

## 影響

**互換性破壊②**: Viewer role の連携は Analyst に移行する必要がある。

**互換性破壊③**: `factors[].score` を合算していたクライアントは `contribution` に移行する。
移行手順は CHANGELOG の Breaking に記載した。

override を伴うスコアは 2 人を要する。単独の運用者は計算された等級のみを記録できる。
