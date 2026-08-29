---
title: テストガイド
---

# テストガイド

Merlon は Go / TypeScript の各レイヤでテストを持つ。本文書は各テストの実行方法とテスト戦略の方針を示す。

## コンポーネント別の実行

### Go (API)

```bash
cd api && go test ./...
```

カバレッジ付き:

```bash
cd api && go test -cover ./...
```

### UI (TypeScript / React)

```bash
cd ui && npm run test
```

## 全体実行

```bash
make test
```

`make test` は Go テストと UI テストを実行する。CI でも同じターゲットを使用する。

## PostgreSQL 統合テスト

PostgreSQL 統合ゲートは、未使用の専用データベースに対して実行する。

```bash
MERLON_DATABASE_URL=postgres://merlon:<password>@127.0.0.1:5432/merlon \
  make test-integration
```

このターゲットは、全migrationを2回適用した後に全Go testを実行する。統合testのpackageは専用データベースを共有するため、`go test -p=1`でpackageを直列実行し、あるpackageのcleanupやretention処理が別packageのfixtureを変更することを防ぐ。packageやtestの省略は行わない。

他のprocessやtest runが使用しているデータベースへ、このターゲットを向けてはならない。実行ごとに未使用のデータベース、または新しいCompose projectとvolumeを作成する。

## テスト戦略の方針（TDD）

機能実装・バグ修正は TDD（テスト駆動開発）で進める。

1. **テスト作成** — 期待する振る舞いを表すテストを先に書く（失敗を確認）
2. **実装** — テストを通す最小限のコードを書く
3. **リファクタリング** — テストが通る状態を保ちつつ整理する

### レイヤ別の重点

- **ネイティブエンジン（Go）** — CDD スコアリング・TM 評価・スクリーニング・バックテストはビジネスロジックの核。判断根拠の再現性（Auditability First）を担保するため、同一入力に対する出力の決定性をテストで固定する
- **API（Go）** — CRUD とオーケストレーション。サービス境界の契約と、エラー時にアラート側へ倒れること（Fail-Alert）を検証する
- **UI** — コンポーネント単位のテストと、調査ワークフローのユーザーフロー

### 既存パターンへの準拠

新規テストは各ディレクトリの既存テストフレームワーク・命名規則・パターンに従う。Go は標準 `testing`、UI は既存のテストランナー設定を踏襲する。
