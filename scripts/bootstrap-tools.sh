#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

go mod download
pnpm install --frozen-lockfile
uv sync --locked --all-packages
./scripts/verify.sh
