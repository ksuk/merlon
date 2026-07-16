---
title: 変更のトレーサビリティ
---

# 変更のトレーサビリティ

実装変更では、公開要件、設計判断、テスト、運用上の証跡をプルリクエストに記載する。非公開の計画資料や顧客データは証跡として扱わない。

各プルリクエストでは、次を満たす。

- `Requirement / issue` に `#<番号>` または公開 GitHub Issue の URL を記載する。
- `Public ADR or design reference` にリポジトリ内の公開文書を記載する。設計判断が不要な場合に限り、理由を付けた `N/A — <理由>` を使用できる。
- Bot を除く全コミットに `Refs #<番号>` フッターを付ける。
- 検証証跡と、ロールバックまたはマイグレーションへの影響を記載する。

`scripts/check-traceability.sh` は `Traceability / Required` チェックでこれらを検証する。保護対象 `main` の Ruleset では、CI、Security、DCO、独立承認、CODEOWNERS レビュー、全レビュー会話の解決も必須にする。`scripts/configure-github-ruleset.sh` は確認可能な設定内容を既定で表示するだけで、明示的に `--apply` を指定した場合のみ GitHub を変更する。

| 変更領域 | 実装 | 検証 |
|---|---|---|
| CDD スコアリング・TM・スクリーニング | `api/internal/engine/native` | Go テストと CI |
| トランザクション監視とリカバリ | `api/internal/batch`、`api/internal/engine/native` | バッチ／リカバリテストとメトリクス |
| API・データストア | `api/internal/server`、`api/internal/store` | API／ストアテスト |
| 監査保持と整合性 | `migrations/`、監査ストア | マイグレーション検証と `merlon-audit verify` |
| 文書とリリースゲート | `.github/workflows`、website checks | CI、`make docs-check`、SBOM、リリース来歴証跡 |

新しい要件を追加する場合は、公開ドキュメントと検証方法を同時に追加する。リリースマニフェストにはタグ、コミット、イメージダイジェスト、SBOM、provenance の関係を保存し、マージ後も変更連鎖を再構成できるようにする。
