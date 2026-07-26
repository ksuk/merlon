---
title: トラブルシューティング
---

# トラブルシューティング

実際に表示されているメッセージを探すこと。各行は、それを説明する節へのリンクになっている。

## 症状の逆引き

| 表示されている内容 | 参照先 |
|---|---|
| `set MERLON_POSTGRES_PASSWORD` または `set MERLON_BOOTSTRAP_TOKEN` が出て何も起動しない | [コンテナが1つも起動せず compose が停止する](#コンテナが1つも起動せず-compose-が停止する) |
| 新規デプロイで `docker compose up --wait` が返ってこない、または readiness プローブが失敗する | [セットアップ完了まで readiness は 503 を返す](#セットアップ完了まで-readiness-は-503-を返す) |
| ログイン画面が出るが、ログインできるアカウントが無い | [ログインできるアカウントが存在しない](#ログインできるアカウントが存在しない) |
| `initial setup has already been completed` | [`/setup` が 409 を返す](#setup-が-409-を返す) |
| `MERLON_AUTH_ENABLED must be true in production` | [本番環境で起動が拒否される](#本番環境で起動が拒否される) |
| `MERLON_DATABASE_URL must be set in production` | [本番環境で起動が拒否される](#本番環境で起動が拒否される) |
| `MERLON_ENCRYPTION_KEY_RING must be set in production` | [本番環境で起動が拒否される](#本番環境で起動が拒否される) |
| `MERLON_SEED must not be true in production` | [本番環境で起動が拒否される](#本番環境で起動が拒否される) |
| `MERLON_TRUSTED_PROXY_CIDRS must be set when MERLON_RATE_LIMIT is enabled in production` | [本番環境で起動が拒否される](#本番環境で起動が拒否される) |
| `database audit privilege preflight failed` | [監査権限がハードニングされていない](#監査権限がハードニングされていない) |
| `native Go engine unavailable`、`read CDD config` | [スコアもアラートも生成されない](#スコアもアラートも生成されない) |
| `MERLON_ENCRYPTION_KEY_RING not set, customer PII fields will be stored in plaintext` | [PII が平文で書き込まれた](#pii-が平文で書き込まれた) |
| `MERLON_MIGRATION_DATABASE_URL is required in production` | [マイグレーション](database.md#マイグレーションロールが指定されていない) |
| `using MERLON_DATABASE_URL as migration role` | [マイグレーション](database.md#マイグレーションロールが指定されていない) |
| `checksum mismatch: ledger=… file=…` | [マイグレーション](database.md#マイグレーションのチェックサムが一致しない) |
| `does not match a migration filename` | [マイグレーション](database.md#既存データベースに台帳が無い) |
| 顧客レコードが復元後に読めなくなった | [マイグレーション](database.md#復元したデータを復号できない) |
| Webhook の受信側にイベントが届かなくなった | [Webhook の配信が止まる](#webhook-の配信が止まる) |
| 制裁リスト／PEP スクリーニング結果が古いように見える | [スクリーニング結果が古い](#スクリーニング結果が古い) |

## 最初に確認すること

以下の2点で、報告の相当数はそれ自体で解決する。

**実際に動いているバージョンを確認する。** `GET /healthz` が返す。`dev` と表示される場合、そのイメージは `VERSION` ビルド引数なしでビルドされており、リリース済みビルドではない。[コンテナイメージ](../operations/container-images.md)を参照。

```bash
curl -s http://localhost:8080/healthz
```

**liveness ではなく readiness を確認する。** `GET /healthz` はプロセスが起動していることを示すだけである。`GET /healthz/ready` は実際にリクエストを処理できるかを示し、失敗しているサブシステムを名指しする。

```bash
curl -s http://localhost:8080/healthz/ready
# {"checks":{"setup":"ok","postgres":"ok","engine":"ok"},"status":"healthy"}
```

失敗したチェックはすべてエラー内容とともに `checks` に現れる。ログを読む前にこれを読むこと。

## 起動と初回実行

### コンテナが1つも起動せず compose が停止する

```
error while interpolating services.db.environment.POSTGRES_PASSWORD:
required variable MERLON_POSTGRES_PASSWORD is missing a value: set MERLON_POSTGRES_PASSWORD
```

compose ファイルは、データベースパスワードとブートストラップトークンにデフォルト値を意図的に持たせていない。デプロイが既知の認証情報を偶然引き継ぐことを防ぐためである。compose はこれらをリポジトリルートの `.env` から読む。このファイルはコミットされていない。

```bash
cp .env.example .env
docker compose up --build
```

`.env.example` の値は開発専用であり、その旨が明記されている。自分のマシン以外で動かす前に置き換えること。

### セットアップ完了まで readiness は 503 を返す

これは新規デプロイでの期待される状態であり、障害ではない。

イメージのヘルスチェックは `GET /healthz/live` を参照するため、`docker ps` はプロセスが応答した時点で（セットアップ前でも、データベースが無くても）コンテナを `healthy` と表示する。

readiness はこれとは別の話である。`GET /healthz/ready` には「管理者アカウントが存在すること」が含まれるため、[初期セットアップ](#ログインできるアカウントが存在しない)を完了するまで `503` と次の内容を返す。

```json
{"checks":{"setup":"error: initial setup not completed"},"status":"unhealthy"}
```

これは、readiness を意図的に条件にしている箇所で問題になる。

- `/healthz/ready` を見る Kubernetes の `readinessProbe`。Pod は Service のエンドポイントに入らない。
- `docker compose up --wait`。コマンドが返ってこない。
- readiness を参照する compose のヘルスチェックに対する `depends_on: condition: service_healthy`。デモ用 compose ファイルは、`healthy` が「デモが実際に利用可能である」ことを意味するよう、意図的にこの構成を採っている。

これらの環境では、初回ロールアウト中にセットアップを完了させること。最初の管理者が作成されれば、readiness は1回のプローブ間隔以内に `healthy` へ切り替わる。

なお、ヘルスチェックが liveness に変更される前にビルドされたイメージは、イメージレベルで readiness を参照している。古いイメージではコンテナ自体がセットアップ完了まで `unhealthy` のままとなり、unhealthy なコンテナを再起動するオーケストレーターはその前にインスタンスを停止してしまう。初回ロールアウト中にセットアップを完了させるか、ヘルスチェックの start period を延ばすこと。

### ログインできるアカウントが存在しない

新規デプロイにはユーザーが存在しないため、ログインする手段も、認証された経路でユーザーを作成する手段も無い。初期セットアップを使う。

- ブラウザ: ログイン画面の「管理者アカウントを作成する」を開くか、`/setup` を直接開く。
- API: `POST /api/v1/setup` に `{"email": "...", "password": "..."}` を送る。

パスワードは12文字以上。作成されるアカウントは Admin であり、それ以降のアカウントは既存の Admin が「ユーザ管理」から作成する。

### `/setup` が 409 を返す

```
initial setup has already been completed
```

初期セットアップは設計上ちょうど1回だけ成功する。そうでなければ、稼働中のシステムに管理者を発行し続ける常設ルートになってしまう。すでにアカウントが存在している。

その認証情報を誰も知らない場合、これはセットアップの問題ではなくデータベースに対するパスワードリセットの問題である。2人目の「最初の管理者」を作成する経路は用意されていない。

### 本番環境で起動が拒否される

`MERLON_ENV=production` は、fail-closed な設定検証を有効にする。各メッセージが設定すべき変数を示している。

| メッセージ | 意味 |
|---|---|
| `MERLON_AUTH_ENABLED must be true in production` | 開発・デモ以外で認証を無効化することはできない |
| `MERLON_DATABASE_URL must be set in production` | インメモリストアは本番用ではなく、再起動ですべて失われる |
| `MERLON_ENCRYPTION_KEY_RING must be set in production` | 直接的な PII 属性は保存時に暗号化しなければならない |
| `MERLON_SEED must not be true in production` | シードは実システムに合成顧客データを書き込んでしまう |
| `MERLON_TRUSTED_PROXY_CIDRS must be set when MERLON_RATE_LIMIT is enabled in production` | プロキシ配下のレート制限は、どの転送ヘッダーを信頼するかを知る必要がある。そうでなければ任意のクライアントがアドレスを詐称して制限を回避できる |

これらは警告ではなく起動拒否である。`MERLON_ENV` を `production` 以外にして回避することは、露出を取り除かずにチェックだけを取り除く行為である。[設定リファレンス](../configuration.md)を参照。

### 監査権限がハードニングされていない

```
database audit privilege preflight failed
```

本番では、API は起動前に、サービング用ロールが `audit_logs` を変更できないことを検証する。アプリケーションが書き換えられる監査証跡は監査証跡ではない。

マイグレーションロールで権限を適用する。

```bash
MERLON_MIGRATION_DATABASE_URL=... make audit-harden
```

[バックアップと復元](../operations/backup-restore.md)および `docs/operations/audit-hardening.sql` を参照。

### スコアもアラートも生成されない

```
{"level":"WARN","msg":"native Go engine unavailable","error":"read CDD config: open cdd_weights.yaml: no such file or directory"}
```

スコアリングおよび取引モニタリングエンジンがルール設定を読み込めなかったため、API は起動して応答するが、一切スコアリングを行わない。これは起動失敗ではなく警告であるため見落とされやすく、その結果「アラートが生成されなかった」と読まれてしまう。これは「アラートに値する事象が無かった」とは全く異なる。

設定変数を実在するファイルに向けること（同梱の compose ファイルはこのために `./content` をマウントしている）。確認は次のとおり。

```bash
curl -s http://localhost:8080/healthz/ready
```

`checks.engine` が `ok` でなければならない。[ルール作成](../rule-authoring.md)を参照。

### PII が平文で書き込まれた

```
{"level":"WARN","msg":"MERLON_ENCRYPTION_KEY_RING not set, customer PII fields will be stored in plaintext"}
```

本番以外ではこれはエラーではなく警告であるため、長期間動かしている評価環境に平文の顧客属性が蓄積されうる。

後から鍵リングを設定しても、暗号化されるのは新規の書き込みだけであり、既存の行が遡って暗号化されることはない。この警告が出ている間に実データを投入したのであれば、それらの行は平文保存として扱い、再書き込みまたは再投入すること。[データ保持](../compliance/data-retention.md)を参照。

## 連携

### Webhook の配信が止まる

配信は最大10回まで再試行される。それを超えたイベントは破棄されるのではなく Dead Letter Queue（DLQ）へ退避するため、イベント自体は残っている。

Webhook 管理 API と「Webhooks」画面から DLQ を確認し、再処理する。継続的に失敗していた受信側は、復旧後に再送すべき DLQ の滞留を持つ。再処理は自動ではなく明示的な操作であるため、まだ壊れているエンドポイントへ誤って再送してしまうことはない。

### スクリーニング結果が古い

リスト取得に失敗した場合、Merlon は空のリストで fail-open するのではなく、最後に取得に成功したリストで照合を継続する。したがってスクリーニングは動作し続け、失敗は「結果が出ない」という形では見えない。

連続失敗はリストごとにカウントされ、3回連続で運用アラートが発報される。スクリーニング結果が想定より古いと感じた場合は、照合ロジックではなく当該リストのインポートジョブを確認すること。結果は誤っているのではなく、古い。

## それでも解決しない場合

`GET /healthz` のバージョン、`GET /healthz/ready` のレスポンス全文、起動時からのコンテナログを添えて issue を作成すること。認証情報と顧客データは事前に除去すること。
