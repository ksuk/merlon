---
title: 概要
sidebar_position: 0
---

# 概要

Merlon は、日本のノンバンク金融機関向けの自己ホスト型 AML/CFT ソフトウェアです。
顧客リスク評価（CDD）、取引モニタリング（TM）、制裁リスト・PEP リストとの照合、
および検知結果であるアラートとケースの管理を行います。これらはすべて、自組織の
インフラストラクチャ上で、自組織の PostgreSQL データベースに対して動作します。

このドキュメントは「何をしたいか」で構成されています。ご自身の役割に合うトラックを
選んでください。各トラックは上から順に読める順序で並んでいます。

## Merlon を理解する

役割にかかわらず、まずここから始めてください。

- [はじめに](getting-started.md) — Docker Compose で全構成を約5分で起動する
- [Merlon デモツアー](demo-tour.md) — 投入済みデータセットで、スコアリングから
  アラート、ケース解決までを一通り体験する
- [アーキテクチャ](architecture.md) — Go API・ネイティブエンジン・React UI の
  関係と、CDD スコアが中心軸である理由
- [よくある質問](faq.md) — Merlon が何であるか、および評価時に最も多く問われる
  設計判断

## コンプライアンスと統制

コンプライアンス担当者・第2線のレビュー担当者向け。Merlon が規制上の義務に対して
何を行い、どのような証跡を残すか。

- [規制上のスコープ](compliance/regulatory-scope.md) — Merlon が対応する義務と、
  意図的に対応しない義務
- [金融庁ガイドライン対応表](compliance/fsa-guideline-mapping.md) — Merlon の統制と
  金融庁 AML/CFT ガイドラインの対応関係
- [データ保持ポリシー](compliance/data-retention.md) — 保持期間と根拠法令
- [ケース管理ワークフロー](case-management.md) — アラートからケースへのライフサイクルと
  疑わしい取引の届出（STR）経路
- [認可と職務分掌](auth.md) — ロール、権限、および二重統制のポイント

## 設定とチューニング

Merlon の検知の厳しさを決める担当者向け。すべてのルールはコードではなく設定ファイルです。

- [設定リファレンス](configuration.md) — 環境変数と `config.yaml`
- [ルール作成ガイド](rule-authoring.md) — CDD ウェイト、カントリーリスクテーブル、
  TM シナリオの記述と、安全な変更の展開手順

## デプロイと運用

本番環境で Merlon を稼働させる担当者向け。

- [デプロイ手順](operations/deployment.md) — 本番構成とロールアウト
- [コンテナイメージ](operations/container-images.md) — 何が公開されるか、各タグが
  何を保証するか、`latest` が存在しない理由
- [API / ワーカーモード](operations/worker-mode.md) — API とバックグラウンドジョブ処理の分離
- [初期データ移行](operations/initial-migration.md) — 既存の顧客マスタと取引履歴を
  ファイルから投入する
- [アップグレード手順](operations/upgrade.md) — 新しいリリースへの移行と、
  デプロイ内容の検証
- [バックアップとリストア手順](operations/backup-restore.md)
- [パーティショニング・キャパシティ運用ガイド](operations/partitioning-guide.md)
- [依存関係のライフサイクル](operations/dependency-lifecycle.md) — サポート対象の
  ランタイムバージョンと EOL 管理
- [リリースノート](release-notes.md) — 各バージョンの変更点
- [トラブルシューティング](troubleshooting/index.md) — 実際に表示されている
  エラーメッセージから引く症状の逆引き

## セキュリティと保証

セキュリティレビュー担当、ベンダーリスク評価担当、内部監査向け。

- [セキュリティと保証](security/index.md) — 要点と、詳細の所在
- [データ送出](security/data-egress.md) — Merlon が行いうる外向き通信のすべてと、
  その契機
- [サプライチェーン](security/supply-chain.md) — 固定、スキャン、リリース来歴、
  および既知のギャップ
- [受容リスク](security/accepted-risks/index.md) — Merlon が意図的に行わないことと、
  その補完統制

## 連携と拡張

Merlon を基幹系システムなどの上流システムに接続する開発者向け。

- [アダプタガイド](adapter-guide.md) — 連携元システムを Merlon のデータモデルに
  マッピングする
- [REST API リファレンス](api/openapi.md) — ルート定義から生成された全エンドポイント
- [ルールスキーマ](api/schema/index.md) — すべてのルール設定ファイルが検証される
  JSON Schema

## 開発に参加する

Merlon 自体を変更する方向け。

- [開発環境セットアップ](development/setup.md)
- [テストガイド](development/testing.md)
- [ドキュメントガイド](development/documentation.md) — 本ドキュメントの記述・翻訳・
  チェックの方法
- [変更トレーサビリティ](development/change-traceability.md)
- [リポジトリガバナンス](development/repository-governance.md)
- [リリースチェックリスト](development/release-checklist.md)
