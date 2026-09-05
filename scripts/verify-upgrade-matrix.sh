#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail
repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repository_root"

version="${VERSION:?VERSION is required}"
archive="dist/stackfort-$version-linux-amd64.tar.gz"
test -f "$archive"
workspace="$(mktemp -d)"
trap 'rm -rf -- "$workspace"' EXIT

# Slurp all pages, then flatten the page arrays. A failed/truncated inventory
# must never turn into an empty predecessor list.
gh api -H 'X-GitHub-Api-Version: 2026-03-10' --paginate --slurp 'repos/RTBGG/Stackfort/releases?per_page=100' |
  jq -e 'add | if type == "array" then . else error("invalid release inventory") end' >"$workspace/published.json"
go run ./cmd/stackfort-upgrade-matrix --target "$version" \
  --published "$workspace/published.json" >"$workspace/plan.json"
evidence_flags=()
if [[ "$(jq '.cells | length' "$workspace/plan.json")" -gt 0 ]]; then
  # Evidence is reviewed on main AFTER testing the candidate commit. The tag
  # still names that tested commit, avoiding an evidence/build hash cycle.
  evidence_ref="$(gh api repos/RTBGG/Stackfort/git/ref/heads/main --jq '.object.sha')"
  [[ "$evidence_ref" =~ ^[0-9a-f]{40}$ ]]
  gh api "repos/RTBGG/Stackfort/contents/packaging/upgrades/evidence/$version.json?ref=$evidence_ref" \
    --jq '.content' | base64 --decode >"$workspace/evidence.json"
  evidence_flags=(--evidence "$workspace/evidence.json")
fi
target_digest="$(sha256sum "$archive" | cut -d ' ' -f 1)"
go run ./cmd/stackfort-upgrade-matrix --target "$version" --verify \
  --published "$workspace/published.json" --target-sha256 "$target_digest" \
  "${evidence_flags[@]}" >dist/upgrade-matrix.json
printf 'Upgrade matrix publication gate passed for %s.\n' "$version"
