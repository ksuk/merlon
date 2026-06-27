#!/usr/bin/env bash
set -euo pipefail

if ! command -v buf &> /dev/null; then
  echo "Error: buf is not installed."
  echo "Install: npm install -g @bufbuild/buf"
  echo "Or visit: https://buf.build/docs/installation"
  exit 1
fi

cd "$(dirname "$0")/../proto"
buf lint
buf generate
echo "Proto generation complete."
