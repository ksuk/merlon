---
title: 性能検証証跡
---

# 性能検証証跡

Merlonには、稼働中のリリース候補に対して取引取込みと同期monitoringを再現測定する、localhost限定のGo製harnessが含まれる。harnessは`POST /api/v1/transactions`を送信するため、成功したrequestには取引の保存、監査mutation、およびresponse前に完了するrealtime monitoring passが含まれる。

これはrelease evidenceであり、すべての環境に共通するbenchmarkではない。出力と一緒にhostおよびcontainer resourceを記録し、あるmachineの結果を異なるsizeのdeploymentへそのまま適用してはならない。

## 安全性とdata範囲

commandが受け付けるのは`localhost`、またはGoが`IsLoopback`と判定するIP addressだけである。環境変数のHTTP proxyを無効化し、redirect先もすべて検証する。loopback以外の宛先、URL内credential、HTTP以外のscheme、base pathは、request送信前に拒否する。

harnessは固定の合成fixtureから専用のcustomerとtransactionを作成する。作成recordは後から削除しないため、freshな専用databaseに対してだけ実行すること。production databaseへ向けてはならない。

## 前提条件

1. exact release-candidate commitをcheckoutし、full SHAを記録する。
2. fresh databaseを使うstandard topologyをloopback portで開始する。image build時に同じSHAを`REVISION`として渡す。harnessはlive `/api/v1/system/status`のcommitが一致しないtargetを拒否する。
3. repository付属の合成policy fixtureでtransaction-monitoring engineを構成し、live system statusで`api`、`database`、`engine`がconfiguredかつ`ready`であることを確認する。いずれかが存在しない、またはreadyでないtargetはharnessが拒否する。
4. 初回setupを完了し、一時的なAnalyst API keyを作成する。値は`MERLON_PERF_BEARER_TOKEN`だけに保持し、reportには含めない。localの認証無効demo topologyで測定する場合だけ未設定にする。
5. JSON出力と一緒にhost CPU、memory、OS、Docker version、image digest、PostgreSQL version、container resource limitを記録する。harnessは自身のGo runtime環境を記録するが、host limitは推測できない。

再現可能なbuild手順の一例を示す。

```bash
release_commit="$(git rev-parse HEAD)"
docker compose build \
  --build-arg VERSION="v0.0.1-candidate" \
  --build-arg REVISION="$release_commit" \
  --build-arg BUILT_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
docker compose up --no-build --detach --wait
```

別のlocal stackが動作中なら、専用のCompose project名とdefault以外のloopback host portを使用する。必要なdevelopment credentialはstandard deploymentとsetupの文書に従い、evidence fileやshell historyへ書き込まないこと。

## 実行

一時API keyを表示せず環境変数へ設定した後、次を実行する。

```bash
make performance-evidence \
  PERF_BASE_URL="http://127.0.0.1:18055" \
  PERF_EXPECTED_COMMIT="$release_commit" \
  PERF_REQUESTS=1000 \
  PERF_CONCURRENCY=16 \
  PERF_CUSTOMERS=16 \
  PERF_WARMUP=100 \
  > performance-evidence.json
```

setup用customer requestとwarmup transactionは測定区間から除外する。測定transactionは複数の合成customerへ分散し、1 customerへのserial trafficではなく、portfolio上の並行活動を表す。

JSON reportは次を記録する。

- targetのversion、exact commit、取得可能な場合のbuild timestamp、認証mode、base currency、必須componentのreadiness
- harnessのbuild情報とGo runtime環境
- customer、warmup、request、concurrencyの件数
- 開始・終了timestampと測定時間
- response status別件数、transport error、error rate、成功throughput、成功requestのP50/P95/P99 latency

測定またはwarmupに失敗が1件でもあればcommandは非0で終了する。JSONと外部のhost/container説明は一緒に保持すること。結果が他文書の表現を裏付けない場合は、表現を変更するか実装を改善する。存在しないpercentileやthroughput値を推測で補ってはならない。
