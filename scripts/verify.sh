#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repository_root"

export GOTOOLCHAIN=local

mapfile -d '' -t go_files < <(find cmd internal -type f -name '*.go' -print0)
format_diff="$(gofmt -d "${go_files[@]}")"
if [[ -n "$format_diff" ]]; then
  printf '%s\n' "$format_diff" >&2
  exit 1
fi

go vet ./...
if [[ "$(uname -s)" == "Linux" ]]; then
  go test -race ./...
  bash scripts/test-agent-smoke.sh
else
  go test ./...
  printf 'Linux agent runtime and Go race tests skipped on %s.\n' "$(uname -s)"
fi

(
  cd web
  npm ci
  npm run typecheck
  npm run check:i18n
  npm test
  npm run build
  npm audit --audit-level=high
)

bash scripts/verify-actions-pinned.sh
bash packaging/waf/verify-locks.sh
printf 'Stackfort verification passed.\n'
