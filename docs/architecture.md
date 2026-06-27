# Architecture

Merlon は日本の非銀行金融業向け AML/CFT（マネー・ローンダリング／テロ資金供与対策）セルフホスト型ソフトウェアである。本文書はシステム全体の構成と設計判断の要点を述べる。

## コンポーネント図

```
                          ┌─────────────────┐
  外部システム            │   React UI      │
 (基幹/取引/KYC) ──┐      │ (TypeScript)    │
                   │      └────────┬────────┘
                   │               │ REST / gRPC-Web
                   ▼               ▼
            ┌──────────────────────────────────┐
            │        Ingestion Layer            │
            │   (取引・顧客データ取り込み)        │
            └──────────────┬───────────────────┘
                           │
                           ▼
            ┌──────────────────────────────────┐
            │           Go API                  │
            │  ┌────────────┐ ┌──────────────┐  │
            │  │ Customer   │ │ Transaction  │  │
            │  │ Service    │ │ Service      │  │
            │  ├────────────┤ ├──────────────┤  │
            │  │ Case       │ │ Report       │  │
            │  │ Service    │ │ Service      │  │
            │  └────────────┘ └──────────────┘  │
            └──────────────┬───────────────────┘
                           │ gRPC (Protocol Buffers)
                           ▼
            ┌──────────────────────────────────┐
            │          Rust Engine              │
            │  ┌────────────┐ ┌──────────────┐  │
            │  │ CDD        │ │ TM           │  │
            │  │ Scoring    │ │ Evaluation   │  │
            │  ├────────────┤ ├──────────────┤  │
            │  │ Screening  │ │ Backtest     │  │
            │  └────────────┘ └──────────────┘  │
            └──────────────┬───────────────────┘
                           │
                           ▼
            ┌──────────────────────────────────┐
            │           Data Layer              │
            │  PostgreSQL │ Redis │ Object Store │
            └──────────────────────────────────┘
```

## 各コンポーネントの役割

### Ingestion Layer
外部の基幹システム・取引システム・KYC ベンダーからの顧客／取引データを受け取り、内部スキーマへ正規化する。Adapter Isolation 原則に基づき、外部連携の差異はこの層で吸収する。

### Go API
顧客・取引・ケース・レポートの CRUD と業務オーケストレーションを担当する。

- **Customer Service** — 顧客プロファイル、リスクティアの管理
- **Transaction Service** — 取引データの受領・永続化・モニタリング起動
- **Case Service** — アラートからのケース生成、調査ワークフロー
- **Report Service** — STR（疑わしい取引の届出）エクスポート、監査レポート

### Rust Engine
計算負荷の高いルール評価を担当する。

- **CDD Scoring** — 顧客リスクスコアの算出（システムの中心軸。[ADR-0004](decisions/0004-score-driven-architecture.md) 参照）
- **TM Evaluation** — 取引モニタリングシナリオの評価
- **Screening** — 制裁対象者・PEP リストとの照合
- **Backtest** — ルール変更の影響をヒストリカルデータで検証

### Data Layer
- **PostgreSQL** — 顧客・取引・ケース・スコア履歴・監査ログの永続化
- **Redis**（オプション）— スクリーニングリスト等のキャッシュ
- **Object Storage** — レポート成果物、エクスポートファイル

## Go / Rust 分割の理由

CRUD／API 開発は Go の生産性が高く、ルール評価・バックテストの計算負荷は Rust が適している（GC なし・メモリ安全）。両者を gRPC で分離することで、計算負荷を API のレイテンシから隔離する。詳細は [ADR-0002](decisions/0002-go-rust-hybrid.md) を参照。

## gRPC による型安全な通信

Go と Rust の境界は Protocol Buffers で定義された契約である。Proto 定義が両言語間のコントラクトとなり、エンジンの実装言語変更は API 側に影響しない。Proto のワークフローは [proto-workflow.md](development/proto-workflow.md) を参照。

## 設計原則

1. **Auditability First** — 全判断根拠を再現可能に記録する
2. **Configuration as the Product** — ルールは JSON/YAML 設定として表現する
3. **Score-Driven Architecture** — CDD スコアが中心軸（[ADR-0004](decisions/0004-score-driven-architecture.md)）
4. **Adapter Isolation** — 外部連携はアダプタ層で抽象化する
5. **Secure by Default** — デフォルトは安全側に倒す
6. **Contract Stability** — 外部契約は後方互換を 12 ヶ月以上維持する
7. **Fail-Alert** — 障害時はアラート側に倒す（見逃しより誤検知を許容）
