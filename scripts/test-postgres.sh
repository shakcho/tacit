#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

: "${TACIT_TEST_DATABASE_URL:?TACIT_TEST_DATABASE_URL is required}"

TACIT_DATABASE_DIRECT_URL="$TACIT_TEST_DATABASE_URL" go run ./cmd/tacit-db migrate
go test -race -tags=integration ./internal/platform/postgres/...
