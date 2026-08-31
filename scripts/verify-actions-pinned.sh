#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
workflow_root="$repository_root/.github/workflows"

if [[ ! -d "$workflow_root" ]]; then
  printf 'Workflow directory does not exist: %s\n' "$workflow_root" >&2
  exit 1
fi

failed=0
while IFS= read -r use; do
  reference="${use#uses:}"
  reference="${reference//[[:space:]]/}"

  if [[ "$reference" == ./* ]]; then
    continue
  fi
  if [[ ! "$reference" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)*@[0-9a-f]{40}$ ]]; then
    printf 'Unpinned workflow dependency: %s\n' "$reference" >&2
    failed=1
  fi
done < <(grep -RhoE 'uses:[[:space:]]*[^[:space:]#]+' "$workflow_root" | sort -u)

if (( failed != 0 )); then
  exit 1
fi

printf 'All workflow dependencies are pinned to full commit hashes.\n'
