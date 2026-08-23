---
description: Run a Codex technical-design review of Claude's immediately preceding response
disable-model-invocation: true
allowed-tools: Bash(bash .claude/scripts/codex-review-last.sh *)
---

Do not use this command for content involving pricing, business strategy, secret keys, customer data, or other confidential information.

!`bash .claude/scripts/codex-review-last.sh "${CLAUDE_SESSION_ID}" --technical-design-only`
