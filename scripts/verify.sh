#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

node scripts/check-boundaries.mjs

unformatted="$(gofmt -l cmd internal)"
if [[ -n "$unformatted" ]]; then
  echo "Go files require gofmt:" >&2
  echo "$unformatted" >&2
  exit 1
fi

go vet ./...
go test ./...
pnpm --recursive lint
pnpm --recursive typecheck
pnpm --recursive test
PYTHONPATH="services/speech/src${PYTHONPATH:+:$PYTHONPATH}" \
  uv run --locked --all-packages python -m unittest discover -s services/speech/tests
./scripts/check-generated.sh

echo "Tacit verification passed."
