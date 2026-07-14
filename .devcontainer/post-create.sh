#!/usr/bin/env bash
set -euo pipefail

echo "==> Fetching project dependencies"
(cd api && go mod download)
(cd ui && npm ci)

echo "==> Installing Claude Code CLI (latest)"
curl -fsSL https://claude.ai/install.sh | bash -s -- latest

echo "==> Installing Codex CLI (latest)"
npm install -g @openai/codex

echo "==> Installing Wrangler CLI (latest)"
npm install -g wrangler

echo "==> post-create.sh done"
