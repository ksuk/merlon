---
title: API / Worker モード
---

# API / Worker モード

PH9 は、明示的な所有モードを持つ単一の `merlon-api` イメージを使用する。

| `MERLON_MODE` | 所有する処理 |
|---|---|
| `all`（デフォルト） | HTTP、リアルタイム監視、スクリーニング取り込み、通知、保持、リカバリ、TM バッチ、永続バックテスト |
| `api` | HTTP／リアルタイム処理、スクリーニング取り込み、通知、保持、EDD |
| `worker` | リカバリ、スケジュール済み TM バッチ、永続バックテスト。制御用 HTTP は `MERLON_WORKER_HTTP_ADDR` で待ち受ける |

分離は任意である。2 コンテナ構成では PostgreSQL のみを共有し、キューやキャッシュは使用しない。

```yaml
services:
  api:
    image: merlon-api:latest
    environment:
      MERLON_MODE: api
      MERLON_DATABASE_URL: postgres://merlon:secret@postgres/merlon
  worker:
    image: merlon-api:latest
    environment:
      MERLON_MODE: worker
      MERLON_WORKER_HTTP_ADDR: :8081
      MERLON_DATABASE_URL: postgres://merlon:secret@postgres/merlon
```

イメージのヘルスチェックは選択されたプロセスモードに従う。`worker` モードでは API listener ではなく、`MERLON_WORKER_HTTP_ADDR`（既定 `:8081`）の liveness を probe する。

## リアルタイムモニタリングの復旧

モニタリング依存先が未設定の場合、`POST /api/v1/transactions` はモニタリング済みとして応答しない。取引、変更監査、1件の保留評価、キュー登録監査を同一トランザクションで保存し、取引とともに `monitoring_evaluation` を返す。このオブジェクトは永続キュー行と現在の状態を示す。同じ冪等性キーで再送しても、取引、保留評価、監査を重複作成せず、同じ取引とキュー行を返す。

モニタリング依存先が利用可能になると、`worker` または `all` モードの復旧ループが保留行を取得する。取引詳細は `RESOLVED` を含む現在のキュー状態を継続して返すため、保存されたという理由だけで評価済みには見えない。APIが保留評価と監査を同一データベーストランザクションで保存できない場合、取引作成は失敗し、取引はcommitされない。

バックテスト要求は永続行である。ネイティブ実行エンジンが利用不能な場合、API はジョブを保存する前に新規要求を拒否する。エンジンを読み込み済みの API 専用構成は別worker向けにジョブを保存できる。worker（または `all`）構成はデータベースリースでキュー行を取得し、再起動後は期限切れの実行中リースを再取得する。

受理済みジョブが無期限に待機し続けることはない。すべての API／worker プロセスがデータベースを使ったライフサイクル監視を実行する。`MERLON_BACKTEST_QUEUE_TIMEOUT`（既定 `10m`）以内にworkerが行を取得しない場合、その行は安定した再試行可能理由を伴う `failed` になる。`POST /api/v1/backtests/{id}/retry` は同じジョブIDを `queued` に戻す。すでに `queued` または `running` の間に同じ要求を繰り返しても冪等である。再試行時は、別workerがジョブを取得できるようになる前に部分的な結果行を同一トランザクションで消去するため、再実行結果が重複して蓄積しない。

workerの失敗時に保存される公開エラーは、内部のエンジンや依存先エラーを含まない安定した文言である。保護されたプロセスログで根因を調査し、依存先を復旧してから失敗ジョブを再試行する。監査記録には開始、失敗、完了、再試行要求、キュー期限切れが記録される。`merlon_backtest_job_transitions_total{transition=...}` カウンターはジョブIDを含めず、同じライフサイクル区分を公開する。

ジョブ作成時に `[from,to)` と設定ダイジェストをスナップショットし、進捗・ETA を報告する。アラートやケースは作成しない。`active` でない baseline／candidate 参照は解決され、バージョン付き定義がジョブ作成時に固定されるため、運用者が新しいルール版を公開してもキュー済みジョブは変化しない。解決できない参照はフェイルクローズする。
