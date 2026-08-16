---
title: コンテナイメージ
---

# コンテナイメージ

Merlon は GitHub Container Registry に単一のイメージを公開する。

```
ghcr.io/ksuk/merlon
```

このイメージには API サーバーとオペレーター UI が含まれる。UI は Go バイナリが `MERLON_UI_DIR` から配信するため、独立した Web サーバーコンテナは存在しない。PostgreSQL は同梱しておらず、利用者が用意する。

## タグ

| タグ | 指すもの | 不変か | 適する用途 |
|---|---|---|---|
| `vX.Y.Z` | リリース | はい | 自組織での評価を経た任意のデプロイ |
| `@sha256:...` | 特定のビルドそのもの | はい | 固定したいあらゆるデプロイ |

意図的に存在しないものが2つある。

**`latest` タグは無い。** 可変タグは、同じ `docker pull` を別の日に実行した2台のホストが、同じバージョンを報告しながら異なるソフトウェアを動かしうることを意味する。規制記録を生成するシステムにおいて、これは擁護できる立場ではない。ここでのリリース同一性はダイジェストであり、バージョンタグはそれを指す利便性にすぎない。

**ローリングな `main` / `dev` タグも、プレリリースチャネルも無い。** 公開されるすべてのイメージは、`main` の祖先である注釈付きの保護されたタグに対応し、かつ `CHANGELOG.md` の節に対応する。ブランチの先頭から公開されるものは何も無く、プレリリース識別子を持つタグは公開されずに拒否される。

### リリースタグが主張することと、しないこと

かつてここには、プロジェクトのガバナンス統制が未整備であることをタグ名で伝えることを役割とする第2のチャネルがあった。タグのサフィックスはそれを伝える手段としては弱い。読み飛ばされるし、意味は結局ドキュメントの中にしかない。同じ事実は現在、問い合わせ可能な形で成果物とともに配られる。

本プロジェクトのメンテナは1名である。したがって `vX.Y.Z` イメージが主張するのは、リリースコミット上で `CI Required` と `Security Required` が通過したことであり、これはイメージのビルド前にリリースワークフローが検証する。ADR-0016 以降にマージされた PR は加えて `Governance Required` が強制するセルフレビュー証跡を持つが、その証跡は PR 上にあり、リリース側では再検証しない。独立した承認と職務分離は**主張しない**。1人が自分の作業を独立してレビューすることはできないためであり、その事実はイメージ自身が述べる。

```bash
docker inspect ghcr.io/ksuk/merlon:v0.1.0 \
  --format '{{json .Config.Labels}}' | jq 'with_entries(select(.key | startswith("io.github.ksuk.merlon.governance")))'
```

```json
{
  "io.github.ksuk.merlon.governance.mode": "single-maintainer",
  "io.github.ksuk.merlon.governance.independent-approval": "false",
  "io.github.ksuk.merlon.governance.separation-of-duties": "false",
  "io.github.ksuk.merlon.governance.adr": "ADR-0016"
}
```

同じ4項目は、すべての GitHub Release の `release-manifest.json` と、リリースノート冒頭のヘッダにも記載される。ビルド自体が弱くなるわけではない。マルチアーキテクチャビルド、来歴証明、SBOM、証跡マニフェストは他のどのリリースとも同一である。

それが導入に足るかどうかは、自組織の規制上の義務に照らして判断すべき事項である。AML/CFT システムにおいては、その判断はベンダーが何を主張していようと当局に対して説明できるものでなければならない。開示の背景は[単独メンテナモード](../development/repository-governance.md)と ADR-0016 に記載している。

## 取得

タグで取得して内容を確認し、ダイジェストで再デプロイする。

```bash
docker pull ghcr.io/ksuk/merlon:v0.1.0
docker inspect --format '{{index .RepoDigests 0}}' ghcr.io/ksuk/merlon:v0.1.0
```

実行前に検証すること。各リリースには `release-manifest.json`、`sbom-image.cdx.json`、`SHA256SUMS` が添付され、イメージには GitHub のビルド来歴証明が付与されている。検証手順は[アップグレード](upgrade.md)にある。

## アーキテクチャ

`linux/amd64` と `linux/arm64`。両者は単一のビルドから生成される。Go バイナリはクロスコンパイルされ、UI バンドルはアーキテクチャに依存しないため、いずれのアーキテクチャもエミュレーション下では生成されず、どちらも二級のビルドではない。

## イメージの内容

| 項目 | 値 |
|---|---|
| ベース | `alpine`（タグとダイジェストで固定） |
| 実行ユーザー | uid/gid `10001`、非 root |
| 必要な書き込み先 | 無し。`--read-only` のまま動作する |
| 公開ポート | `8080` |
| ヘルスチェック | `GET /healthz/live`（liveness。`MERLON_MODE` が選択した listener を尊重） |
| 外向き通信 | 設定したものだけ。[データ送出](../security/data-egress.md)を参照 |

組み込みのヘルスチェックは liveness プローブであるため、新規のコンテナはプロセスが応答した時点で `healthy` になる。readiness（初期セットアップの完了、データベースへの到達、エンジンのロード）は `GET /healthz/ready` として別に公開されており、オーケストレーターのプローブや、「利用可能な状態」を意図的に条件にする compose のヘルスチェックはこちらを使う。

`worker` モードでは `MERLON_WORKER_HTTP_ADDR`（既定 `:8081`）を、`api` と `all` モードでは `MERLON_HTTP_ADDR`（既定 `:8080`）を probe に使用する。wildcard、IPv4／IPv6、host-qualified の listen address は、probe の実行前に有効な loopback または host URL へ正規化される。

すべての状態は PostgreSQL にある。コンテナはデータを保持しないため、コンテナの置き換えがデータ損失になることはない。バックアップが必要な対象は[バックアップと復元](backup-restore.md)を参照。

### イメージのメタデータ

標準的な OCI アノテーションを設定している。`org.opencontainers.image.revision` は、取得したイメージを、その来歴証明が発行された対象のコミットに結び付ける。

```bash
docker inspect ghcr.io/ksuk/merlon:v0.1.0 \
  --format '{{json .Config.Labels}}'
```

## 自分でビルドする

イメージはリポジトリから再現可能であり、BUSL の許諾範囲内である。

```bash
docker build -f api/Dockerfile \
  --build-arg VERSION=local \
  --build-arg REVISION="$(git rev-parse HEAD)" \
  -t merlon:local .
```

`VERSION` を渡すことには意味がある。渡さない場合、バイナリは `GET /healthz` で `dev` を報告する。自らのバージョンを名乗れないイメージは、監査証跡の中に見つけたいものではない。
