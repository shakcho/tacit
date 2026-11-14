#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

go generate ./...
pnpm --recursive generate

orval_output="apps/web/src/api/generated"
tracked_orval_output="$(git ls-files -- "$orval_output")"
if [[ -n "$tracked_orval_output" ]]; then
  echo "$tracked_orval_output"
  echo "Orval output is disposable and must not be committed." >&2
  exit 1
fi

if ! git check-ignore --quiet "$orval_output/tacit.ts"; then
  echo "$orval_output must be ignored by Git." >&2
  exit 1
fi

test -s "$orval_output/tacit.ts"
test -s "$orval_output/models/index.ts"

if ! rg --quiet 'useCreateShellSample' "$orval_output/tacit.ts"; then
  echo "Orval did not generate the expected TanStack Query hooks." >&2
  exit 1
fi

echo "Generated source check passed; Orval output is disposable."
