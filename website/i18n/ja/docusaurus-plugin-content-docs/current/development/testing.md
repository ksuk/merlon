# Testing Guide

Merlon は Go / Rust / TypeScript / Proto の各レイヤでテストを持つ。本文書は各テストの実行方法とテスト戦略の方針を示す。

## コンポーネント別の実行

### Go (API)

```bash
cd api && go test ./...
```

カバレッジ付き:

```bash
cd api && go test -cover ./...
```

### Rust (Engine)

```bash
cd engine && cargo test
```

### UI (TypeScript / React)

```bash
cd ui && npm run test
```

### Proto (Protocol Buffers)

```bash
cd proto && buf lint
```

破壊的変更の検出:

```bash
cd proto && buf breaking --against '.git#branch=main'
```

## 全体実行

```bash
make test
```

`make test` は Go テスト・Rust テスト・UI テスト・`buf lint` をまとめて実行する。CI でも同じターゲットを使用する。

## テスト戦略の方針（TDD）

機能実装・バグ修正は TDD（テスト駆動開発）で進める。

1. **テスト作成** — 期待する振る舞いを表すテストを先に書く（失敗を確認）
2. **実装** — テストを通す最小限のコードを書く
3. **リファクタリング** — テストが通る状態を保ちつつ整理する

### レイヤ別の重点

- **Engine（Rust）** — CDD スコアリング・TM 評価・スクリーニングはビジネスロジックの核。判断根拠の再現性（Auditability First）を担保するため、同一入力に対する出力の決定性をテストで固定する
- **API（Go）** — CRUD とオーケストレーション。サービス境界の契約と、エラー時にアラート側へ倒れること（Fail-Alert）を検証する
- **Proto** — 契約安定性（Contract Stability、後方互換 12 ヶ月以上）を `buf breaking` で機械的に保証する
- **UI** — コンポーネント単位のテストと、調査ワークフローのユーザーフロー

### 既存パターンへの準拠

新規テストは各ディレクトリの既存テストフレームワーク・命名規則・パターンに従う。Go は標準 `testing`、Rust は `#[cfg(test)]` モジュール、UI は既存のテストランナー設定を踏襲する。
