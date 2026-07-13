---
title: リポジトリガバナンス
---

# リポジトリガバナンス

変更は `main` 以外のブランチから GitHub プルリクエストで提案する。必須ゲートは `make lint`、`make test`、`make docs-check` である。

ルールの有効化・無効化は、作成者本人ではない別の Admin が実行する。データベースマイグレーションは専用の migration role を使用し、API role は `audit_logs` を更新・削除できないことを起動前に検証する。

GitHub Free の非公開リポジトリでは required reviewer のブランチ保護を設定できない場合がある。その間は CODEOWNERS、CI、プルリクエスト手順、二人の Admin による運用で補完し、本番リリース前に再確認する。
