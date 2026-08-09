# [ADR-0024] オーナーシップ、SLA、ケイパビリティの権限境界

| 項目 | 内容 |
|---|---|
| ステータス | 承認 |
| 決定日 | 2026-08-08 |
| 関連ADR | ADR-0016, ADR-0018, ADR-0023 |

## コンテキスト

ADR-0023 は DR-06（オーナーシップ階層）と DR-07（SLA ポリシー）を
「未決定かつ未実装であり、Wave 4 の P27（#79）着手前に解決すること」として明示した。
DR-17（ケイパビリティカタログと認証無効時の付与）は Wave 4 の P28（#80/#81）を止めていた。

3 件はいずれも**「Merlon がどこまで組織の構造を持つか」**という同じ問いの別の面である。
持ちすぎれば IdP や人事システムと二重管理になり、持たなすぎれば
規制業務として必要な統制を表現できない。

現状あるものは次のとおり。

- `assigned_to`（ユーザ）と `assigned_team`（文字列）が alert と case にある
- `server/operator_directory.go` が assignment 時に両方を検証する。
  ユーザの正本は API key store、チームの正本は `MERLON_OPERATOR_TEAMS`
- `due_at` は**クライアントが PATCH で入れる値**で、ポリシーもサーバ計算もない
- `auth.RolePermissions` が admin / analyst / viewer に 7 権限を割り当てる
- `MERLON_AUTH_ENABLED=false` では role が context に入らない（ADR-0018 は
  「role 検査は行わないが職務分離は行う」としている）

## 決定

### DR-06 — オーナーシップは user と team の 2 階層で終わり

**現状を正式契約とする。**

- ユーザの正本は API key store、チームの正本はデプロイ設定
  `MERLON_OPERATOR_TEAMS` の文字列リスト
- **チームを DB エンティティにしない。**階層（部門 → 課）も持たない
- **キューを永続エンティティにしない。**キューは filter の組み合わせであり、
  `mine` / `unassigned` / `team` / `overdue` / 年齢範囲の直交する条件で表現する

P27 は既存の `assigned_to` / `assigned_team` と operator directory だけで
ワークロードを集計できる。追加の migration もテーブルも要らない。

### DR-07 — SLA はポリシー駆動、既定は「未設定」

`content/sla_policy_v1.yaml` を ADR-0016 の型で新設する。固定の
`schema_version`、必須の `policy_version`、コード内 `DefaultSLAPolicy()`、
空パスで既定を返す `LoadSLAPolicy(path)`、解釈できない文書を拒否する
`Validate()`、そして digest。

- **サーバが計算する。UI は再計算しない。**`due_at` は severity / priority ごとの
  ポリシー宣言から `basis_at` を起点に算出する
- 状態は `not_configured | running | breached | met`
- **出荷時のポリシーはルール空**とする。すなわち既定は `not_configured` であり、
  Merlon が期限を勝手に作らない
- 第一版の業務時計は**単純経過時間（24 時間カレンダー）**とし、
  営業日カレンダーと pause 条件は持たない
- Alert / Case に適用したポリシー version は後から置換しない

`due_at` を PATCH で入れる既存の契約は残す（API-01）。ポリシーが解決した値と
クライアントが入れた値が食い違う場合、**ポリシーの値を正本とし、
クライアント指定は override として記録する**（ADR-0023 の DR 群と同じ扱い）。

### DR-17 — ケイパビリティは既存権限から導出し、demo は全許可を明示する

- **新しい権限体系を作らない。**`GET /system/capabilities` が返す
  `CapabilityDescriptor` は `auth.RolePermissions` から導出する
- role→permission を設定ファイルに逃がさない。認可をポリシーに移すと
  「設定ミス＝認可漏れ」の面が増える
- **`MERLON_AUTH_ENABLED=false` では全ケイパビリティを `available` とし、
  `auth_mode: disabled` を必ず併記する。**UI は #81 の受入基準どおり、
  この状態を認証済み本番セッションと視覚的に区別する
- 可用性の語彙は master plan CAP-01 の
  `available | not_configured | forbidden | unsupported | degraded | unavailable`
  をそのまま使う
- **UI の非表示は認可ではない。**同じ permission をサーバ route でも強制する

## 根拠

DR-06 と DR-07 は逆向きに見えて同じ規準に従っている。**Merlon が
一次情報を持たないものについて、持っているふりをしない。**

組織階層は IdP と人事システムが持つ。Merlon がそれを複製すれば、
権威のある側が変わったときに黙って古い構造で集計する。
同じ理由で、設定されていない SLA について既定の期限を作らない——
「設定した覚えのない期限で超過と表示される」のは、
未設定を healthy と表示するのと同じ種類の誤りである（ADR-0023 で棄却済み）。

DR-17 で demo を全許可にするのは、認証を構成していないデプロイに
role が存在しないからである。存在しない role から権限を推測するより、
「認証していない」という事実を表示する方が正確で、
評価用デプロイ（#35）でバッチやスクリーニングを試せる状態も保てる。

## 棄却した代替案

**チームを DB エンティティにして階層を持たせる**: 入れ子の集計はできるが、
migration・CRUD API・管理 UI が要り、IdP のグループと二重管理になる。

**保存済みキューをエンティティにする**: 運用の自由度は最大だが Wave 4 の
スコープを大きく超え、キューの権限モデルという新しい問題を持ち込む。

**SLA に営業日カレンダーと pause を含める**: 規制業務としては正確だが、
カレンダー保守と pause 状態遷移の設計・テストが要る。第一版で持たないのは、
**未設定を既定にした以上、精緻な既定値そのものに意味がない**からである。
必要になった組織はポリシー文書を書く。

**SLA を契約から削除する**: 最小工数だが、#79 の
「SLA ポリシー未設定を明示する」を表現できないまま Wave 4 に入ることになる。

**demo でも viewer 相当に制限する**: 安全側に見えるが、`MERLON_SEED=true` の
デモでバッチ実行もスクリーニングも試せなくなり、#35 で整備した評価導線を損なう。
demo の危険は権限ではなく**データが合成であること**で、そちらは既に表示している。

**role→permission を設定ファイル化する**: 組織ごとの権限設計に対応できるが、
認可の正しさが設定の正しさに依存するようになる。認可はコードで持つ。

## 影響

- P27（#79）は追加の migration なしで着手できる。ワークロード集計は
  `assigned_to` / `assigned_team` / operator directory だけで成立する
- **SLA ポリシーを書いていないデプロイでは、#79 のダッシュボードは
  期限系の数値を `not_configured` として表示し、0 件とも healthy とも表示しない**
- `sla_policy_v1` は新規 migration を必要としない。`due_at` 列は既にあり、
  ポリシー由来の値をそこへ書く
- ケイパビリティは `auth.RolePermissions` の変更に追従する。
  権限を増やすときはカタログではなくそちらを変える
- 認証無効のデプロイは全機能が使える。これは意図された挙動であり、
  `auth_mode: disabled` の表示がその証跡になる
