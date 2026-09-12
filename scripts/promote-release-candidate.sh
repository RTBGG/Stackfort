#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail
repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repository_root"
[[ "${GITHUB_EVENT_NAME:-}" == push && "${GITHUB_REPOSITORY:-}" == RTBGG/Stackfort ]]
version="${VERSION:?VERSION is required}"
commit="${GITHUB_SHA:?GITHUB_SHA is required}"
[[ "$version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-beta\.[1-9][0-9]*)?$ ]]
[[ "$commit" =~ ^[0-9a-f]{40}$ && "${GITHUB_REF:-}" == "refs/tags/v$version" && "$(git rev-parse HEAD)" == "$commit" ]]
[[ ! -e dist && ! -L dist ]]
workspace="$(mktemp -d)"
trap 'rm -rf -- "$workspace"' EXIT
api() { gh api -H 'X-GitHub-Api-Version: 2026-03-10' "$@"; }
evidence_ref="$(api repos/RTBGG/Stackfort/git/ref/heads/main --jq '.object.sha')"
[[ "$evidence_ref" =~ ^[0-9a-f]{40}$ ]]
comparison="$(api "repos/RTBGG/Stackfort/compare/$commit...$evidence_ref" --jq '.status')"
[[ "$comparison" == ahead || "$comparison" == identical ]]
api -H 'Accept: application/vnd.github.raw+json' \
  "repos/RTBGG/Stackfort/contents/packaging/releases/promotion/$version.json?ref=$evidence_ref" >"$workspace/promotion.json"
node scripts/promote-release-candidate.mjs --mode inventory --promotion "$workspace/promotion.json" \
  --version "$version" --commit "$commit" >"$workspace/candidate.json"
run_id="$(jq -r '.build.runId' "$workspace/candidate.json")"
artifact_id="$(jq -r '.build.artifactId' "$workspace/candidate.json")"
api "repos/RTBGG/Stackfort/actions/runs/$run_id" >"$workspace/run.json"
api --paginate --slurp "repos/RTBGG/Stackfort/actions/runs/$run_id/artifacts?per_page=100" |
  jq -e 'if length > 0 and all(.[]; (.artifacts | type) == "array" and (.total_count | type) == "number")
    and ([.[].total_count] | unique | length) == 1
    then {total_count: .[0].total_count, artifacts: [.[].artifacts[]]}
    else error("invalid artifact inventory") end' >"$workspace/artifacts.json"
node scripts/promote-release-candidate.mjs --mode verify --promotion "$workspace/promotion.json" \
  --version "$version" --commit "$commit" --run "$workspace/run.json" --artifacts "$workspace/artifacts.json" >"$workspace/verified.json"
api "repos/RTBGG/Stackfort/actions/artifacts/$artifact_id/zip" >"$workspace/candidate.zip"
umask 077
python3 scripts/extract-release-candidate.py "$workspace/verified.json" "$workspace/candidate.zip" "$workspace/payload"
# Only the validated flat regular-file inventory is copied. No archive paths are
# extracted into the checkout, and no build/replacement fallback is available.
mkdir dist
for source in "$workspace/payload/"*; do
  install -m 0644 "$source" "dist/${source##*/}"
done
install -m 0644 "$workspace/verified.json" dist/candidate-promotion.json
printf 'Exact retained candidate promoted for tag qualification; publication is not authorized.\n'
