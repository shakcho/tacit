#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

go generate ./...
pnpm --recursive generate

if ! git diff --quiet -- packages/contracts protocols; then
  git diff -- packages/contracts protocols
  echo "Generated sources are stale. Regenerate and commit them." >&2
  exit 1
fi

echo "Generated source check passed."
