---
title: ドキュメント運用ガイド
sidebar_position: 12
---

# ドキュメント運用ガイド

本ガイドは、Merlon のドキュメントがどのように構成・翻訳・検査されるか、及びローカルでチェックを実行する方法を説明する。

## 正典言語とレイアウト

英語が、リポジトリルートの `docs/` 配下にあるすべての手書きドキュメントの正典言語である。他のすべてのロケールはその原文の翻訳である。

- `docs/decisions/**`（アーキテクチャ決定記録）は構築済みサイトから完全に除外され、日本語で記述されている。翻訳対象ではなく、ドキュメントチェックもこれをスキップする。
- `docs/api/**` は生成されたリファレンス出力（OpenAPI、gRPC proto、JSON Schema のリファレンスページ）であり、gitignore されている。直接編集してはならない。`make generate-openapi`、`make generate-proto-docs`、及びウェブサイトのスキーマドキュメントジェネレータによって上書きされる。このパイプラインの proto 側については[Protocol Buffers ワークフロー](./proto-workflow.md)を参照。

## ja 翻訳

日本語訳は `website/i18n/ja/docusaurus-plugin-content-docs/current/` に、`docs/` 配下の各英語ドキュメントの相対パスをそのまま反映する形で配置される。リファレンスジェネレータ（OpenAPI、proto、スキーマ）は、そのソースとなるコメント・スキーマから `en` と `ja` の両方のリファレンスページを直接生成するため、`docs/api/**` とその ja 版は自動的に同期され、翻訳チェックの対象外である。

まだ実際の翻訳が存在しない手書きの英語ドキュメントは、ビルド時に `website/scripts/sync-i18n-fallback.mjs` によって英語のフォールバックコピーで埋められる。これにより、ja ロケール内のページ間の相対リンクが 404 になることはない。フォールバックコピーは `website/.i18n-fallback-manifest.json` に記録され、実際の翻訳では**ない**。以下のチェックはこれらを未翻訳の場合と同様に扱う。

## 翻訳ワークフロー規則

`docs/` 配下の手書き英語ドキュメント（すなわち `docs/api/**` や `docs/decisions/**` を除く）を変更するプルリクエストは、以下のいずれかを行わなければならない。

1. 同じ PR 内で `website/i18n/ja/docusaurus-plugin-content-docs/current/` 配下の対応する ja ファイルを更新する、または
2. `website/docs-lint/i18n-freshness-acks.json` にエントリを追加し、この英語版の改訂について ja 翻訳が意図的に未更新であることを承認する（詳細は後述）。

これにより、翻訳が英語原文の背後で気づかれないまま陳腐化することを防ぐ。

## 5つのチェック

`node website/scripts/checks/run-all.mjs` は以下の5つのチェックすべてを実行し、合否表を出力する。いずれかのチェックが失敗すると非ゼロで終了する。`make docs-check` で実行できる。

### 1. `check-en-language`

手書きの英語ドキュメント、または `proto/merlon/v1/*.proto` のどこかに日本語文字が出現すると失敗する。許容される例外（例えば、意図的に引用された日本の法令名など）は `website/docs-lint/jp-allowlist.json` に、完全一致部分文字列のエントリの配列として列挙する。

```json
[{ "file": "docs/compliance/data-retention.md", "text": "<exact substring to allow>" }]
```

**陳腐化エントリの規則**: allowlist エントリの `text` がその `file` にもはや存在しない場合、チェックは失敗する。allowlist は常に実在するテキストを記述していなければならず、そうでなければ何も保護しないまま黙って形骸化する。

### 2. `check-title-style`

手書きドキュメント及び `_category_.json` のラベルについて: フロントマターの `title` が存在しなければならず、ドキュメントに H1 がある場合はそれと完全に一致しなければならない。タイトル、フロントマターの `sidebar_label`、カテゴリの `label` はタイトルケース（先頭または末尾でない限り、a, an, the, and, or, nor, but, for, of, in, on, to, at, by, with, vs, via といった小語を除くすべての単語を大文字化）でなければならない。`website/docs-lint/proper-nouns.json` に列挙された固有名詞（gRPC、OpenAPI、API、CDD、JWT など）は、そこに綴られた通り正確に表記しなければならない。

### 3. `check-i18n-parity`

すべての手書き英語ドキュメントは、同じ相対パスに**実際の**翻訳である ja 対応ファイルを持たなければならない。すなわち、存在すること、英語原文とバイト単位で同一でないこと、マニフェスト上のフォールバックコピーでないこと、最初の H1 に日本語文字を含むことが必要である。例外（意図的に未翻訳のままにするドキュメント）は `website/docs-lint/i18n-exceptions.json` に、`docs/` 相対パスの配列として記載する。

**陳腐化エントリの規則**: もはや手書きの英語ドキュメントでないパスに対する例外はチェックを失敗させる。

### 4. `check-i18n-freshness`

実際の en/ja 翻訳ペアそれぞれについて、最終コミット時刻（`git log -1 --format=%ct`）で比較したとき、ja ファイルは英語原文より古くてはならない。英語ファイルに未コミットのローカル変更がある場合、それはコミット済みの ja 翻訳より常に新しいものとして扱われる。両側が未コミットの新規ペア（1つの PR でまとめて追加されたもの）は通過する。

陳腐化したペアは `website/docs-lint/i18n-freshness-acks.json` で承認できる。`docs/` 相対パスを、英語ファイルの現在の HEAD コミットハッシュ（`git log -1 --format=%H -- <file>`）にマッピングする。

```json
{ "getting-started.md": "a1b2c3d4..." }
```

**陳腐化エントリの規則**: ハッシュが英語ファイルの現在の HEAD ハッシュと一致しない ack はチェックを失敗させる。以降の英語側の変更のたびに再確認する（または翻訳を更新する）必要がある。

### 5. `check-ui-translations`

Docusaurus の UI 文字列翻訳ファイルが存在し、有効な JSON であることを確認する: `website/i18n/ja/code.json`、`website/i18n/ja/docusaurus-plugin-content-docs/current.json`、`website/i18n/ja/docusaurus-theme-classic/navbar.json`、及び `.../footer.json`。すべての `message` 値は日本語文字を含まなければならず、含まない場合は `website/docs-lint/ui-string-allowlist.json` に許可されたトークンのみで構成されていなければならない（GitHub、Merlon、REST API のようなブランド名・略語、数字・句読点はメッセージの判定に影響しない）。

## チェックの実行

```bash
make docs-check
```

これは `node website/scripts/checks/run-all.mjs` と等価であり、npm install は不要である。各チェックは依存関係のない Node ESM スクリプトである。CI は `docs-deploy.yml` の「Docs checks」ステップで、サイトビルドの前に、フレッシュネスチェックがコミットタイムスタンプを比較できるようフル git 履歴（`fetch-depth: 0`）を用いて同じコマンドを実行する。
