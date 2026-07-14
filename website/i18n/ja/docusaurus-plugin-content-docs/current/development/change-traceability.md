---
title: 変更のトレーサビリティ
---

# 変更のトレーサビリティ

実装変更では、公開要件、設計判断、テスト、運用上の証跡をプルリクエストに記載する。非公開の計画資料や顧客データは証跡として扱わない。

| 変更領域 | 実装 | 検証 |
|---|---|---|
| CDD スコアリング・TM・スクリーニング | `api/internal/engine/native` | Go テストと CI |
| トランザクション監視とリカバリ | `api/internal/batch`、`api/internal/engine/native` | バッチ／リカバリテストとメトリクス |
| API・データストア | `api/internal/server`、`api/internal/store` | API／ストアテスト |
| 監査保持と整合性 | `migrations/`、監査ストア | マイグレーション検証と `merlon-audit verify` |
| 文書とリリースゲート | `.github/workflows`、website checks | CI、`make docs-check` |

新しい要件を追加する場合は、公開ドキュメントと検証方法を同時に追加する。
