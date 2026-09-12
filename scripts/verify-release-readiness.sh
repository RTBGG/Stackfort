#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later

set -euo pipefail
repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repository_root"

# This wrapper is deliberately publication-only; workflow_dispatch candidates
# are built without invoking it and must never claim publication readiness.
[[ "${GITHUB_EVENT_NAME:-}" == push && "${GITHUB_REPOSITORY:-}" == RTBGG/Stackfort ]]
version="${VERSION:?VERSION is required}"
commit="${GITHUB_SHA:?GITHUB_SHA is required}"
[[ "$version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-beta\.[1-9][0-9]*)?$ ]]
[[ "$commit" =~ ^[0-9a-f]{40}$ && "${GITHUB_REF:-}" == "refs/tags/v$version" ]]
[[ "$(git rev-parse HEAD)" == "$commit" ]]
archive="dist/stackfort-$version-linux-amd64.tar.gz"
test -f "$archive" && test ! -L "$archive"
archive_digest="$(sha256sum "$archive" | cut -d ' ' -f 1)"
workspace="$(mktemp -d)"
trap 'rm -rf -- "$workspace"' EXIT

api() {
  gh api -H 'X-GitHub-Api-Version: 2026-03-10' "$@"
}
fetch_file() {
  api -H 'Accept: application/vnd.github.raw+json' \
    "repos/RTBGG/Stackfort/contents/$1?ref=$evidence_ref"
}

# Like upgrade evidence, reviewed readiness is committed after the fixed
# candidate. Resolve main once; every human report comes from that same commit.
evidence_ref="$(api repos/RTBGG/Stackfort/git/ref/heads/main --jq '.object.sha')"
[[ "$evidence_ref" =~ ^[0-9a-f]{40}$ ]]
comparison="$(api "repos/RTBGG/Stackfort/compare/$commit...$evidence_ref" --jq '.status')"
[[ "$comparison" == ahead || "$comparison" == identical ]]
fetch_file "packaging/releases/evidence/$version.json" >"$workspace/evidence.json"
fetch_file packaging/releases/readiness-policy.json >"$workspace/policy.json"
# A later evidence commit cannot silently lower the tested candidate's scope.
cmp packaging/releases/readiness-policy.json "$workspace/policy.json"
# The reviewed archive AND its retained build identity must be the ones actually
# promoted in this job, never a synthesized assertion about a rebuilt archive.
test -f dist/candidate-promotion.json && test ! -L dist/candidate-promotion.json
jq -e --slurpfile promotion dist/candidate-promotion.json \
  '.candidate == $promotion[0].candidate and $promotion[0].kind == "verified-candidate-promotion" and $promotion[0].publicationAuthorized == false' \
  "$workspace/evidence.json" >/dev/null

arguments=(--policy packaging/releases/readiness-policy.json --evidence "$workspace/evidence.json"
  --version "$version" --commit "$commit" --archive-sha256 "$archive_digest")
node scripts/verify-release-readiness.mjs --mode inventory "${arguments[@]}" >"$workspace/inventory.json"

index=0
while IFS= read -r report_path; do
  fetch_file "$report_path" >"$workspace/report-$index"
  index=$((index + 1))
done < <(jq -r '.reports[].path' "$workspace/inventory.json")

while IFS=$'\t' read -r run_id attempt; do
  api "repos/RTBGG/Stackfort/actions/runs/$run_id" >"$workspace/run-$run_id.json"
  # Latest run metadata rejects an older successful attempt after a failed rerun.
  # Preserve the total count; truncated pagination must not look complete.
  api --paginate --slurp "repos/RTBGG/Stackfort/actions/runs/$run_id/attempts/$attempt/jobs?per_page=100" |
    jq -e 'if length > 0 and all(.[]; (.jobs | type) == "array" and (.total_count | type) == "number")
      and ([.[].total_count] | unique | length) == 1
      then {total_count: .[0].total_count, jobs: [.[].jobs[]]}
      else error("invalid workflow job inventory") end' >"$workspace/jobs-$run_id.json"
done < <(jq -r '.workflowRuns[] | [.runId, .attempt] | @tsv' "$workspace/inventory.json")

node scripts/verify-release-readiness.mjs --mode verify "${arguments[@]}" \
  --evidence-commit "$evidence_ref" --input-directory "$workspace" \
  --security-policy SECURITY.md >"$workspace/verified.json"
install -m 0644 "$workspace/verified.json" dist/release-readiness.json
printf 'Release readiness gate passed for the exact %s candidate.\n' "$version"
