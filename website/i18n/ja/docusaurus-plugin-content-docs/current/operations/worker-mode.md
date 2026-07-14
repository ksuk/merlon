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

バックテスト要求は永続行である。API 専用構成はジョブを受け付けて保存し、worker（または `all`）構成はデータベースリースでキュー行を取得し、再起動後も再開する。ジョブ作成時に `[from,to)` と設定ダイジェストをスナップショットし、進捗・ETA を報告する。アラートやケースは作成しない。`active` でない baseline／candidate 参照は解決され、バージョン付き定義がジョブ作成時に固定されるため、運用者が新しいルール版を公開してもキュー済みジョブは変化しない。解決できない参照はフェイルクローズする。
