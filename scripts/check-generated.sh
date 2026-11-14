#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

go generate ./...
pnpm --recursive generate

generated_paths=(
  apps/web/src/api/generated
  packages/contracts
  protocols
)

if [[ -n "$(git status --porcelain --untracked-files=all -- "${generated_paths[@]}")" ]]; then
  git diff -- "${generated_paths[@]}"
  git status --short --untracked-files=all -- "${generated_paths[@]}"
  echo "Generated sources are stale. Regenerate and commit them." >&2
  exit 1
fi

echo "Generated source check passed."
