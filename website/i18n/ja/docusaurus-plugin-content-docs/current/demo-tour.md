---
sidebar_position: 1.5
---

# Merlon デモツアー

このツアーはローカルデモ環境を2つのゴールデンパスで案内する: **動線A**はコンプラ担当者向け（7〜10分）、**動線B**は技術評価者向け（5〜7分）。登場する顧客・企業・取引はすべて合成データである — `merlon-demogen` が生成したものであり、実在の人物・企業・制裁/PEPリスト収載者・取引は一切含まれない。参照する固定ID（ラベル/UUID対応表）はリポジトリ内の `deploy/seed/demo/STORY_IDS.md` を参照。

このデモは自分のマシン上でのみ実行すること。認証は無効化されておりあくまでローカル評価用である — 公開到達可能なホストで動かしてはならない。

## ローカルスタックを起動する

```bash
docker compose -f docker-compose.demo.yml up --build
```

環境変数の設定は不要。`db` と `api` が `127.0.0.1:8080` にバインドされた状態で起動し、認証は無効化済み、合成顧客約1,015件・アラート98件が既にロードされている。

`docker compose` が両サービスともhealthyになったら [http://127.0.0.1:8080](http://127.0.0.1:8080) を開く。このツアー中に行った操作（ケースへのメモ追加、ステータス変更、STR下書き作成）はスタックを落とすまで保持される。

### データセットをリセットする

```bash
docker compose -f docker-compose.demo.yml down -v
docker compose -f docker-compose.demo.yml up --build
```

`down -v` はデモ用Postgresのボリュームを削除するため、次の `up` で同じ決定論的な合成データセットが最初から再ロードされる。

## 動線A — コンプラ担当者（7〜10分）

1. **ダッシュボード**（[`/`](http://127.0.0.1:8080/)）— 顧客数・アラート数・ケース数の概要、リスクティア分布、重大度分布チャートから始める。
2. **アラート**（[`/alerts`](http://127.0.0.1:8080/alerts)）— 「Critical」バッジの付いた唯一のアラートを探す。これは `demo-story-04`（Meridian Cross Trading Pte. Ltd.）のものであり、直接開くこともできる: [`/alerts/38d7a6ce-c160-5cf3-b748-ce2650893ff3`](http://127.0.0.1:8080/alerts/38d7a6ce-c160-5cf3-b748-ce2650893ff3)。
3. **アラート詳細** — `scenario_id`（`tm_rapid_movement`。Rules画面へのリンクあり）と説明文を確認し、関連取引バッジ3件のうち1件を開く。例えばパススルーの起点となったinbound取引: [`/transactions/b3dbf56d-8e2a-5f1c-86d2-ddf35ce38bfd`](http://127.0.0.1:8080/transactions/b3dbf56d-8e2a-5f1c-86d2-ddf35ce38bfd) — 香港からの320万円のinbound送金で、6時間以内にシンガポール・マレーシア宛のoutboundが続き、rapid-movementウィンドウでまとめて検知されている。
4. **顧客詳細**（[`/customers/61a626c6-ced4-536d-be74-41d6ca874e4d`](http://127.0.0.1:8080/customers/61a626c6-ced4-536d-be74-41d6ca874e4d)）— CDDスコアのファクター内訳を確認し、「Score」ボタン（UI言語設定がjaの場合の表示は「スコアリング」）をクリックしてnative engineによるライブ再スコアリングを実演する。スコア履歴に同じルールセット・同じtierの新しい行が追加されることを確認する（Auditability First: 同一入力から同一出力が再現される）。
5. **ケース** — アラートに紐づく既存ケースを開き（[`/cases/3a55610e-d00f-5a34-8bfa-cc9753cbfa06`](http://127.0.0.1:8080/cases/3a55610e-d00f-5a34-8bfa-cc9753cbfa06)）、短いメモを追加し、ステータスを遷移させる。ここが書き込み体験のステップである — メモとステータス変更は事前に仕込まれたものではなく、自分の操作である。
6. **Reports**（[`/reports`](http://127.0.0.1:8080/reports)）— 一覧から `demo-story-04` のアラートを選び（severity=criticalのため対象に含まれる）、STR下書きを作成し、CSVまたはJSONでエクスポートする。
7. **Audit**（[`/audit`](http://127.0.0.1:8080/audit)）— 直前に行った再スコアリング・ケースメモ・ステータス変更・STR作成のすべてが、実行者とタイムスタンプ付きで記録されていることを確認する。Auditability First原則の締めくくりであり、あらかじめ仕込まれた履歴だけでなく、いま自分が行った操作も記録される。

## 動線B — 技術評価者（5〜7分）

1. **Rules**（[`/rules`](http://127.0.0.1:8080/rules)）— タイプを `TM_SCENARIO` でフィルタし、`rapid_movement`（動線Aのアラートには `scenario_id: tm_rapid_movement` として記録されている）を開いて定義全体をJSONで確認する。続けて「Export」でJSONまたはYAMLとしてダウンロードする。ルールはハードコードされたロジックではなく、あくまで設定である。
2. **顧客の再スコアリング** — 任意の顧客詳細ページから「Score」をクリックし、新しい `customer_score_history` の行に評価対象のルールセットIDとバージョンが記録されることを確認する。
3. **Backtest**（[`/backtest`](http://127.0.0.1:8080/backtest)）— candidate rule setに `rapid_movement` を入力し、少数の顧客を選択して実行する。native engineが過去の取引に対して候補シナリオを再生するだけであり、実際のアラートには影響を与えない。
4. **Batch**（[`/batch`](http://127.0.0.1:8080/batch)）— 少数の顧客を選択してscoreまたはmonitorバッチを実行し、ダッシュボードの数値が変化することを確認する。
5. 最後に **Audit**（[`/audit`](http://127.0.0.1:8080/audit)）と **System**（[`/system`](http://127.0.0.1:8080/system)）でbacktestとbatchの実行が記録されていること、およびバージョン・機能フラグ・各コンポーネントのヘルスを確認し、OpenAPI契約をライブで取得する:

   ```bash
   curl http://127.0.0.1:8080/api/v1/openapi.json
   ```

## ほかにも見られる合成ストーリー

`demo-story-04` は仕込まれた6本のストーリーのうちの1つに過ぎない（ストラクチャリング、売り口座ミュール、ハイリスク国送金、上記のrapid-movementパススルー、休眠口座再活性化、複合ケース）。加えて合成の制裁/PEPリストに対するスクリーニングヒットもある。顧客・アラート・ケース・取引範囲の固定IDはすべてリポジトリ内の `deploy/seed/demo/STORY_IDS.md` に記載されているので、興味があれば他のストーリーも同様に辿ることができる。

## Dockerを使わずに実行する

Dockerを使いたくない場合は、一度ビルドしてから同じ合成データセットを生成し、デモcomposeスタックと同じルールコンテンツ・同じUIでAPIをin-memoryで起動できる。

```bash
make build   # Go APIとui/distをビルドする
make demogen # deploy/seed/demo/*.json を生成する

cd api
MERLON_SEED=true \
MERLON_AUTH_ENABLED=false \
MERLON_UI_DIR=../ui/dist \
MERLON_DEMO_DATA_DIR=../deploy/seed/demo \
MERLON_CDD_WEIGHTS_PATH=../content/_sample/cdd_weights/funds_transfer.yaml \
MERLON_TM_SCENARIOS_PATH=../content/_sample/tm_scenarios \
MERLON_COUNTRY_RISK_PATH=../content/_sample/country_risk_sample.yaml \
MERLON_SCREENING_LISTS_PATH=../deploy/seed/demo/screening_lists \
go run ./cmd/merlon-api
```

[http://localhost:8080](http://localhost:8080) を開く — 同じUI、同じ1,015顧客・98アラート、同じnative engine（ルールコンテンツは上記4つの `MERLON_*_PATH` 変数）が、PostgreSQLの代わりにin-memoryストアで動く。4つのルールコンテンツ変数をすべて設定しないと、engineが無効フォールバックしスコアリング・モニタリングが動作しないため、動線Aのステップ4と動線Bのすべてが機能しない — 4つとも必ず設定すること。ボリュームの削除は不要である。in-memoryストアを使っているため、プロセスを止めて再度 `go run ./cmd/merlon-api` を実行するだけで、生成し直したデータセットに戻る。
