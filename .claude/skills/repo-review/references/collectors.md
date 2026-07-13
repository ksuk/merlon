# エビデンス収集コマンド集(collectors)

監査ステップ2で実行する読み取り専用コマンド群。生出力は `.audit/evidence-<YYYYMMDD>-<shortsha>/` に保存する。
各コマンドの結果がどの評価項目の機械判定シグナルに対応するかは、標準第II部の各領域「AIエージェント向け判断基準」を参照。

実行原則:
- すべて読み取り専用。リポジトリ・環境の状態を変更しない。
- 出力が大きいものはファイルへリダイレクトし、レポートには集計値+参照パスを載せる。
- このリポジトリ(Merlon)の構成: Go=`api/`、Rust=`engine/`、Node=`ui/` `website/`、proto=`proto/`、CI=`.github/workflows/`。
  他リポジトリで使う場合はエコシステム対応表を読み替える。

## 共通(スコーピング・全域)

```bash
git rev-parse HEAD                          # 対象リビジョン固定
git status --porcelain                      # ダーティ確認
git rev-parse --is-shallow-repository       # shallow なら履歴系は NE
git ls-files | wc -l                        # 母集団サイズ(サンプリング記録用)
git shortlog -sn --no-merges --since=24.months   # 作者分布(D1/D11。個人名は役割表記へ変換)
git log --format=%s -200                    # 直近件名 200 件(D2)
git tag --sort=creatordate                  # タグ一覧(D2-5/D10-4)
git log --since=12.months --format= --name-only | sort | uniq -c | sort -rn | head -30   # 変更頻度上位=ホットスポット候補(D4/D11、サンプリング入力)
```

## D1 ガバナンス

```bash
ls LICENSE* COPYING* NOTICE* README* CONTRIBUTING* CODE_OF_CONDUCT* SECURITY* 2>/dev/null
ls .github/CODEOWNERS CODEOWNERS docs/CODEOWNERS 2>/dev/null
git log -1 --format=%ci -- CONTRIBUTING.md            # ガバナンス文書の鮮度
find . -path ./node_modules -prune -o -type f -size +500k -print | head -20   # 不要物・巨大ファイル混入(D1-6)
```

## D2 バージョン管理

```bash
git log --merges --oneline -50 | wc -l                # マージコミット比率の分子
git log --format=%s -200 | grep -Ec '^(feat|fix|docs|refactor|test|chore|perf|ci|build|style|revert)(\([^)]+\))?!?: '   # Conventional Commits 適合数(/200)
git log --shortstat --no-merges -100 | grep -E 'files? changed'   # コミットサイズ分布(原子性)
git log --format=%B -100 | grep -c '^Signed-off-by:'  # DCO 署名率
# 全履歴シークレット走査は CI の gitleaks(security.yml)結果を E3 として参照。
# ローカル実行できる場合のみ: gitleaks git --no-banner . (検出値は転記禁止)
```

## D3 アーキテクチャ

```bash
ls docs/architecture.md docs/decisions/ 2>/dev/null && ls docs/decisions/ | tail -5   # ADR の存在と最新
git log -1 --format=%ci -- docs/architecture.md       # 構造文書の鮮度
git log --since=6.months --oneline -- proto/ | head   # 外部契約(proto)の変更履歴(D3-4)
grep -rn "import" api/internal/ --include="*.go" -l | head   # 依存方向の確認入口(詳細は判断的評価)
```

## D4 実装品質

```bash
ls .golangci.yml api/.golangci.yml engine/rustfmt.toml engine/clippy.toml ui/eslint.config.* 2>/dev/null
grep -rn "nolint" api/ --include="*.go" | wc -l        # Go 抑制注釈数
grep -rEn "#\[allow\(" engine/ --include="*.rs" | grep -v target | wc -l   # Rust 抑制注釈数
grep -rEn "eslint-disable|@ts-ignore|@ts-expect-error" ui/src website/src 2>/dev/null | wc -l
grep -rEn "TODO|FIXME|HACK" api/ engine/ ui/src --include="*.go" --include="*.rs" --include="*.ts" --include="*.tsx" 2>/dev/null | wc -l
# 空 catch / エラー握りつぶし候補(ヒットは文脈確認のうえ分類):
grep -rEn "catch \{|catch \(.*\) \{ *\}" ui/src website/src 2>/dev/null | head
grep -rn "_ = " api/internal/ --include="*.go" | head   # Go の戻り値破棄候補
```

## D5 テスト

```bash
find api -name "*_test.go" | wc -l && find api -name "*.go" ! -name "*_test.go" | wc -l
grep -rn "#\[test\]" engine/crates --include="*.rs" | wc -l
find ui/src -name "*.test.*" -o -name "*.spec.*" | wc -l
grep -n "test" .github/workflows/ci.yml               # CI ゲート性(必須ステップか)
git log --no-merges --format=%H:%s -100 | grep -Ei ':fix' | head -20   # fix 系→テスト同伴率の分子抽出へ
# 上記 fix コミット各々: git show --stat <hash> でテストファイル変更の有無を数える
grep -rEn "\.skip|#\[ignore\]|t\.Skip" api engine/crates ui/src 2>/dev/null | head   # 無効化テスト
```

## D6 セキュリティ

```bash
ls SECURITY.md && head -30 SECURITY.md                # 窓口・期限・範囲の充足確認
grep -n "gitleaks\|govulncheck\|audit" .github/workflows/security.yml   # CI 検査の組込み
# 危険パターン(ヒットは候補。到達可能性を個別確認してから重大度判定):
grep -rn "InsecureSkipVerify" api/ --include="*.go"
grep -rEn "exec\.Command|os/exec" api/internal --include="*.go" | head
grep -rEn "dangerouslySetInnerHTML" ui/src | head
grep -rEn "md5|sha1" api/internal engine/crates --include="*.go" --include="*.rs" | grep -iv "sha256\|test" | head
```

## D7 サプライチェーン

```bash
ls api/go.sum engine/Cargo.lock ui/package-lock.json website/package-lock.json   # ロック存在
ls .github/dependabot.yml .github/renovate.json 2>/dev/null                      # 自動更新ボット設定
git log -5 --format=%ci -- api/go.sum engine/Cargo.lock ui/package-lock.json     # ロック更新頻度
grep -n "go 1\." api/go.mod && grep -n "edition" engine/Cargo.toml && grep -n '"node"' ui/package.json website/package.json 2>/dev/null   # ランタイム版(EOL 照合は外部照会可のときのみ)
find . -path ./node_modules -prune -o \( -name "*.jar" -o -name "*.so" -o -name "*.dll" \) -print   # 野良バイナリ
```

## D8 CI/CD

```bash
ls .github/workflows/
grep -n "uses:" .github/workflows/*.yml | grep -v "#"   # 部品参照の固定方式分類(@vN タグ / @<sha> ダイジェスト)
grep -n "go-version\|node-version\|rust-toolchain" .github/workflows/*.yml   # ツールチェーン固定
grep -n "^[a-z-]*:" Makefile                          # ローカル単一入口と CI の対応(D8-6)
```

## D9 運用性

```bash
grep -rn "/health\|/ready\|healthz" api/internal --include="*.go" | head   # ヘルスチェック実装
ls docs/operations/ && ls config.example.yaml .env.example 2>/dev/null     # 運用文書・設定例
grep -rn "MERLON_" api/internal --include="*.go" -l | head                 # 環境変数注入の確認入口
grep -rEn "fmt\.Print|println!|console\.log" api/internal engine/crates ui/src 2>/dev/null | grep -v test | wc -l   # デバッグ残骸密度
# Runbook 実在性: docs/operations/*.md 内のコマンド・パスを抽出し、実在と突合(D9 の AI 判断基準)
```

## D10 ドキュメント

```bash
head -60 README.md                                     # 要素充足(目的/前提/セットアップ/使用法)
ls CHANGELOG.md && head -30 CHANGELOG.md && git tag | tail -5   # CHANGELOG とタグの対応
make docs-check                                        # 既存文書ゲート(リンク・言語・i18n)の結果を流用
git log -1 --format=%ci -- docs/getting-started.md docs/architecture.md   # 主要文書の鮮度
```

## D11 保守性

```bash
git shortlog -sn --no-merges --since=24.months -- api/ | head -5    # 領域別集中度(engine/ ui/ でも同様に)
git log --format=%s -300 | grep -c '^refactor'         # リファクタ継続性
grep -rn "Deprecated\|deprecated" api/internal engine/crates --include="*.go" --include="*.rs" | head   # 非推奨滞留(blame で経年確認)
```

## D12 コンプライアンス(regulated プロファイル時)

```bash
git log --format=%s -100 | grep -Ec '#[0-9]+|[A-Z]+-[0-9]+'   # チケット/Issue 参照率
ls docs/compliance/                                    # 規制マッピング文書(fsa-guideline-mapping 等)
git log -1 --format=%ci -- docs/compliance/            # マッピング鮮度
# 逆方向トレース: 直近タグ → git log <prev>..<tag> --oneline → 各変更の参照連鎖を確認(標準 D12 確認方法)
ls migrations/ && grep -rn "retention\|保持" docs/compliance/ | head   # 保持方針と実装痕跡
```
