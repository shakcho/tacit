#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

if command -v mise >/dev/null 2>&1; then
  mise install
  exec mise exec -- ./scripts/bootstrap-tools.sh
fi

echo "mise is required to install the pinned toolchain from mise.toml." >&2
echo "Install mise, then run ./scripts/bootstrap.sh again." >&2
exit 1
